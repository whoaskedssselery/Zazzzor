import type { GuildEntry, Role } from "./reducer";

// The port of server/internal/permissions (stage 1.2a): the same bits, the
// same order of computation (spec §5), the same fail-closed rules. The client
// uses it only to hide what the user cannot do; the server decides.

/** Permission bits as in 1.2a (Discord's numbering). */
export const PERM = {
  CREATE_INVITE: 1n << 0n,
  KICK_MEMBERS: 1n << 1n,
  BAN_MEMBERS: 1n << 2n,
  ADMINISTRATOR: 1n << 3n,
  MANAGE_CHANNELS: 1n << 4n,
  MANAGE_GUILD: 1n << 5n,
  ADD_REACTIONS: 1n << 6n,
  STREAM: 1n << 9n,
  VIEW_CHANNEL: 1n << 10n,
  SEND_MESSAGES: 1n << 11n,
  MANAGE_MESSAGES: 1n << 13n,
  ATTACH_FILES: 1n << 15n,
  READ_MESSAGE_HISTORY: 1n << 16n,
  MENTION_EVERYONE: 1n << 17n,
  CONNECT: 1n << 20n,
  SPEAK: 1n << 21n,
  MUTE_MEMBERS: 1n << 22n,
  DEAFEN_MEMBERS: 1n << 23n,
  MOVE_MEMBERS: 1n << 24n,
  USE_VOICE_ACTIVITY: 1n << 25n,
  CHANGE_NICKNAME: 1n << 26n,
  MANAGE_NICKNAMES: 1n << 27n,
  MANAGE_ROLES: 1n << 28n,
  MANAGE_EXPRESSIONS: 1n << 30n,
} as const satisfies Record<string, bigint>;

/** Every defined bit. Bits outside it are kept in masks but never granted. */
export const ALL: bigint = Object.values(PERM).reduce((a, b) => a | b, 0n);

/** The @everyone mask of a new server. */
export const DEFAULT_EVERYONE: bigint =
  PERM.VIEW_CHANNEL |
  PERM.SEND_MESSAGES |
  PERM.READ_MESSAGE_HISTORY |
  PERM.ATTACH_FILES |
  PERM.ADD_REACTIONS |
  PERM.MENTION_EVERYONE |
  PERM.CREATE_INVITE |
  PERM.CHANGE_NICKNAME |
  PERM.CONNECT |
  PERM.SPEAK |
  PERM.STREAM |
  PERM.USE_VOICE_ACTIVITY;

/** Whether `mask` holds every bit of `bit` (true for 0n). */
export function has(mask: bigint, bit: bigint): boolean {
  return (mask & bit) === bit;
}

const MAX_MASK = (1n << 64n) - 1n;

/**
 * A mask from the API: only a string of ASCII decimal digits without leading
 * zeros that fits 64 bits, as the server accepts; anything else is null.
 */
export function parseMask(value: unknown): bigint | null {
  if (typeof value !== "string" || !/^(0|[1-9][0-9]{0,19})$/.test(value)) return null;
  const mask = BigInt(value);
  return mask <= MAX_MASK ? mask : null;
}

/** A mask as the API writes it: a decimal string. */
export function formatMask(mask: bigint): string {
  return mask.toString();
}

/** A mask of the store; one that does not parse grants nothing. */
const maskOf = (value: string): bigint => parseMask(value) ?? 0n;

interface Subject {
  userId: string;
  guildId: string;
  isOwner: boolean;
  everyone: { permissions: bigint; position: number };
  /** The member's roles without @everyone. */
  roles: Array<{ id: string; position: number; permissions: bigint }>;
}

/** A snowflake that is set: not empty, not "0". */
const isId = (id: string): boolean => /^[1-9][0-9]*$/.test(id);

/**
 * The member `userId` of the server as the permission model sees them, or
 * null when they are not a member or an ID is unset: an invalid subject must
 * never be mistaken for the owner, so it gets no rights at all.
 */
function subjectOf(entry: GuildEntry, userId: string): Subject | null {
  const { id: guildId, owner_id: ownerId } = entry.guild;
  const member = entry.members[userId];
  if (!member || !isId(userId) || !isId(guildId) || !isId(ownerId)) return null;
  const everyone = entry.roles[guildId];
  const roles = member.roles
    .filter((id) => id !== guildId)
    .flatMap((id) => {
      const role = entry.roles[id];
      return role ? [{ id, position: role.position, permissions: maskOf(role.permissions) }] : [];
    });
  return {
    userId,
    guildId,
    isOwner: userId === ownerId,
    everyone: { permissions: everyone ? maskOf(everyone.permissions) : 0n, position: everyone?.position ?? 0 },
    roles,
  };
}

