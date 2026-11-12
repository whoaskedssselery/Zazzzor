-- name: CreateChannel :one
INSERT INTO channels (id, guild_id, type, name, topic, parent_id, position, permission_synced, created_at)
VALUES (@id, @guild_id::bigint, @type, @name::text, @topic, @parent_id, @position, @permission_synced, @created_at)
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
UPDATE channels SET name = @name::text, topic = @topic WHERE id = @id;

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

-- name: SeedReadStates :exec
INSERT INTO read_states (user_id, channel_id, guild_id, last_read_id, mention_count)
SELECT @user_id::bigint, c.id, c.guild_id, COALESCE(c.last_message_id, 0), 0
FROM channels AS c
WHERE c.id = ANY(@channel_ids::bigint[])
ON CONFLICT (user_id, channel_id) DO NOTHING;

-- name: GuildReadStateKeys :many
SELECT user_id, channel_id FROM read_states WHERE guild_id = @guild_id::bigint;

-- name: DropReadStates :exec
DELETE FROM read_states AS r
USING (
    SELECT unnest(@user_ids::bigint[]) AS user_id, unnest(@channel_ids::bigint[]) AS channel_id
) AS hidden
WHERE r.guild_id = @guild_id::bigint
    AND r.user_id = hidden.user_id AND r.channel_id = hidden.channel_id;

-- name: LockDirectPair :exec
SELECT pg_advisory_xact_lock(hashtextextended(
    'zazzzor.dm:' || @low_id::bigint || ':' || @high_id::bigint, 0));

-- name: DirectChannelOfPair :one
SELECT channel_id FROM direct_channel_pairs WHERE low_id = $1 AND high_id = $2;

-- name: BindDirectPair :exec
INSERT INTO direct_channel_pairs (low_id, high_id, channel_id) VALUES ($1,$2,$3);

-- name: LockPrivateAccounts :many
-- Same ordered account locks as relationship writes, plus group quotas.
SELECT id FROM users WHERE id = ANY(@user_ids::bigint[]) ORDER BY id FOR NO KEY UPDATE;

-- name: CanDirectMessage :one
SELECT (
    EXISTS (SELECT 1 FROM relationships WHERE user_id = @actor_id::bigint AND other_id = @other_id::bigint AND type = 1)
    OR EXISTS (SELECT 1 FROM members a JOIN members b USING (guild_id)
               WHERE a.user_id = @actor_id AND b.user_id = @other_id)
) AND NOT EXISTS (
    SELECT 1 FROM relationships WHERE type = 2 AND
    ((user_id = @actor_id AND other_id = @other_id) OR (user_id = @other_id AND other_id = @actor_id))
) AS allowed;

-- name: CreatePrivateChannel :one
INSERT INTO channels (id, type, owner_id, position, permission_synced, created_at)
VALUES (@id, @type, sqlc.narg(owner_id), 0, false, @created_at)
RETURNING *;

-- name: AddPrivateRecipients :exec
INSERT INTO channel_recipients (channel_id, user_id, joined_at)
SELECT @channel_id, id, @joined_at FROM unnest(@user_ids::bigint[]) AS targets(id);

-- name: OpenPrivateChannels :many
SELECT c.* FROM channels c JOIN channel_recipients r ON c.id = r.channel_id
WHERE r.user_id = @user_id AND NOT r.closed
ORDER BY c.last_message_id DESC NULLS LAST, c.id DESC;

-- name: PrivateProfiles :many
SELECT r.channel_id, u.id, u.username, u.display_name, u.avatar_hash, u.created_at
FROM channel_recipients r JOIN users u ON r.user_id = u.id
WHERE r.channel_id = ANY(@channel_ids::bigint[])
ORDER BY r.channel_id, r.joined_at, r.user_id;

-- name: PrivateRecipientIDs :many
SELECT user_id FROM channel_recipients WHERE channel_id = @channel_id ORDER BY user_id;

-- name: IsPrivateRecipient :one
SELECT EXISTS (SELECT 1 FROM channel_recipients WHERE channel_id = @channel_id AND user_id = @user_id);

-- name: LockPrivateChannel :one
SELECT * FROM channels WHERE id = @id AND guild_id IS NULL FOR NO KEY UPDATE;

-- name: SetDirectClosed :execrows
UPDATE channel_recipients SET closed = @closed
WHERE channel_id = @channel_id AND user_id = @user_id AND closed <> @closed;

-- name: ReopenPrivateRecipients :many
UPDATE channel_recipients SET closed = false WHERE channel_id = @channel_id AND closed RETURNING user_id;

-- name: GroupFriendIDs :many
SELECT other_id FROM relationships WHERE user_id = @actor_id AND type = 1 AND other_id = ANY(@user_ids::bigint[]);

-- name: GroupsAtCapacity :one
SELECT EXISTS (
    SELECT 1 FROM channel_recipients AS r JOIN channels AS c ON c.id = r.channel_id
    WHERE c.type = 3 AND r.user_id = ANY(@user_ids::bigint[])
    GROUP BY r.user_id HAVING count(*) >= @max_groups::integer
);

-- name: RenamePrivateGroup :exec
UPDATE channels SET name = sqlc.narg(name) WHERE id = @id AND type = 3;

-- name: RemovePrivateRecipient :exec
DELETE FROM channel_recipients WHERE channel_id = @channel_id AND user_id = @user_id;
