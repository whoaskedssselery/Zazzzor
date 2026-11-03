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

/**
 * The category a new channel of `type` goes into when none is chosen: the
 * one of the last channel of that type in the list, so text channels join
 * the text ones and voice channels the voice ones, whatever the categories
 * are called. Null when no category holds a channel of that type.
 */
export const homeCategory = (entry: GuildEntry, type: number): Channel | null => {
  const all = Object.values(entry.channels);
  const categories = all.filter((c) => c.type === CHANNEL_TYPE.CATEGORY).sort(compareChannels);
  for (let i = categories.length - 1; i >= 0; i--) {
    const category = categories[i];
    if (category && all.some((c) => c.type === type && c.parent_id === category.id)) return category;
  }
  return null;
};

/** Whether channel `channelId` is a text channel the user may view (a place to open). */
export const isOpenableChannel = (entry: GuildEntry, channelId: string, userId: string): boolean =>
  entry.channels[channelId]?.type === CHANNEL_TYPE.TEXT && canView(entry, channelId, userId);

/** A text or voice channel the user may view: what the channel address can show. */
export const isVisitableChannel = (entry: GuildEntry, channelId: string, userId: string): boolean => {
  const type = entry.channels[channelId]?.type;
  return (type === CHANNEL_TYPE.TEXT || type === CHANNEL_TYPE.VOICE) && canView(entry, channelId, userId);
};

/** The path of a text channel. */
export const channelPath = (guildId: string, channelId: string): string => `/channels/${guildId}/${channelId}`;

interface Preference {
  /** A channel to open if the user may view it (an invite's channel). */
  prefer?: string | undefined;
  /** The channel last opened in the server. */
  last?: string | undefined;
}

/**
 * The text channel to open in a server: the one asked for (`prefer`), else
 * the one last opened there (`last`), else the first text channel of the
 * list, whichever the user may view; null when no text channel is visible.
 */
export const channelToOpen = (entry: GuildEntry, userId: string, { prefer, last }: Preference = {}): string | null => {
  for (const id of [prefer, last]) {
    if (id !== undefined && isOpenableChannel(entry, id, userId)) return id;
  }
  for (const group of visibleChannelGroups(entry, userId)) {
    const text = group.channels.find((c) => c.type === CHANNEL_TYPE.TEXT);
    if (text) return text.id;
  }
  return null;
};

/**
 * Where to go in server `guildId` (see channelToOpen). Home when the server
 * is not held, the user is unknown, or no text channel is visible.
 */
export const fallbackPath = (state: GuildsState, guildId: string | null, preference: Preference = {}): string => {
  const entry = guildId === null ? undefined : state.guilds[guildId];
  const userId = state.me?.id;
  if (!entry || !userId) return HOME_PATH;
  const id = channelToOpen(entry, userId, preference);
  return id === null ? HOME_PATH : channelPath(entry.guild.id, id);
};
