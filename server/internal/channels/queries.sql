-- name: CreateChannel :one
INSERT INTO channels (id, guild_id, type, name, topic, parent_id, position, permission_synced, created_at)
VALUES (@id, @guild_id, @type, @name, @topic, @parent_id, @position, @permission_synced, @created_at)
RETURNING *;

-- name: ChannelByID :one
SELECT * FROM channels WHERE id = @id;

-- name: GuildChannels :many
SELECT * FROM channels WHERE guild_id = @guild_id ORDER BY position, id;

-- name: CountChannels :one
SELECT count(*) FROM channels WHERE guild_id = @guild_id;

-- name: OverwritesOf :many
-- The overwrites of the given channels (a channel and its category).
SELECT * FROM overwrites
WHERE channel_id = ANY(@channel_ids::bigint[])
ORDER BY channel_id, target_id;

-- name: GuildOverwrites :many
SELECT overwrites.* FROM overwrites
JOIN channels ON channels.id = overwrites.channel_id
WHERE channels.guild_id = @guild_id
ORDER BY overwrites.channel_id, overwrites.target_id;

-- name: SetChannelInfo :exec
UPDATE channels SET name = @name, topic = @topic WHERE id = @id;

-- name: SetChannelLayout :exec
-- Moves channels within the guild in one statement; parent 0 means none.
-- The unique position is checked at commit.
UPDATE channels
SET position = new.position, parent_id = nullif(new.parent_id, 0), permission_synced = new.synced
FROM (
    SELECT unnest(@ids::bigint[]) AS id, unnest(@positions::integer[]) AS position,
        unnest(@parent_ids::bigint[]) AS parent_id, unnest(@synced::boolean[]) AS synced
) AS new
WHERE channels.guild_id = @guild_id AND channels.id = new.id;

-- name: DeleteChannel :exec
DELETE FROM channels WHERE id = @id;

-- name: ClearOverwrites :exec
DELETE FROM overwrites WHERE channel_id = @channel_id;

-- name: InsertOverwrites :exec
INSERT INTO overwrites (channel_id, target_id, target_type, allow, deny)
SELECT @channel_id, unnest(@target_ids::bigint[]), unnest(@target_types::smallint[]),
    unnest(@allows::bigint[]), unnest(@denies::bigint[]);

-- name: DeleteTargetOverwrites :exec
-- Drops every overwrite of a role or member in the guild's channels.
DELETE FROM overwrites
USING channels
WHERE channels.id = overwrites.channel_id
  AND channels.guild_id = @guild_id
  AND overwrites.target_id = @target_id;
