import { useMutation } from "@tanstack/react-query";
import { CHANNEL_TYPE } from "@/entities/channel";
import {
  byRank,
  canActOn,
  canManageRole,
  computeBase,
  computeChannel,
  type GuildEntry,
  has,
  PERM,
  type Role,
} from "@/entities/guild";
import { api, errorText, unwrap } from "@/shared/api";
import { useToast } from "@/shared/ui";

/** What the user may do to a member, as the server would allow it. */
export interface MemberActions {
  nick: boolean;
  roles: boolean;
  kick: boolean;
  ban: boolean;
}

/**
 * The moderation `actorId` may apply to `targetId` in server `entry`:
 * - their own nickname with CHANGE_NICKNAME, another's with MANAGE_NICKNAMES
 *   and a rank above them (never the owner's);
 * - roles with MANAGE_ROLES while some role is below the actor's highest;
 * - kick and ban with the permission and a rank above the target.
 * The server checks all of it again; this only hides what it would refuse.
 */
export function memberActions(entry: GuildEntry, actorId: string, targetId: string): MemberActions {
  const base = computeBase(entry, actorId);
  const self = actorId === targetId;
  const above = canActOn(entry, actorId, targetId);
  return {
    nick: self ? has(base, PERM.CHANGE_NICKNAME) : has(base, PERM.MANAGE_NICKNAMES) && above,
    roles: assignableRoles(entry, actorId).length > 0,
    kick: !self && has(base, PERM.KICK_MEMBERS) && above,
    ban: !self && has(base, PERM.BAN_MEMBERS) && above,
  };
}

/** Roles (without @everyone) the actor may give or take, highest first. */
export const assignableRoles = (entry: GuildEntry, actorId: string): Role[] =>
  Object.values(entry.roles)
    .filter((r) => r.id !== entry.guild.id && canManageRole(entry, actorId, r))
    .sort(byRank);

/**
 * The member requests of one server. Each reports its failure as a toast in
 * the words of errors.ts (`role_hierarchy` included); the store follows the
 * server's events, so nothing is patched by hand.
 */
export const useMemberMutations = (guildId: string) => {
  const toast = useToast();
  const onError = (e: unknown) => {
    toast.show({ text: errorText(e), tone: "error" });
  };
  const path = (userId: string) => ({ path: { guild_id: guildId, user_id: userId } });
  const update = useMutation({
    mutationFn: async ({ userId, body }: { userId: string; body: { nick?: string | null; roles?: string[] } }) =>
      unwrap(await api.PATCH("/guilds/{guild_id}/members/{user_id}", { params: path(userId), body })),
    onError,
  });
  const kick = useMutation({
    mutationFn: async (userId: string) => {
      unwrap(await api.DELETE("/guilds/{guild_id}/members/{user_id}", { params: path(userId) }));
    },
    onSuccess: () => {
      toast.show({ text: "Участник выгнан с сервера" });
    },
    onError,
  });
  const ban = useMutation({
    mutationFn: async ({ userId, reason }: { userId: string; reason: string | null }) => {
      unwrap(await api.PUT("/guilds/{guild_id}/bans/{user_id}", { params: path(userId), body: { reason } }));
    },
    onSuccess: () => {
      toast.show({ text: "Участник забанен" });
    },
    onError,
  });
  return { update, kick, ban };
};

/** What the user may do to a member who is in voice, and how that member is now. */
export interface VoiceActions {
  /** Muted or deafened by a moderator now: the items say «включить» then. */
  muted: boolean;
  deafened: boolean;
  mute: boolean;
  deafen: boolean;
  disconnect: boolean;
  /** Voice channels to move them to, in channel order. */
  moveTo: string[];
}

/**
 * The voice moderation `actorId` may apply to `targetId`, who is in a voice
 * channel of `entry` (null when they are not, or nothing is allowed):
 * MUTE_MEMBERS and DEAFEN_MEMBERS in their channel; MOVE_MEMBERS there to
 * disconnect, and also in a destination they may view and connect to, to
 * move. On anyone but themself the actor also needs a higher role (the
 * owner acts on all). The server checks all of it again.
 */
export function voiceActions(entry: GuildEntry, actorId: string, targetId: string): VoiceActions | null {
  const state = entry.voice[targetId];
  if (!state) return null;
  if (actorId !== targetId && !canActOn(entry, actorId, targetId)) return null;
  const here = computeChannel(entry, state.channel_id, actorId);
  const canMove = has(here, PERM.MOVE_MEMBERS);
  const moveTo = canMove
    ? Object.values(entry.channels)
        .filter(
          (c) =>
            c.type === CHANNEL_TYPE.VOICE &&
            c.id !== state.channel_id &&
            has(computeChannel(entry, c.id, actorId), PERM.MOVE_MEMBERS) &&
            has(computeChannel(entry, c.id, targetId), PERM.VIEW_CHANNEL | PERM.CONNECT),
        )
        .sort((a, b) => a.position - b.position || (a.id < b.id ? -1 : 1))
        .map((c) => c.id)
    : [];
  const actions = {
    muted: state.mute,
    deafened: state.deaf,
    mute: has(here, PERM.MUTE_MEMBERS),
    deafen: has(here, PERM.DEAFEN_MEMBERS),
    disconnect: canMove,
    moveTo,
  };
  return actions.mute || actions.deafen || actions.disconnect ? actions : null;
}

/** The voice moderation requests of one server; a refusal is a toast in the words of errors.ts. */
export const useVoiceModeration = (guildId: string) => {
  const toast = useToast();
  return useMutation({
    mutationFn: async ({
      userId,
      body,
    }: {
      userId: string;
      body: { mute?: boolean; deaf?: boolean; channel_id?: string | null };
    }) => {
      unwrap(
        await api.PATCH("/guilds/{guild_id}/members/{user_id}/voice", {
          params: { path: { guild_id: guildId, user_id: userId } },
          body,
        }),
      );
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};
