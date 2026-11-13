import { z } from "zod/mini";
import type { components } from "../schema.gen";

// The one decoding boundary of the gateway (api/openapi.yaml, `x-gateway`).
// Everything the server sends is checked here against the contract's shapes
// before the rest of the app sees it as typed data: a frame that does not
// match is logged and dropped, never thrown. The schemas are typed with the
// generated types, so a contract change that they miss fails typecheck.

type S = components["schemas"];

/** Frame ops (`x-gateway.ops`). */
export const OP = { DISPATCH: 0, HEARTBEAT: 1, HELLO: 10, HEARTBEAT_ACK: 11 } as const;

/** Close codes of `x-gateway.close_codes` that the client treats specially. */
export const CLOSE = {
  DECODE_ERROR: 4002,
  AUTHENTICATION_FAILED: 4004,
  OVERLOADED: 4008,
  SESSION_TIMEOUT: 4009,
} as const;

/** The only frame a client sends (`d` is ignored by the server). */
export const HEARTBEAT_FRAME = JSON.stringify({ op: OP.HEARTBEAT, d: null });

/** A server frame after decoding: DISPATCH with its event, HELLO with its interval, or an ACK. */
export type ServerFrame =
  | { op: typeof OP.DISPATCH; t: string; s: number; d: unknown }
  | { op: typeof OP.HELLO; d: S["GatewayHello"] }
  | { op: typeof OP.HEARTBEAT_ACK };

/** setInterval treats anything above this as 0 and would heartbeat in a loop. */
const MAX_TIMER_MS = 2 ** 31 - 1;

const helloSchema: z.ZodMiniType<S["GatewayHello"]> = z.object({
  heartbeat_interval_ms: z.number().check(z.positive(), z.lte(MAX_TIMER_MS)),
});

const frameSchema = z.discriminatedUnion("op", [
  z.object({
    op: z.literal(OP.DISPATCH),
    t: z.string().check(z.minLength(1)),
    s: z.int().check(z.positive()),
    d: z.unknown(),
  }),
  z.object({ op: z.literal(OP.HELLO), d: helloSchema }),
  z.object({ op: z.literal(OP.HEARTBEAT_ACK) }),
]);

const parseJson = (text: string): { ok: true; value: unknown } | { ok: false } => {
  try {
    return { ok: true, value: JSON.parse(text) };
  } catch {
    return { ok: false };
  }
};

/**
 * Decodes the data of one socket message into a server frame, or returns
 * null (after a console warning) for anything the contract does not allow:
 * a binary frame, invalid JSON, a missing or unknown op, a DISPATCH without
 * `t` or `s`, a HELLO without a usable interval.
 */
export function decodeFrame(data: unknown): ServerFrame | null {
  if (typeof data !== "string") {
    console.warn("gateway: dropped a binary frame");
    return null;
  }
  const json = parseJson(data);
  if (!json.ok) {
    console.warn("gateway: dropped a frame that is not JSON");
    return null;
  }
  const frame = frameSchema.safeParse(json.value);
  if (!frame.success) {
    console.warn("gateway: dropped a malformed frame", frame.error.issues);
    return null;
  }
  const f = frame.data;
  switch (f.op) {
    case OP.DISPATCH:
      return { op: f.op, t: f.t, s: f.s, d: f.d };
    case OP.HELLO:
      return { op: f.op, d: f.d };
    case OP.HEARTBEAT_ACK:
      return { op: f.op };
  }
}

// Event payloads (`x-gateway.events`) the guild store applies. Messages and
// typing have their own map below (ChatEvents); the guild store also reads
// MESSAGE_CREATE and MESSAGE_DELETE from it, for unread channels.

/** A snowflake: a decimal string, kept a string. */
const snowflake = z.string().check(z.regex(/^\d{1,20}$/));
/** A permission mask: a decimal string without leading zeros (its bits are read as bigint). */
const permission = z.string().check(z.regex(/^(0|[1-9]\d{0,19})$/));
const nullableString = z.nullable(z.string());