function base(s: Subject): bigint {
  if (s.isOwner) return ALL;
  const p = s.roles.reduce((mask, r) => mask | r.permissions, s.everyone.permissions);
  return has(p, PERM.ADMINISTRATOR) ? ALL : p;
}

/** The server-wide permissions of `userId` (the owner and administrators get ALL). */
export function computeBase(entry: GuildEntry, userId: string): bigint {
  const s = subjectOf(entry, userId);
  return s ? base(s) : 0n;
}

/** `p` with an overwrite applied: deny first, then allow; ADMINISTRATOR in it is ignored. */
const overwrite = (p: bigint, allow: bigint, deny: bigint): bigint =>
  (p & ~(deny & ~PERM.ADMINISTRATOR)) | (allow & ~PERM.ADMINISTRATOR);

/**
 * The permissions of `userId` in channel `channelId`: base, the administrator
 * shortcut, then the @everyone overwrite, the merged overwrites of the
 * member's roles and the member's own overwrite. Without VIEW_CHANNEL the
 * result is 0. Only the first overwrite for a target counts. A channel the
 * server does not have gives 0.
 */
export function computeChannel(entry: GuildEntry, channelId: string, userId: string): bigint {
  const s = subjectOf(entry, userId);
  const channel = entry.channels[channelId];
  if (!s || !channel) return 0n;
  let p = base(s);
  if (has(p, PERM.ADMINISTRATOR)) return ALL;

  const roleIds = new Set(s.roles.map((r) => r.id));
  const seenRoles = new Set<string>();
  let everyoneOW: { allow: bigint; deny: bigint } | null = null;
  let memberOW: { allow: bigint; deny: bigint } | null = null;
  let roleAllow = 0n;
  let roleDeny = 0n;
  for (const ow of channel.permission_overwrites) {
    const allow = maskOf(ow.allow);
    const deny = maskOf(ow.deny);
    if (ow.type === 1 && ow.id === s.userId) {
      memberOW ??= { allow, deny };
    } else if (ow.type === 0 && ow.id === s.guildId) {
      everyoneOW ??= { allow, deny };
    } else if (ow.type === 0 && roleIds.has(ow.id) && !seenRoles.has(ow.id)) {
      seenRoles.add(ow.id);
      roleAllow |= allow;
      roleDeny |= deny;
    }
  }

  if (everyoneOW) p = overwrite(p, everyoneOW.allow, everyoneOW.deny);
  p = overwrite(p, roleAllow, roleDeny);
  if (memberOW) p = overwrite(p, memberOW.allow, memberOW.deny);
  return has(p, PERM.VIEW_CHANNEL) ? p : 0n;
}

function highest(s: Subject): number {
  if (s.isOwner) return Number.MAX_SAFE_INTEGER;
  return s.roles.reduce((pos, r) => Math.max(pos, r.position), s.everyone.position);
}

/**
 * The rank of `userId`: the owner outranks everything, others have the
 * position of their highest role (0 with only @everyone); -1 for someone who
 * is not a member.
 */
export function highestPosition(entry: GuildEntry, userId: string): number {
  const s = subjectOf(entry, userId);
  return s ? highest(s) : -1;
}

/** Whether `actorId` may edit or assign `role`: MANAGE_ROLES and a rank strictly above it. */
export function canManageRole(entry: GuildEntry, actorId: string, role: Role): boolean {
  const s = subjectOf(entry, actorId);
  return s !== null && has(base(s), PERM.MANAGE_ROLES) && highest(s) > role.position;
}

/**
 * Whether `actorId` may change the bits `changed` (new ^ old for an edit, the
 * whole mask for a creation): owners and administrators any defined bit,
 * everyone else only bits they hold. Undefined bits are always refused.
 */
export function canGrant(entry: GuildEntry, actorId: string, changed: bigint): boolean {
  const s = subjectOf(entry, actorId);
  if (!s || (changed & ~ALL) !== 0n) return false;
  const p = base(s);
  return has(p, PERM.ADMINISTRATOR) || has(p, changed);
}

/**
 * Whether `actorId` may moderate `targetId` (nickname, kick, ban): never
 * oneself or the owner; the owner anyone else; otherwise only from a highest
 * role strictly above the target's.
 */
export function canActOn(entry: GuildEntry, actorId: string, targetId: string): boolean {
  const actor = subjectOf(entry, actorId);
  const target = subjectOf(entry, targetId);
  if (!actor || !target || actorId === targetId || target.isOwner) return false;
  if (actor.isOwner) return true;
  return highest(actor) > highest(target);
}
