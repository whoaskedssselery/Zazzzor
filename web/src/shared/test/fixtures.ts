import type { components } from "@/shared/api";

type S = components["schemas"];
type User = S["User"];
type Guild = S["Guild"];
type Role = S["Role"];
type Channel = S["Channel"];
type Member = S["Member"];
type GuildState = S["GuildState"];
type GatewayReady = S["GatewayReady"];

/**
 * Builders of contract-shaped objects for tests (types from schema.gen.ts):
 * sensible defaults, any field overridable.
 */
const AT = "2026-10-21T12:00:00.000Z";

export function makeUser(id: string, over: Partial<User> = {}): User {
  return { id, username: `user${id}`, display_name: `Пользователь ${id}`, avatar_url: null, created_at: AT, ...over };
}

export function makeGuild(id: string, ownerId: string, over: Partial<Guild> = {}): Guild {
  return { id, name: `Сервер ${id}`, icon_url: null, owner_id: ownerId, created_at: AT, ...over };
}

export function makeRole(id: string, over: Partial<Role> = {}): Role {
  return { id, name: `роль ${id}`, color: 0, position: 1, permissions: "0", hoist: false, mentionable: false, ...over };
}

/** The @everyone role of a server: its ID is the server's, position 0. */
export function makeEveryone(guildId: string, permissions = "68160"): Role {
  return makeRole(guildId, { name: "@everyone", position: 0, permissions });
}

export function makeChannel(id: string, guildId: string, over: Partial<Channel> = {}): Channel {
  return {
    id,
    guild_id: guildId,
    type: 0,
    name: `канал-${id}`,
    topic: null,
    parent_id: null,
    position: 0,
    permission_synced: false,
    permission_overwrites: [],
    ...over,
  };
}

export function makeMember(user: User, over: Partial<Member> = {}): Member {
  return { user, nick: null, roles: [], joined_at: AT, ...over };
}

/** A server as `me` sees it: @everyone, one text channel, `me` as the only member unless given. */
export function makeGuildState(guildId: string, me: User, over: Partial<GuildState> = {}): GuildState {
  const myMember = over.my_member ?? makeMember(me);
  return {
    guild: makeGuild(guildId, me.id),
    roles: [makeEveryone(guildId)],
    channels: [makeChannel(`${guildId}1`, guildId)],
    members: [myMember],
    my_member: myMember,
    presences: [{ user_id: me.id, status: "online" }],
    ...over,
  };
}

export function makeReady(me: User, guilds: GuildState[] = []): GatewayReady {
  return { user: me, guilds, presences: [{ user_id: me.id, status: "online" }] };
}