const userSchema: z.ZodMiniType<S["User"]> = z.object({
  id: snowflake,
  username: z.string(),
  display_name: z.string(),
  avatar_url: nullableString,
  created_at: z.string(),
});

const chosenStatus = z.enum(["online", "idle", "dnd", "invisible"]);

const currentUserSchema: z.ZodMiniType<S["CurrentUser"]> = z.object({
  id: snowflake,
  username: z.string(),
  display_name: z.string(),
  avatar_url: nullableString,
  created_at: z.string(),
  bio: z.string(),
  status: chosenStatus,
  custom_status: nullableString,
});

const guildSchema: z.ZodMiniType<S["Guild"]> = z.object({
  id: snowflake,
  name: z.string(),
  icon_url: nullableString,
  owner_id: snowflake,
  created_at: z.string(),
});

const roleSchema: z.ZodMiniType<S["Role"]> = z.object({
  id: snowflake,
  name: z.string(),
  color: z.int(),
  position: z.int(),
  permissions: permission,
  hoist: z.boolean(),
  mentionable: z.boolean(),
});

const overwriteSchema: z.ZodMiniType<S["Overwrite"]> = z.object({
  id: snowflake,
  type: z.union([z.literal(0), z.literal(1)]),
  allow: permission,
  deny: permission,
});

const channelSchema: z.ZodMiniType<S["Channel"]> = z.object({
  id: snowflake,
  guild_id: snowflake,
  type: z.union([z.literal(0), z.literal(2), z.literal(4)]),
  name: z.string(),
  topic: nullableString,
  parent_id: z.nullable(snowflake),
  position: z.int(),
  permission_synced: z.boolean(),
  permission_overwrites: z.array(overwriteSchema),
  last_message_id: z.nullable(snowflake),
});

const memberSchema: z.ZodMiniType<S["Member"]> = z.object({
  user: userSchema,
  nick: nullableString,
  roles: z.array(snowflake),
  joined_at: z.string(),
});

const presenceSchema: z.ZodMiniType<S["Presence"]> = z.object({
  user_id: snowflake,
  status: z.enum(["online", "idle", "dnd", "offline"]),
  custom_status: nullableString,
});

const voiceStateSchema: z.ZodMiniType<S["VoiceState"]> = z.object({
  guild_id: z.nullable(snowflake),
  channel_id: snowflake,
  user_id: snowflake,
  self_mute: z.boolean(),
  self_deaf: z.boolean(),
  self_stream: z.boolean(),
  self_video: z.boolean(),
  mute: z.boolean(),
  deaf: z.boolean(),
  joined_at: z.string(),
  left: z.optional(z.boolean()),
});

const readStateSchema: z.ZodMiniType<S["ReadState"]> = z.object({
  channel_id: snowflake,
  last_read_id: snowflake,
  mention_count: z.int().check(z.nonnegative()),
});

const guildEmojiSchema: z.ZodMiniType<S["GuildEmoji"]> = z.object({
  id: snowflake,
  guild_id: snowflake,
  name: z.string(),
  animated: z.boolean(),
  url: z.string(),
  creator_id: z.nullable(snowflake),
  created_at: z.string(),
});

const guildStateSchema: z.ZodMiniType<S["GuildState"]> = z.object({
  emojis: z.array(guildEmojiSchema),
  guild: guildSchema,
  roles: z.array(roleSchema),
  channels: z.array(channelSchema),
  members: z.array(memberSchema),
  my_member: memberSchema,
  presences: z.array(presenceSchema),
  voice_states: z.array(voiceStateSchema),
  read_states: z.array(readStateSchema),
});

const relationshipType = z.union([z.literal(1), z.literal(2), z.literal(3), z.literal(4)]);

const relationshipSchema: z.ZodMiniType<S["Relationship"]> = z.object({
  id: snowflake,
  type: relationshipType,
  user: userSchema,
  since: z.string(),
});

