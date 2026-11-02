import type { Channel } from "@/entities/guild/@x/channel";

/** One entry of PATCH /guilds/{id}/channels. */
export interface ChannelMove {
  id: string;
  position: number;
  parent_id?: string | null;
}

/** Past the end of any parent: the server puts the channel last. */
const LAST = 9999;

/**
 * The request that moves `channel` into `parentId` (null: the top level)
 * right before `before` (null: to the end). The server moves only the
 * listed channel, as a splice: it takes it out of its parent, then puts it
 * at `position` among the rest, hidden channels included. So within the
 * same parent a move down lands one position earlier than `before`'s.
 * Null when nothing would change.
 */
export function channelMove(channel: Channel, parentId: string | null, before: Channel | null): ChannelMove | null {
  const sameParent = channel.parent_id === parentId;
  if (before?.id === channel.id) return null;
  let position: number;
  if (before === null) position = LAST;
  else position = sameParent && channel.position < before.position ? before.position - 1 : before.position;
  if (sameParent && position === channel.position) return null;
  return sameParent ? { id: channel.id, position } : { id: channel.id, position, parent_id: parentId };
}

/**
 * The move of Alt+↑ / Alt+↓ on a channel of `siblings` (its group, in list
 * order): one place up or down within the group; null at its edge.
 */
export function stepMove(channel: Channel, siblings: Channel[], up: boolean): ChannelMove | null {
  const i = siblings.findIndex((c) => c.id === channel.id);
  if (i === -1) return null;
  const target = up ? i - 1 : i + 2;
  if (up && target < 0) return null;
  if (!up && i === siblings.length - 1) return null;
  return channelMove(channel, channel.parent_id, siblings[target] ?? null);
}
