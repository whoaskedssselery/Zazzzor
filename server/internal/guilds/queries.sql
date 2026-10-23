-- name: LockUserGuilds :exec
-- Serializes the guild creations and joins of one user, so the per-user
-- guild limit holds under concurrency. Held until the transaction ends.
SELECT pg_advisory_xact_lock(hashtextextended('zazzzor.guilds.user:' || sqlc.arg(user_id)::bigint, 0));

-- name: LockGuild :one
-- Serializes the role and member-role changes of one guild. NO KEY UPDATE
-- does not block the key-share locks that inserts referencing the guild take.
SELECT id FROM guilds WHERE id = @id FOR NO KEY UPDATE;

-- name: CountUserGuilds :one
SELECT count(*) FROM members WHERE user_id = @user_id;

-- name: CreateGuild :one
INSERT INTO guilds (id, name, owner_id, created_at)
VALUES (@id, @name, @owner_id, @created_at)
RETURNING *;

-- name: GuildByID :one
SELECT * FROM guilds WHERE id = @id;

-- name: GuildsOfUser :many
SELECT guilds.* FROM guilds
JOIN members ON members.guild_id = guilds.id
WHERE members.user_id = @user_id
ORDER BY guilds.id;

-- name: GuildIDsOfUser :many
SELECT guild_id FROM members WHERE user_id = @user_id ORDER BY guild_id;

-- name: PeerIDs :many
-- Everyone sharing at least one guild with user_id (user_id included when
-- it is in any guild), ascending.
SELECT DISTINCT other.user_id FROM members AS mine
JOIN members AS other ON other.guild_id = mine.guild_id
WHERE mine.user_id = @user_id
ORDER BY other.user_id;

-- name: RenameGuild :one
UPDATE guilds SET name = @name WHERE id = @id RETURNING *;

-- name: ReplaceIconHash :one
-- Locks the row first, so concurrent calls each see the hash they replace.
UPDATE guilds SET icon_hash = @icon_hash
FROM (SELECT guilds.icon_hash FROM guilds WHERE guilds.id = @id FOR UPDATE) AS prev
WHERE guilds.id = @id
RETURNING sqlc.embed(guilds), prev.icon_hash AS previous_hash;

-- name: DeleteGuild :one
DELETE FROM guilds WHERE id = @id RETURNING *;

-- name: AddMember :execrows
INSERT INTO members (guild_id, user_id, joined_at)
VALUES (@guild_id, @user_id, @joined_at)
ON CONFLICT DO NOTHING;

-- name: DeleteMember :execrows
DELETE FROM members WHERE guild_id = @guild_id AND user_id = @user_id;

-- name: MemberByID :one
SELECT * FROM members WHERE guild_id = @guild_id AND user_id = @user_id;

-- name: IsMember :one
SELECT EXISTS (SELECT 1 FROM members WHERE guild_id = @guild_id AND user_id = @user_id);

-- name: MemberIDs :many
SELECT user_id FROM members WHERE guild_id = @guild_id ORDER BY user_id;

-- name: MembersPage :many
SELECT * FROM members
WHERE guild_id = @guild_id AND user_id > @after
ORDER BY user_id
LIMIT @lim;

-- name: AllMembers :many
SELECT * FROM members WHERE guild_id = @guild_id ORDER BY user_id;

-- name: SetNick :one
UPDATE members SET nick = @nick WHERE guild_id = @guild_id AND user_id = @user_id RETURNING *;

-- name: MemberSubject :one
-- The owner and @everyone role of a member's guild; no row for a non-member.
SELECT guilds.owner_id, everyone.position AS everyone_position, everyone.permissions AS everyone_permissions
FROM members
JOIN guilds ON guilds.id = members.guild_id
JOIN roles AS everyone ON everyone.id = guilds.id
WHERE members.guild_id = @guild_id AND members.user_id = @user_id;

-- name: RolesOfMember :many
SELECT roles.* FROM member_roles
JOIN roles ON roles.id = member_roles.role_id
WHERE member_roles.guild_id = @guild_id AND member_roles.user_id = @user_id
ORDER BY roles.position;

