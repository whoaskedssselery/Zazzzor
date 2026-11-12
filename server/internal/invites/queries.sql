-- name: CreateInvite :one
-- Times come from the database clock, which also judges expiry.
INSERT INTO invites (code, guild_id, channel_id, inviter_id, max_uses, expires_at, created_at)
VALUES (@code, @guild_id, @channel_id, @inviter_id, @max_uses,
    CASE WHEN @max_age_seconds::integer = 0 THEN NULL
         ELSE now() + make_interval(secs => @max_age_seconds::integer) END,
    now())
RETURNING *;

-- name: InviteByCode :one
SELECT invites.*,
    (expires_at IS NOT NULL AND expires_at <= now())::boolean AS expired,
    (max_uses > 0 AND uses >= max_uses)::boolean AS exhausted
FROM invites WHERE code = @code;

-- name: UseInvite :one
-- Takes one use if the invite is still valid; no row otherwise. A
-- concurrent use waits for the row lock and re-checks the condition.
UPDATE invites SET uses = uses + 1
WHERE code = @code
  AND (max_uses = 0 OR uses < max_uses)
  AND (expires_at IS NULL OR expires_at > now())
RETURNING *;

-- name: GuildInvites :many
SELECT i.* FROM invites AS i
LEFT JOIN invites AS cursor ON cursor.code = @before_code::text
    AND cursor.guild_id = @guild_id AND cursor.channel_id = ANY(@channel_ids::bigint[])
WHERE i.guild_id = @guild_id AND i.channel_id = ANY(@channel_ids::bigint[])
  AND (@before_code = '' OR i.created_at < cursor.created_at
       OR (i.created_at = cursor.created_at AND i.code > cursor.code))
ORDER BY i.created_at DESC, i.code
LIMIT @lim::bigint;

-- name: DeleteInvite :execrows
DELETE FROM invites WHERE code = @code;
