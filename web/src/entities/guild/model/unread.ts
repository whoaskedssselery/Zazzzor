import { computeChannel, has, PERM } from "./permissions";
import { isPrivateUnread } from "./private";
import { type GuildEntry, type GuildsState, newer } from "./reducer";

/** Text channels (type 0) are the ones with messages. */
const TEXT = 0;

/**
 * Whether text channel `channelId` has messages the user has not read.
 * A channel whose history they may not read never counts: they could not
 * read it to the end, so it would stay marked forever.
 */
export const isUnread = (e: GuildEntry, channelId: string): boolean => {
  const channel = e.channels[channelId];
  if (channel?.type !== TEXT || channel.last_message_id === null) return false;
  if (!has(computeChannel(e, channelId, e.myMember.user.id), PERM.READ_MESSAGE_HISTORY)) return false;
  const mark = e.reads[channelId];
  return mark === undefined || newer(channel.last_message_id, mark.lastReadId);
};

/** Unread messages in `channelId` that mention the user. */
export const mentionCount = (e: GuildEntry, channelId: string): number =>
  e.channels[channelId] === undefined ? 0 : (e.reads[channelId]?.mentions ?? 0);

/** Whether any channel of the server has unread messages. */
export const isGuildUnread = (e: GuildEntry): boolean => Object.keys(e.channels).some((id) => isUnread(e, id));

/** Unread mentions across the server's channels. */
export const guildMentionCount = (e: GuildEntry): number =>
  Object.keys(e.channels).reduce((sum, id) => sum + mentionCount(e, id), 0);

/** The server entry holding channel `channelId`, if any. */
const entryOf = (s: GuildsState, channelId: string): GuildEntry | undefined => {
  for (const id of s.order) {
    const e = s.guilds[id];
    if (e?.channels[channelId] !== undefined) return e;
  }
  return undefined;
};

/** Whether channel `channelId`, of a server or a DM or group, has messages the user has not read. */
export const isChannelUnread = (s: GuildsState, channelId: string): boolean => {
  const p = s.privates[channelId];
  if (p) return isPrivateUnread(p);
  const e = entryOf(s, channelId);
  return e !== undefined && isUnread(e, channelId);
};

/** The newest message of channel `channelId`, of a server or private; null when empty or unknown. */
export const lastMessageOf = (s: GuildsState, channelId: string): string | null =>
  s.privates[channelId]?.channel.last_message_id ?? entryOf(s, channelId)?.channels[channelId]?.last_message_id ?? null;

/** The user's read mark in `channelId` as it is now, if the channel has unread messages: where «Новые» goes. */
export const unreadFrom = (s: GuildsState, channelId: string): string | null => {
  if (!isChannelUnread(s, channelId)) return null;
  const p = s.privates[channelId];
  if (p) return p.read?.lastReadId ?? "0";
  return entryOf(s, channelId)?.reads[channelId]?.lastReadId ?? "0";
};
