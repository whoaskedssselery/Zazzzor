import type { components } from "@/shared/api";

type S = components["schemas"];
type User = S["User"];
type Guild = S["Guild"];
type Role = S["Role"];
type Channel = S["Channel"];
type Member = S["Member"];
type GuildState = S["GuildState"];
type GatewayReady = S["GatewayReady"];
type Message = S["Message"];

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

/** The @everyone role of a server: its ID is the server's, position 0, the permissions of a new server (DEFAULT_EVERYONE). */
export function makeEveryone(guildId: string, permissions = "104042049"): Role {
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
    last_message_id: null,
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
    voice_states: [],
    read_states: [],
    emojis: [],
    ...over,
  };
}

/** Custom emoji `name` of server `guildId`. */
export function makeGuildEmoji(id: string, guildId: string, name: string, over: Partial<S["GuildEmoji"]> = {}) {
  return {
    id,
    guild_id: guildId,
    name,
    animated: false,
    url: `/api/v1/emojis/${id}`,
    creator_id: null,
    created_at: AT,
    ...over,
  } satisfies S["GuildEmoji"];
}

export function makeReady(me: User, guilds: GuildState[] = []): GatewayReady {
  return {
    user: me,
    guilds,
    presences: [{ user_id: me.id, status: "online" }],
    read_states: guilds.flatMap((g) => g.read_states),
    relationships: [],
  };
}

/** `user` as the signed-in user's friend (1), block (2), incoming (3) or outgoing (4) request. */
export function makeRelationship(user: User, type: 1 | 2 | 3 | 4 = 1): S["Relationship"] {
  return { id: user.id, type, user, since: AT };
}

/** When message `id` was sent: one second per ID unit after 23.10.2026 18:00 UTC, so IDs in order are seconds apart. */
const messageTime = (id: string): string =>
  new Date(Date.UTC(2026, 9, 23, 18) + (Number(id) % 1_000_000) * 1000).toISOString();

/** A message in channel `channelId` of server 10 by `author` (null: a deleted account). */
export function makeMessage(id: string, channelId: string, author: User | null, over: Partial<Message> = {}): Message {
  return {
    id,
    channel_id: channelId,
    guild_id: "10",
    author,
    content: `сообщение ${id}`,
    created_at: messageTime(id),
    edited_at: null,
    reply_to_id: null,
    referenced_message: null,
    embeds: [],
    mention_user_ids: [],
    mention_role_ids: [],
    mention_everyone: false,
    mention_here: false,
    ...over,
    attachments: over.attachments ?? [],
  };
}

/** `userId` in voice channel `channelId`, nothing muted, joined at 18:00 UTC 23.10.2026 unless given. */
export function makeVoiceState(
  guildId: string,
  channelId: string,
  userId: string,
  over: Partial<S["VoiceState"]> = {},
): S["VoiceState"] {
  return {
    guild_id: guildId,
    channel_id: channelId,
    user_id: userId,
    self_mute: false,
    self_deaf: false,
    self_stream: false,
    mute: false,
    deaf: false,
    joined_at: "2026-10-23T18:00:00.000Z",
    ...over,
  };
}
