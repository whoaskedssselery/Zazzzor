-- name: InsertEntry :exec
INSERT INTO audit_log (id, guild_id, actor_id, action_type, target_id, changes, options, reason, created_at)
VALUES (@id, @guild_id, @actor_id, @action_type, @target_id, @changes, @options, @reason, @created_at);

-- name: ListEntries :many
-- Newest first; before 0 means from the newest, zero filters mean any.
SELECT * FROM audit_log
WHERE guild_id = @guild_id
    AND (@before::bigint = 0 OR id < @before::bigint)
    AND (@actor_id::bigint = 0 OR actor_id = @actor_id::bigint)
    AND (@action_type::smallint = 0 OR action_type = @action_type::smallint)
ORDER BY id DESC
LIMIT @lim;

-- name: PruneEntries :execrows
-- Entries older than the cutoff go.
DELETE FROM audit_log WHERE created_at < @cutoff;