-- name: MemberRoleIDs :many
-- The non-@everyone roles of the given members, for listing them.
SELECT user_id, role_id FROM member_roles
WHERE guild_id = @guild_id AND user_id = ANY(@user_ids::bigint[])
ORDER BY user_id, role_id;

-- name: AddMemberRoles :exec
INSERT INTO member_roles (guild_id, user_id, role_id)
SELECT @guild_id, @user_id, unnest(@role_ids::bigint[])
ON CONFLICT DO NOTHING;

-- name: RemoveMemberRoles :exec
DELETE FROM member_roles
WHERE guild_id = @guild_id AND user_id = @user_id AND role_id = ANY(@role_ids::bigint[]);

-- name: CreateRole :one
INSERT INTO roles (id, guild_id, name, color, position, permissions, hoist, mentionable, created_at)
VALUES (@id, @guild_id, @name, @color, @position, @permissions, @hoist, @mentionable, @created_at)
RETURNING *;

-- name: GuildRoles :many
SELECT * FROM roles WHERE guild_id = @guild_id ORDER BY position;

-- name: CountRoles :one
SELECT count(*) FROM roles WHERE guild_id = @guild_id;

-- name: RoleByID :one
SELECT * FROM roles WHERE guild_id = @guild_id AND id = @id;

-- name: ShiftRolesUp :exec
-- Makes room at position 1 for a new role.
UPDATE roles SET position = position + 1 WHERE guild_id = @guild_id AND position >= 1;

-- name: CloseRoleGap :exec
-- Closes the gap a deleted role left at @position.
UPDATE roles SET position = position - 1 WHERE guild_id = @guild_id AND position > @position;

-- name: UpdateRole :one
UPDATE roles SET name = @name, color = @color, permissions = @permissions,
    hoist = @hoist, mentionable = @mentionable
WHERE guild_id = @guild_id AND id = @id
RETURNING *;

-- name: SetRolePositions :exec
UPDATE roles SET position = new.position
FROM (SELECT unnest(@ids::bigint[]) AS id, unnest(@positions::integer[]) AS position) AS new
WHERE roles.guild_id = @guild_id AND roles.id = new.id;

-- name: DeleteRole :one
DELETE FROM roles WHERE guild_id = @guild_id AND id = @id RETURNING *;

-- name: MembersWithRoles :many
-- Every member of the guild with the IDs of their non-@everyone roles.
SELECT members.user_id,
    coalesce(array_agg(member_roles.role_id ORDER BY member_roles.role_id)
        FILTER (WHERE member_roles.role_id IS NOT NULL), '{}')::bigint[] AS role_ids
FROM members
LEFT JOIN member_roles ON member_roles.guild_id = members.guild_id AND member_roles.user_id = members.user_id
WHERE members.guild_id = @guild_id
GROUP BY members.user_id
ORDER BY members.user_id;

-- name: CreateBan :execrows
-- Bans user_id; an existing ban is left as it is (no row).
INSERT INTO guild_bans (guild_id, user_id, reason, created_by, created_at)
VALUES (@guild_id, @user_id, @reason, @created_by, @created_at)
ON CONFLICT DO NOTHING;

-- name: DeleteBan :execrows
DELETE FROM guild_bans WHERE guild_id = @guild_id AND user_id = @user_id;

-- name: IsBanned :one
SELECT EXISTS (SELECT 1 FROM guild_bans WHERE guild_id = @guild_id AND user_id = @user_id);

-- name: BanByID :one
SELECT * FROM guild_bans WHERE guild_id = @guild_id AND user_id = @user_id;

-- name: GuildBans :many
SELECT * FROM guild_bans WHERE guild_id = @guild_id ORDER BY created_at, user_id LIMIT @lim;

-- name: SharedPeers :many
-- The pairs (user, peer) with user among user_ids and peer among peer_ids
-- that share at least one guild (a user is its own peer when in a guild).
SELECT DISTINCT mine.user_id, other.user_id AS peer_id FROM members AS mine
JOIN members AS other ON other.guild_id = mine.guild_id
WHERE mine.user_id = ANY(@user_ids::bigint[]) AND other.user_id = ANY(@peer_ids::bigint[])
ORDER BY mine.user_id, peer_id;
