import { type components, type GatewayEvents, parseEvent } from "@/shared/api";

type S = components["schemas"];
export type User = S["User"];
export type Guild = S["Guild"];
export type Role = S["Role"];
export type Channel = S["Channel"];
export type Member = S["Member"];
export type GuildState = S["GuildState"];
export type ReadyPayload = S["GatewayReady"];
export type PresenceStatus = S["Presence"]["status"];
/** Someone in a voice channel; `left` exists only on the event. */
export type VoiceState = Omit<S["VoiceState"], "left">;

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
}

/** Everything the gateway tells about the user's servers. */
export interface GuildsState {
  me: User | null;
  guilds: Record<string, GuildEntry>;
  /** Server IDs in the order of READY, then of joining. */
  order: string[];
  /** Users seen online or offline; anyone missing is offline. */
  presences: Record<string, PresenceStatus>;
}

export const EMPTY_GUILDS: GuildsState = { me: null, guilds: {}, order: [], presences: {} };

const byId = <T extends { id: string }>(items: T[]): Record<string, T> =>
  Object.fromEntries(items.map((item) => [item.id, item]));

function entryOf(g: GuildState): GuildEntry {
  const members: Record<string, Member> = Object.fromEntries(g.members.map((m) => [m.user.id, m]));
  members[g.my_member.user.id] = g.my_member;
  const voice = Object.fromEntries(g.voice_states.map(({ left: _left, ...v }) => [v.user_id, v]));
  return { guild: g.guild, roles: byId(g.roles), channels: byId(g.channels), members, myMember: g.my_member, voice };
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
  return { me: d.user, guilds, order, presences };
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
  return { ...s, guilds: { ...s.guilds, [id]: fn(entry) } };
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

/** The events the store applies, with their payloads already checked. */
type Handlers = { [K in Exclude<keyof GatewayEvents, "READY">]: (s: GuildsState, d: GatewayEvents[K]) => GuildsState };

const handlers: Handlers = {
  GUILD_CREATE: upsertGuild,
  GUILD_SYNC: upsertGuild,
  GUILD_UPDATE: (s, guild) => updateGuild(s, guild.id, (e) => ({ ...e, guild })),
  GUILD_DELETE: (s, d) => removeGuild(s, d.id),
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
      voice: Object.fromEntries(Object.entries(e.voice).filter(([, v]) => v.channel_id !== c.id)),
    })),
  PRESENCE_UPDATE: (s, p) => ({ ...s, presences: { ...s.presences, [p.user_id]: p.status } }),
  USER_UPDATE: (s, user) => ({
    ...s,
    me: s.me?.id === user.id ? user : s.me,
    guilds: Object.fromEntries(Object.entries(s.guilds).map(([id, e]) => [id, renameUser(e, user)])),
  }),
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
 * nothing. An event the store does not hold (messages, typing, bans,
 * anything unknown) leaves the state as it was, and so does a payload that
 * does not match the contract (logged).
 */
export function applyEvent(s: GuildsState, t: string, d: unknown): GuildsState {
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
