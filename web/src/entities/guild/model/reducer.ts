import { type ChatEvents, type components, type GatewayEvents, parseChatEvent, parseEvent } from "@/shared/api";

type S = components["schemas"];
export type User = S["User"];
export type Guild = S["Guild"];
export type Role = S["Role"];
export type Channel = S["Channel"];
export type Member = S["Member"];
export type GuildState = S["GuildState"];
/** A custom emoji of a server. */
export type GuildEmoji = S["GuildEmoji"];
export type ReadyPayload = S["GatewayReady"];
export type PresenceStatus = S["Presence"]["status"];
/**
 * The user's tie to someone else: a friend, someone they blocked, or a
 * friend request either way (`RELATIONSHIP`).
 */
export type Relationship = S["Relationship"];

/** A direct message (type 1) or group DM (type 3) as the user sees it; `recipients` are everyone else. */
export type DMChannel = S["DMChannel"];

/** Private channel types, as Discord numbers them. */
export const PRIVATE_TYPE = { DM: 1, GROUP: 3 } as const;

/** One open direct message or group the user is in, with how far they read it. */
export interface PrivateEntry {
  channel: DMChannel;
  /** Null before the user read anything there. */
  read: ReadMark | null;
}

/** Relationship types, as Discord numbers them. */
export const RELATIONSHIP = { FRIEND: 1, BLOCKED: 2, INCOMING: 3, OUTGOING: 4 } as const;

/** Someone in a voice channel; `left` exists only on the event. */
export type VoiceState = Omit<S["VoiceState"], "left">;

/** How far the user has read a channel, and how many unread messages there mention them. */
export interface ReadMark {
  /** The newest message read; "0" before the first. */
  lastReadId: string;
  mentions: number;
}

/** Whether snowflake `a` is newer than `b` (decimal strings, compared as numbers). */
export const newer = (a: string, b: string): boolean => BigInt(a) > BigInt(b);

/** One server as the signed-in user sees it, keyed by ID for upserts. */
export interface GuildEntry {
  guild: Guild;
  roles: Record<string, Role>;
  channels: Record<string, Channel>;
  /** The members the server sent (the first 1000) plus `myMember`. */
  members: Record<string, Member>;
  myMember: Member;
  /** Who is in which voice channel of the server, by user ID (a user is in one at most). */
  voice: Record<string, VoiceState>;
  /**
   * Read marks by channel ID. A channel without one was never read (it was
   * created, or became visible, after the user joined).
   */
  reads: Record<string, ReadMark>;
  /** The server's custom emoji by ID, in the order the server lists them. */
  emojis: Record<string, GuildEmoji>;
}

/** Everything the gateway tells about the user's servers. */
export interface GuildsState {
  me: User | null;
  guilds: Record<string, GuildEntry>;
  /** Server IDs in the order of READY, then of joining. */
  order: string[];
  /** Users seen online or offline; anyone missing is offline. */
  presences: Record<string, PresenceStatus>;
  /** The user's friends, blocks and requests, by the other user's ID. */
  relationships: Record<string, Relationship>;
  /** The user's open direct messages and their groups, by channel ID. */
  privates: Record<string, PrivateEntry>;
}

export const EMPTY_GUILDS: GuildsState = {
  me: null,
  guilds: {},
  order: [],
  presences: {},
  relationships: {},
  privates: {},
};

const byId = <T extends { id: string }>(items: T[]): Record<string, T> =>
  Object.fromEntries(items.map((item) => [item.id, item]));

function entryOf(g: GuildState): GuildEntry {
  const members: Record<string, Member> = Object.fromEntries(g.members.map((m) => [m.user.id, m]));
  members[g.my_member.user.id] = g.my_member;
  const voice = Object.fromEntries(g.voice_states.map(({ left: _left, ...v }) => [v.user_id, v]));
  const reads = Object.fromEntries(
    g.read_states.map((r) => [r.channel_id, { lastReadId: r.last_read_id, mentions: r.mention_count }]),
  );
  return {
    guild: g.guild,
    roles: byId(g.roles),
    channels: byId(g.channels),
    members,
    myMember: g.my_member,
    voice,
    reads,
    emojis: byId(g.emojis),
  };
}

/**
 * The presences a GuildState tells: its `presences` lists every online
 * member of the server, so a member it holds who is not listed is offline.
 */
function presencesOf(g: GuildState, into: Record<string, PresenceStatus>): Record<string, PresenceStatus> {
  const online = new Set(g.presences.filter((p) => p.status === "online").map((p) => p.user_id));
  const next = { ...into };
  for (const m of [...g.members, g.my_member]) next[m.user.id] = online.has(m.user.id) ? "online" : "offline";
  for (const p of g.presences) next[p.user_id] = p.status;
  return next;
}

