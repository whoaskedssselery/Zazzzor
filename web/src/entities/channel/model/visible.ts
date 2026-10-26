import {
  type Channel,
  computeChannel,
  type GuildEntry,
  type GuildsState,
  has,
  PERM,
} from "@/entities/guild/@x/channel";

/** Channel types of the contract (components.schemas.ChannelType). */
export const CHANNEL_TYPE = { TEXT: 0, VOICE: 2, CATEGORY: 4 } as const;

/** Where a signed-in user goes when no server is open. */
export const HOME_PATH = "/channels/@me";

/** One block of the channel list: a category (null for the channels above all categories) and its channels. */
export interface ChannelGroup {
  category: Channel | null;
  channels: Channel[];
}

/** Snowflakes compare as numbers: a shorter decimal string is a smaller number. */
const compareIds = (a: string, b: string): number => a.length - b.length || (a < b ? -1 : a > b ? 1 : 0);

/** The order of the channel list: by position, then by ID (as the server orders them). */
export const compareChannels = (a: Channel, b: Channel): number => a.position - b.position || compareIds(a.id, b.id);

/** Whether `userId` may view channel `channelId` of the server (VIEW_CHANNEL after overwrites). */
export const canView = (entry: GuildEntry, channelId: string, userId: string): boolean =>
  has(computeChannel(entry, channelId, userId), PERM.VIEW_CHANNEL);

/**
 * The channel list of a server as `userId` sees it: first the channels
 * outside any category, then the categories, each sorted by position and ID.
 * Channels without VIEW_CHANNEL are left out. A category is shown when the
 * user may view it or any channel in it; a channel whose category is gone
 * is listed at the top.
 */
export const visibleChannelGroups = (entry: GuildEntry, userId: string): ChannelGroup[] => {
  const all = Object.values(entry.channels).sort(compareChannels);
  const categories = all.filter((c) => c.type === CHANNEL_TYPE.CATEGORY);
  const categoryIds = new Set(categories.map((c) => c.id));
  const visible = all.filter((c) => c.type !== CHANNEL_TYPE.CATEGORY && canView(entry, c.id, userId));

  const top = visible.filter((c) => c.parent_id === null || !categoryIds.has(c.parent_id));
  const groups: ChannelGroup[] = top.length > 0 ? [{ category: null, channels: top }] : [];
  for (const category of categories) {
    const channels = visible.filter((c) => c.parent_id === category.id);
    if (channels.length > 0 || canView(entry, category.id, userId)) groups.push({ category, channels });
  }
  return groups;
};

/** Whether channel `channelId` is a text channel the user may view (a place to open). */
export const isOpenableChannel = (entry: GuildEntry, channelId: string, userId: string): boolean =>
  entry.channels[channelId]?.type === CHANNEL_TYPE.TEXT && canView(entry, channelId, userId);

/** The path of a text channel. */
export const channelPath = (guildId: string, channelId: string): string => `/channels/${guildId}/${channelId}`;

/**
 * Where to go in server `guildId`: the channel asked for (`prefer`), else the
 * one last opened there (`last`), else the first text channel of the list,
 * whichever the user may view. Home when the server is not held, the user is
 * unknown, or no text channel is visible.
 */
export const fallbackPath = (
  state: GuildsState,
  guildId: string | null,
  { prefer, last }: { prefer?: string | undefined; last?: string | undefined } = {},
): string => {
  const entry = guildId === null ? undefined : state.guilds[guildId];
  const userId = state.me?.id;
  if (!entry || !userId) return HOME_PATH;
  const gid = entry.guild.id;
  for (const id of [prefer, last]) {
    if (id !== undefined && isOpenableChannel(entry, id, userId)) return channelPath(gid, id);
  }
  for (const group of visibleChannelGroups(entry, userId)) {
    const text = group.channels.find((c) => c.type === CHANNEL_TYPE.TEXT);
    if (text) return channelPath(gid, text.id);
  }
  return HOME_PATH;
};
