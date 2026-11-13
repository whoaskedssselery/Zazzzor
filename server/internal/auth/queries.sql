-- name: CreateUser :one
INSERT INTO users (id, username, display_name, password_hash, created_at)
VALUES (@id, @username, @display_name, @password_hash, @created_at)
ON CONFLICT (username) DO NOTHING
RETURNING *;

-- name: UserByUsername :one
SELECT * FROM users WHERE username = @username;

-- name: UserByID :one
SELECT * FROM users WHERE id = @id;

-- name: UsersByIDs :many
SELECT * FROM users WHERE id = ANY(@ids::bigint[]) ORDER BY id;

-- name: SetPasswordHash :execrows
UPDATE users SET password_hash = @password_hash WHERE id = @id;

-- name: ReplacePasswordHash :execrows
UPDATE users SET password_hash = @password_hash
WHERE id = @id AND password_hash = @verified_hash AND disabled_at IS NULL;

-- name: SetDisplayName :one
UPDATE users SET display_name = @display_name WHERE id = @id RETURNING *;

-- name: ChangeProfile :one
-- Changes the fields given (a null leaves one as it is); custom_status is
-- written only with set_custom_status, so it can be cleared.
UPDATE users SET
    display_name  = COALESCE(sqlc.narg(display_name)::text, display_name),
    bio           = COALESCE(sqlc.narg(bio)::text, bio),
    status        = COALESCE(sqlc.narg(status)::text, status),
    custom_status = CASE WHEN @set_custom_status::bool THEN sqlc.narg(custom_status)::text ELSE custom_status END
WHERE id = @id RETURNING *;

-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, created_at, last_used_at, expires_at, user_agent, ip)
VALUES (@id, @user_id, @token_hash, @created_at, @created_at, @expires_at, @user_agent, @ip);

-- name: DeleteExpiredUserSessions :exec
DELETE FROM sessions WHERE user_id = @user_id AND expires_at <= @now;

-- name: LockUserCredentials :one
-- Serializes session creation with credential changes and other sign-ins.
SELECT * FROM users WHERE id = @id FOR UPDATE;

-- name: DeleteSessionsBeyond :many
-- Keeps the newest @keep sessions of the user.
DELETE FROM sessions AS s WHERE s.id IN (
    SELECT k.id FROM sessions AS k WHERE k.user_id = sqlc.arg(user_id)::bigint
    ORDER BY k.created_at DESC, k.id DESC
    OFFSET sqlc.arg(keep)::bigint
)
RETURNING s.id;

-- name: SessionByTokenHash :one
-- Only live sessions of enabled accounts authenticate.
SELECT sessions.id AS session_id, sessions.last_used_at, sessions.expires_at, sqlc.embed(users)
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = @token_hash
  AND sessions.expires_at > @now
  AND users.disabled_at IS NULL;

-- name: TouchSession :execrows
-- Conditional on the previous last_used_at, so concurrent requests renew once.
UPDATE sessions SET last_used_at = @now, expires_at = @expires_at
WHERE id = @id AND last_used_at = @prev_last_used_at;

-- name: DeleteSession :one
DELETE FROM sessions WHERE id = @id RETURNING user_id;

-- name: DeleteUserSessions :many
DELETE FROM sessions WHERE user_id = @user_id RETURNING id;

-- name: DeleteOtherUserSessions :many
DELETE FROM sessions WHERE user_id = @user_id AND id <> @keep_id RETURNING id;

-- name: DisableUser :one
-- Keeps the time of the first disable.
UPDATE users SET disabled_at = COALESCE(disabled_at, @now::timestamptz) WHERE id = @id RETURNING *;

-- name: EnableUser :one
UPDATE users SET disabled_at = NULL WHERE id = @id RETURNING *;

-- name: ReplaceAvatarHash :one
-- Locks the row first, so concurrent calls each see the hash they replace.
UPDATE users SET avatar_hash = @avatar_hash
FROM (SELECT users.avatar_hash FROM users WHERE users.id = @id FOR UPDATE) AS prev
WHERE users.id = @id
RETURNING sqlc.embed(users), prev.avatar_hash AS previous_hash;

-- name: AvatarReferences :many
SELECT id, COALESCE(avatar_hash, '')::text AS hash FROM users WHERE avatar_hash IS NOT NULL;

-- name: AvatarReferenced :one
SELECT EXISTS (SELECT 1 FROM users WHERE id = @id AND avatar_hash = @hash::text);