/** The store as READY describes it, replacing anything held before. */
export function applyReady(d: ReadyPayload): GuildsState {
  const guilds: Record<string, GuildEntry> = {};
  const order: string[] = [];
  let presences: Record<string, PresenceStatus> = {};
  for (const g of d.guilds) {
    if (!(g.guild.id in guilds)) order.push(g.guild.id);
    guilds[g.guild.id] = entryOf(g);
    presences = presencesOf(g, presences);
  }
  for (const p of d.presences) presences[p.user_id] = p.status;
  // READY's own read_states hold the marks of the private channels too.
  const marks = new Map(d.read_states.map((r) => [r.channel_id, r]));
  const privates: Record<string, PrivateEntry> = {};
  for (const channel of d.private_channels) {
    const r = marks.get(channel.id);
    privates[channel.id] = { channel, read: r ? { lastReadId: r.last_read_id, mentions: r.mention_count } : null };
  }
  return { me: d.user, guilds, order, presences, relationships: byId(d.relationships), privates };
}

/** Changes private channel `id` with `fn`; one not held is left alone. */
function updatePrivate(s: GuildsState, id: string, fn: (p: PrivateEntry) => PrivateEntry): GuildsState {
  const entry = s.privates[id];
  if (!entry) return s;
  const next = fn(entry);
  return next === entry ? s : { ...s, privates: { ...s.privates, [id]: next } };
}

/** A private channel opened, reopened or changed: its read mark stays. */
function upsertPrivate(s: GuildsState, channel: DMChannel): GuildsState {
  const held = s.privates[channel.id];
  return { ...s, privates: { ...s.privates, [channel.id]: { channel, read: held?.read ?? null } } };
}

/** A server whole (GUILD_CREATE, GUILD_SYNC): replaced if held, else added at the end. */
function upsertGuild(s: GuildsState, g: GuildState): GuildsState {
  const id = g.guild.id;
  return {
    ...s,
    guilds: { ...s.guilds, [id]: entryOf(g) },
    order: s.order.includes(id) ? s.order : [...s.order, id],
    presences: presencesOf(g, s.presences),
  };
}

function removeGuild(s: GuildsState, id: string): GuildsState {
  if (!(id in s.guilds)) return s;
  const { [id]: _removed, ...guilds } = s.guilds;
  return { ...s, guilds, order: s.order.filter((g) => g !== id) };
}

/** Changes the entry of server `id` with `fn`; a server not held is left alone. */
function updateGuild(s: GuildsState, id: string, fn: (e: GuildEntry) => GuildEntry): GuildsState {
  const entry = s.guilds[id];
  if (!entry) return s;
  const next = fn(entry);
  return next === entry ? s : { ...s, guilds: { ...s.guilds, [id]: next } };
}

function without<T>(record: Record<string, T>, id: string): Record<string, T> {
  const { [id]: _removed, ...rest } = record;
  return rest;
}

function upsertMember(e: GuildEntry, m: Member): GuildEntry {
  const mine = m.user.id === e.myMember.user.id;
  return { ...e, members: { ...e.members, [m.user.id]: m }, myMember: mine ? m : e.myMember };
}

function withoutRole(m: Member, roleId: string): Member {
  return m.roles.includes(roleId) ? { ...m, roles: m.roles.filter((r) => r !== roleId) } : m;
}

function renameUser(e: GuildEntry, user: User): GuildEntry {
  const member = e.members[user.id];
  const mine = e.myMember.user.id === user.id;
  if (!member && !mine) return e;
  return {
    ...e,
    members: member ? { ...e.members, [user.id]: { ...member, user } } : e.members,
    myMember: mine ? { ...e.myMember, user } : e.myMember,
  };
}

/** The server that holds channel `channelId`, if any. */
const guildOfChannel = (s: GuildsState, channelId: string): string | undefined =>
  s.order.find((id) => s.guilds[id]?.channels[channelId] !== undefined);

/**
 * Moves the read mark of `channelId` to `mark`, never back: an older mark
 * (an ack that crossed a newer one) changes nothing.
 */
export function advanceRead(s: GuildsState, channelId: string, mark: ReadMark): GuildsState {
  const moved = (held: ReadMark | undefined | null): boolean => {
    if (held && newer(held.lastReadId, mark.lastReadId)) return false;
    return !(held?.lastReadId === mark.lastReadId && held.mentions === mark.mentions);
  };
  if (channelId in s.privates) return updatePrivate(s, channelId, (p) => (moved(p.read) ? { ...p, read: mark } : p));
  const guildId = guildOfChannel(s, channelId);
  if (guildId === undefined) return s;
  return updateGuild(s, guildId, (e) =>
    moved(e.reads[channelId]) ? { ...e, reads: { ...e.reads, [channelId]: mark } } : e,
  );
}

type NewMessage = ChatEvents["MESSAGE_CREATE"];

