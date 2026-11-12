-- name: IsMember :one
SELECT EXISTS (SELECT 1 FROM members WHERE guild_id = @guild_id AND user_id = @user_id);

-- name: ScopeSettings :one
SELECT muted, mute_until, message_notifications, suppress_everyone
FROM user_guild_settings WHERE user_id = @user_id AND scope_id = @scope_id;

-- name: SaveScopeSettings :exec
INSERT INTO user_guild_settings (user_id, guild_id, muted, mute_until, message_notifications, suppress_everyone)
VALUES (@user_id, sqlc.narg(guild_id), @muted, sqlc.narg(mute_until), @message_notifications, @suppress_everyone)
ON CONFLICT (user_id, scope_id) DO UPDATE SET muted = EXCLUDED.muted, mute_until = EXCLUDED.mute_until,
    message_notifications = EXCLUDED.message_notifications, suppress_everyone = EXCLUDED.suppress_everyone;

-- name: DeleteScopeSettings :exec
DELETE FROM user_guild_settings WHERE user_id = @user_id AND scope_id = @scope_id;

-- name: ScopeOverrides :many
SELECT channel_id, muted, mute_until, message_notifications
FROM channel_notification_overrides
WHERE user_id = @user_id AND guild_id IS NOT DISTINCT FROM sqlc.narg(guild_id)
ORDER BY channel_id;

-- name: SaveOverride :exec
INSERT INTO channel_notification_overrides (user_id, channel_id, guild_id, muted, mute_until, message_notifications)
VALUES (@user_id, @channel_id, sqlc.narg(guild_id), @muted, sqlc.narg(mute_until), @message_notifications)
ON CONFLICT (user_id, channel_id) DO UPDATE SET muted = EXCLUDED.muted, mute_until = EXCLUDED.mute_until,
    message_notifications = EXCLUDED.message_notifications;

-- name: DeleteOverride :exec
DELETE FROM channel_notification_overrides WHERE user_id = @user_id AND channel_id = @channel_id;

-- name: AllScopeSettings :many
SELECT guild_id, muted, mute_until, message_notifications, suppress_everyone
FROM user_guild_settings WHERE user_id = @user_id ORDER BY scope_id;

-- name: AllOverrides :many
SELECT guild_id, channel_id, muted, mute_until, message_notifications
FROM channel_notification_overrides WHERE user_id = @user_id ORDER BY guild_id NULLS FIRST, channel_id;

-- name: LockUserSettings :exec
-- Serializes one user's PATCHes, so a merge reads what the last one wrote.
SELECT pg_advisory_xact_lock(hashtextextended('zazzzor.notify:' || @user_id::bigint, 0));
