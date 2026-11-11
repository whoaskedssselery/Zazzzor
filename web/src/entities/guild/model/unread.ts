import { computeChannel, has, PERM } from "./permissions";
import { type GuildEntry, newer } from "./reducer";

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