/** Whether `m` mentions the user of `e`: by ID, by one of their roles, or everyone. */
export const mentionsMe = (
  e: GuildEntry,
  m: Pick<NewMessage, "mention_everyone" | "mention_user_ids" | "mention_role_ids">,
): boolean =>
  m.mention_everyone ||
  m.mention_user_ids.includes(e.myMember.user.id) ||
  m.mention_role_ids.some((r) => e.myMember.roles.includes(r));

/**
 * A new message becomes its channel's newest. The user's own message is
 * read (the server marks it so); one that mentions them adds to the
 * channel's mentions. A message not newer than the channel's newest was
 * already counted, so a repeated event changes nothing.
 */
function messageCreated(s: GuildsState, m: NewMessage): GuildsState {
  if (m.guild_id === null) return privateMessageCreated(s, m);
  const e = s.guilds[m.guild_id];
  const channel = e?.channels[m.channel_id];
  if (!e || !channel) return s;
  if (channel.last_message_id !== null && !newer(m.id, channel.last_message_id)) return s;
  const channels = { ...e.channels, [channel.id]: { ...channel, last_message_id: m.id } };
  let reads = e.reads;
  if (m.author?.id === e.myMember.user.id) {
    reads = { ...reads, [channel.id]: { lastReadId: m.id, mentions: 0 } };
  } else if (mentionsMe(e, m)) {
    const held = reads[channel.id];
    reads = { ...reads, [channel.id]: { lastReadId: held?.lastReadId ?? "0", mentions: (held?.mentions ?? 0) + 1 } };
  }
  return { ...s, guilds: { ...s.guilds, [e.guild.id]: { ...e, channels, reads } } };
}

/**
 * A new message in a private channel. As in Discord, every message from
 * someone else counts like a mention (the badge counts messages); the
 * user's own reads the channel.
 */
function privateMessageCreated(s: GuildsState, m: NewMessage): GuildsState {
  return updatePrivate(s, m.channel_id, (p) => {
    const last = p.channel.last_message_id;
    if (last !== null && !newer(m.id, last)) return p;
    const channel = { ...p.channel, last_message_id: m.id };
    if (m.author !== null && m.author.id === s.me?.id) return { channel, read: { lastReadId: m.id, mentions: 0 } };
    return { channel, read: { lastReadId: p.read?.lastReadId ?? "0", mentions: (p.read?.mentions ?? 0) + 1 } };
  });
}

/** A deleted message may have been the channel's newest: the event says which one is now. */
function messageDeleted(s: GuildsState, d: ChatEvents["MESSAGE_DELETE"]): GuildsState {
  if (d.guild_id === null) {
    return updatePrivate(s, d.channel_id, (p) =>
      p.channel.last_message_id === d.last_message_id
        ? p
        : { ...p, channel: { ...p.channel, last_message_id: d.last_message_id } },
    );
  }
  const guildId = d.guild_id;
  const channel = s.guilds[guildId]?.channels[d.channel_id];
  if (!channel || channel.last_message_id === d.last_message_id) return s;
  return updateGuild(s, guildId, (e) => ({
    ...e,
    channels: { ...e.channels, [channel.id]: { ...channel, last_message_id: d.last_message_id } },
  }));
}

/** The events the store applies, with their payloads already checked. */
type Handlers = { [K in Exclude<keyof GatewayEvents, "READY">]: (s: GuildsState, d: GatewayEvents[K]) => GuildsState };