/** A direct message or group DM as the receiving user sees it (recipients: everyone else). */
const dmChannelSchema: z.ZodMiniType<S["DMChannel"]> = z.object({
  id: snowflake,
  type: z.union([z.literal(1), z.literal(3)]),
  name: nullableString,
  owner_id: z.nullable(snowflake),
  recipients: z.array(userSchema),
  last_message_id: z.nullable(snowflake),
});

const channelOverrideSchema: z.ZodMiniType<S["ChannelOverride"]> = z.object({
  channel_id: snowflake,
  muted: z.boolean(),
  mute_until: nullableString,
  message_notifications: z.union([z.literal(0), z.literal(1), z.literal(2), z.literal(3)]),
});

/** The user's notification settings of a server, or of the private channels (guild_id null). */
const userGuildSettingsSchema: z.ZodMiniType<S["UserGuildSettings"]> = z.object({
  guild_id: z.nullable(snowflake),
  version: z.number(),
  muted: z.boolean(),
  mute_until: nullableString,
  message_notifications: z.union([z.literal(0), z.literal(1), z.literal(2)]),
  suppress_everyone: z.boolean(),
  channel_overrides: z.array(channelOverrideSchema),
});

/** A call going on in a DM or a group DM, and whom it rings. */
const callSchema: z.ZodMiniType<S["Call"]> = z.object({ channel_id: snowflake, ringing: z.array(snowflake) });

/** The payload type of each event the guild store applies (`x-gateway.events`). */
export interface GatewayEvents {
  READY: S["GatewayReady"];
  GUILD_CREATE: S["GuildState"];
  GUILD_SYNC: S["GuildState"];
  GUILD_UPDATE: S["Guild"];
  GUILD_DELETE: S["GuildDeleteEvent"];
  GUILD_EMOJIS_UPDATE: S["GuildEmojisUpdate"];
  GUILD_MEMBER_ADD: S["MemberEvent"];
  GUILD_MEMBER_UPDATE: S["MemberEvent"];
  GUILD_MEMBER_REMOVE: S["MemberRemoveEvent"];
  GUILD_ROLE_CREATE: S["RoleEvent"];
  GUILD_ROLE_UPDATE: S["RoleEvent"];
  GUILD_ROLE_DELETE: S["RoleDeleteEvent"];
  CHANNEL_CREATE: S["Channel"];
  CHANNEL_UPDATE: S["Channel"];
  CHANNEL_DELETE: S["Channel"];
  PRESENCE_UPDATE: S["Presence"];
  USER_UPDATE: S["CurrentUser"] | S["User"];
  RELATIONSHIP_ADD: S["Relationship"];
  RELATIONSHIP_REMOVE: S["RelationshipRemove"];
  DM_CHANNEL_CREATE: S["DMChannel"];
  DM_CHANNEL_UPDATE: S["DMChannel"];
  DM_CHANNEL_DELETE: S["DMChannelDeleteEvent"];
  USER_GUILD_SETTINGS_UPDATE: S["UserGuildSettings"];
  CALL_CREATE: S["Call"];
  CALL_UPDATE: S["Call"];
  CALL_DELETE: S["CallDeleteEvent"];
  VOICE_STATE_UPDATE: S["VoiceState"];
  MESSAGE_ACK: S["ReadState"];
}

const memberEventSchema: z.ZodMiniType<S["MemberEvent"]> = z.object({ guild_id: snowflake, member: memberSchema });
const roleEventSchema: z.ZodMiniType<S["RoleEvent"]> = z.object({ guild_id: snowflake, role: roleSchema });