const handlers: Handlers = {
  GUILD_CREATE: upsertGuild,
  GUILD_SYNC: upsertGuild,
  GUILD_UPDATE: (s, guild) => updateGuild(s, guild.id, (e) => ({ ...e, guild })),
  GUILD_DELETE: (s, d) => removeGuild(s, d.id),
  // The whole list after any change, as Discord sends it.
  GUILD_EMOJIS_UPDATE: (s, d) => updateGuild(s, d.guild_id, (e) => ({ ...e, emojis: byId(d.emojis) })),
  GUILD_MEMBER_ADD: (s, d) => updateGuild(s, d.guild_id, (e) => upsertMember(e, d.member)),
  GUILD_MEMBER_UPDATE: (s, d) => updateGuild(s, d.guild_id, (e) => upsertMember(e, d.member)),
  GUILD_MEMBER_REMOVE: (s, d) => {
    const entry = s.guilds[d.guild_id];
    if (!entry) return s;
    // The user left, was kicked or banned: the server is gone for them.
    if (d.user.id === entry.myMember.user.id) return removeGuild(s, d.guild_id);
    return updateGuild(s, d.guild_id, (e) => ({
      ...e,
      members: without(e.members, d.user.id),
      voice: without(e.voice, d.user.id),
    }));
  },
  GUILD_ROLE_CREATE: (s, d) =>
    updateGuild(s, d.guild_id, (e) => ({ ...e, roles: { ...e.roles, [d.role.id]: d.role } })),
  GUILD_ROLE_UPDATE: (s, d) =>
    updateGuild(s, d.guild_id, (e) => ({ ...e, roles: { ...e.roles, [d.role.id]: d.role } })),
  GUILD_ROLE_DELETE: (s, d) =>
    updateGuild(s, d.guild_id, (e) => ({
      ...e,
      roles: without(e.roles, d.role_id),
      members: Object.fromEntries(Object.entries(e.members).map(([id, m]) => [id, withoutRole(m, d.role_id)])),
      myMember: withoutRole(e.myMember, d.role_id),
    })),
  CHANNEL_CREATE: (s, c) => updateGuild(s, c.guild_id, (e) => ({ ...e, channels: { ...e.channels, [c.id]: c } })),
  CHANNEL_UPDATE: (s, c) => updateGuild(s, c.guild_id, (e) => ({ ...e, channels: { ...e.channels, [c.id]: c } })),
  CHANNEL_DELETE: (s, c) =>
    updateGuild(s, c.guild_id, (e) => ({
      ...e,
      channels: without(e.channels, c.id),
      reads: without(e.reads, c.id),
      voice: Object.fromEntries(Object.entries(e.voice).filter(([, v]) => v.channel_id !== c.id)),
    })),
  MESSAGE_ACK: (s, r) => advanceRead(s, r.channel_id, { lastReadId: r.last_read_id, mentions: r.mention_count }),
  PRESENCE_UPDATE: (s, p) => ({ ...s, presences: { ...s.presences, [p.user_id]: p.status } }),
  USER_UPDATE: (s, user) => {
    const held = s.relationships[user.id];
    return {
      ...s,
      me: s.me?.id === user.id ? user : s.me,
      guilds: Object.fromEntries(Object.entries(s.guilds).map(([id, e]) => [id, renameUser(e, user)])),
      relationships: held ? { ...s.relationships, [user.id]: { ...held, user } } : s.relationships,
      privates: Object.fromEntries(
        Object.entries(s.privates).map(([id, p]) => [
          id,
          p.channel.recipients.some((r) => r.id === user.id)
            ? {
                ...p,
                channel: { ...p.channel, recipients: p.channel.recipients.map((r) => (r.id === user.id ? user : r)) },
              }
            : p,
        ]),
      ),
    };
  },
  DM_CHANNEL_CREATE: upsertPrivate,
  DM_CHANNEL_UPDATE: upsertPrivate,
  DM_CHANNEL_DELETE: (s, d) => (d.id in s.privates ? { ...s, privates: without(s.privates, d.id) } : s),
  RELATIONSHIP_ADD: (s, r) => ({ ...s, relationships: { ...s.relationships, [r.id]: r } }),
  // Only the tie the event names: a request accepted meanwhile is a friendship now, and stays.
  RELATIONSHIP_REMOVE: (s, d) =>
    s.relationships[d.id]?.type === d.type ? { ...s, relationships: without(s.relationships, d.id) } : s,
  VOICE_STATE_UPDATE: (s, { left, ...v }) =>
    updateGuild(s, v.guild_id, (e) => {
      if (!left) return { ...e, voice: { ...e.voice, [v.user_id]: v } };
      // A leave of a channel the user already moved out of changes nothing.
      return e.voice[v.user_id]?.channel_id === v.channel_id ? { ...e, voice: without(e.voice, v.user_id) } : e;
    }),
};

const isHandled = (t: string): t is keyof Handlers => Object.hasOwn(handlers, t);

/**
 * The store after DISPATCH `t` with payload `d`. Pure; idempotent (upserts
 * and deletes by ID), so an event already reflected in READY changes
 * nothing. Of messages it keeps only what unread needs: each channel's
 * newest message and the user's read marks. An event the store does not hold (typing, bans,
 * anything unknown) leaves the state as it was, and so does a payload that
 * does not match the contract (logged).
 */
export function applyEvent(s: GuildsState, t: string, d: unknown): GuildsState {
  if (t === "MESSAGE_CREATE") {
    const m = parseChatEvent(t, d);
    return m ? messageCreated(s, m) : s;
  }
  if (t === "MESSAGE_DELETE") {
    const m = parseChatEvent(t, d);
    return m ? messageDeleted(s, m) : s;
  }
  if (!isHandled(t)) return s;
  const payload = parseEvent(t, d);
  if (payload === null) return s;
  return run(s, t, payload);
}

/** Handler `t` on a payload that parseEvent checked for `t`. */
const run = <K extends keyof Handlers>(s: GuildsState, t: K, payload: GatewayEvents[K]): GuildsState => {
  const handler: (s: GuildsState, d: GatewayEvents[K]) => GuildsState = handlers[t];
  return handler(s, payload);
};