const eventSchemas: { [K in keyof GatewayEvents]: z.ZodMiniType<GatewayEvents[K]> } = {
  READY: z.object({
    user: currentUserSchema,
    guilds: z.array(guildStateSchema),
    presences: z.array(presenceSchema),
    read_states: z.array(readStateSchema),
    relationships: z.array(relationshipSchema),
    private_channels: z.array(dmChannelSchema),
    user_guild_settings: z.array(userGuildSettingsSchema),
    private_voice_states: z.array(voiceStateSchema),
    calls: z.array(callSchema),
  }),
  GUILD_CREATE: guildStateSchema,
  GUILD_SYNC: guildStateSchema,
  GUILD_UPDATE: guildSchema,
  GUILD_DELETE: z.object({ id: snowflake }),
  GUILD_EMOJIS_UPDATE: z.object({ guild_id: snowflake, emojis: z.array(guildEmojiSchema) }),
  GUILD_MEMBER_ADD: memberEventSchema,
  GUILD_MEMBER_UPDATE: memberEventSchema,
  GUILD_MEMBER_REMOVE: z.object({ guild_id: snowflake, user: userSchema }),
  GUILD_ROLE_CREATE: roleEventSchema,
  GUILD_ROLE_UPDATE: roleEventSchema,
  GUILD_ROLE_DELETE: z.object({ guild_id: snowflake, role_id: snowflake }),
  CHANNEL_CREATE: channelSchema,
  CHANNEL_UPDATE: channelSchema,
  CHANNEL_DELETE: channelSchema,
  PRESENCE_UPDATE: presenceSchema,
  // The user's own account comes with the fields only they see.
  USER_UPDATE: z.union([currentUserSchema, userSchema]),
  RELATIONSHIP_ADD: relationshipSchema,
  RELATIONSHIP_REMOVE: z.object({ id: snowflake, type: relationshipType }),
  DM_CHANNEL_CREATE: dmChannelSchema,
  DM_CHANNEL_UPDATE: dmChannelSchema,
  DM_CHANNEL_DELETE: z.object({ id: snowflake }),
  USER_GUILD_SETTINGS_UPDATE: userGuildSettingsSchema,
  CALL_CREATE: callSchema,
  CALL_UPDATE: callSchema,
  CALL_DELETE: z.object({ channel_id: snowflake }),
  VOICE_STATE_UPDATE: voiceStateSchema,
  MESSAGE_ACK: readStateSchema,
};

/**
 * The payload `d` of event `t` checked against the contract, or null (after
 * a console warning) when it does not match.
 */
export function parseEvent<K extends keyof GatewayEvents>(t: K, d: unknown): GatewayEvents[K] | null {
  const result = eventSchemas[t].safeParse(d);
  if (result.success) return result.data;
  console.warn(`gateway: dropped a malformed ${t}`, result.error.issues);
  return null;
}

// Chat events: the message cache and the typing indicator apply them, the
// guild store never does.

const messageStubSchema: z.ZodMiniType<S["MessageStub"]> = z.object({
  id: snowflake,
  author: z.nullable(userSchema),
  content: z.string(),
});

const attachmentSchema: z.ZodMiniType<S["Attachment"]> = z.object({
  id: snowflake,
  filename: z.string(),
  content_type: z.string(),
  size: z.number(),
  width: z.nullable(z.number()),
  height: z.nullable(z.number()),
  url: z.string(),
  pending: z.boolean(),
});

const emojiSchema: z.ZodMiniType<S["Emoji"]> = z.object({ id: z.nullable(snowflake), name: z.string() });

const reactionSchema: z.ZodMiniType<S["Reaction"]> = z.object({
  emoji: emojiSchema,
  count: z.int().check(z.positive()),
  me: z.boolean(),
});

const embedMediaSchema: z.ZodMiniType<S["EmbedMedia"]> = z.object({
  url: z.string(),
  width: z.int().check(z.positive()),
  height: z.int().check(z.positive()),
});

const embedSchema: z.ZodMiniType<S["Embed"]> = z.object({
  url: z.string(),
  type: z.enum(["link", "image", "video"]),
  site_name: nullableString,
  title: nullableString,
  description: nullableString,
  image: z.nullable(embedMediaSchema),
  video: z.nullable(embedMediaSchema),
});

/** The server of a chat event; null in a direct message or group DM. */
const chatGuildId = z.nullable(snowflake);

const messageSchema: z.ZodMiniType<S["Message"]> = z.object({
  id: snowflake,
  channel_id: snowflake,
  guild_id: chatGuildId,
  author: z.nullable(userSchema),
  content: z.string(),
  created_at: z.string(),
  edited_at: nullableString,
  reply_to_id: z.nullable(snowflake),
  referenced_message: z.nullable(messageStubSchema),
  attachments: z.array(attachmentSchema),
  embeds: z.array(embedSchema),
  mention_user_ids: z.array(snowflake),
  mention_role_ids: z.array(snowflake),
  mention_everyone: z.boolean(),
  mention_here: z.boolean(),
  reactions: z.optional(z.array(reactionSchema)),
  nonce: z.optional(z.string()),
});

/** The payload type of each chat event (`x-gateway.events`). */
export interface ChatEvents {
  MESSAGE_CREATE: S["Message"];
  MESSAGE_UPDATE: S["Message"];
  MESSAGE_DELETE: S["MessageDeleteEvent"];
  TYPING_START: S["TypingStartEvent"];
  MESSAGE_REACTION_ADD: S["MessageReactionEvent"];
  MESSAGE_REACTION_REMOVE: S["MessageReactionEvent"];
}

const reactionEventSchema: z.ZodMiniType<S["MessageReactionEvent"]> = z.object({
  channel_id: snowflake,
  guild_id: chatGuildId,
  message_id: snowflake,
  user_id: snowflake,
  emoji: emojiSchema,
});

const chatSchemas: { [K in keyof ChatEvents]: z.ZodMiniType<ChatEvents[K]> } = {
  MESSAGE_CREATE: messageSchema,
  MESSAGE_UPDATE: messageSchema,
  MESSAGE_DELETE: z.object({
    id: snowflake,
    channel_id: snowflake,
    guild_id: chatGuildId,
    last_message_id: z.nullable(snowflake),
  }),
  TYPING_START: z.object({ channel_id: snowflake, guild_id: chatGuildId, user_id: snowflake, timestamp: z.string() }),
  MESSAGE_REACTION_ADD: reactionEventSchema,
  MESSAGE_REACTION_REMOVE: reactionEventSchema,
};

/** Whether `t` is a chat event. */
export const isChatEvent = (t: string): t is keyof ChatEvents => Object.hasOwn(chatSchemas, t);

/** The payload of chat event `t` checked against the contract, or null (logged) when it does not match. */
export function parseChatEvent<K extends keyof ChatEvents>(t: K, d: unknown): ChatEvents[K] | null {
  const result = chatSchemas[t].safeParse(d);
  if (result.success) return result.data;
  console.warn(`gateway: dropped a malformed ${t}`, result.error.issues);
  return null;
}

// Call events: only the voice call applies them (VOICE_MOVE is private to
// the participant being moved), never the guild store.

/** The payload type of each call event (`x-gateway.events`). */
export interface CallEvents {
  VOICE_MOVE: S["VoiceMove"];
}

const callSchemas: { [K in keyof CallEvents]: z.ZodMiniType<CallEvents[K]> } = {
  VOICE_MOVE: z.object({ channel_id: snowflake }),
};

/** Whether `t` is a call event. */
export const isCallEvent = (t: string): t is keyof CallEvents => Object.hasOwn(callSchemas, t);

/** The payload of call event `t` checked against the contract, or null (logged) when it does not match. */
export function parseCallEvent<K extends keyof CallEvents>(t: K, d: unknown): CallEvents[K] | null {
  const result = callSchemas[t].safeParse(d);
  if (result.success) return result.data;
  console.warn(`gateway: dropped a malformed ${t}`, result.error.issues);
  return null;
}
