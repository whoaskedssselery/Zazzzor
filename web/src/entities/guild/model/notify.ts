import { type ChannelOverride, type GuildsState, type NotifySettings, PRIVATE_SCOPE } from "./reducer";

/** What alerts: every message, only mentions, nothing; INHERIT (a channel's own) takes the scope's. */
export const NOTIFY_LEVEL = { ALL: 0, MENTIONS: 1, NOTHING: 2, INHERIT: 3 } as const;
export type NotifyLevel = 0 | 1 | 2;

/** The settings of a scope nobody changed: a server alerts to mentions, the DMs to every message. */
export const defaultNotify = (guildId: string | null): NotifySettings => ({
  guild_id: guildId,
  muted: false,
  mute_until: null,
  message_notifications: guildId === null ? NOTIFY_LEVEL.ALL : NOTIFY_LEVEL.MENTIONS,
  suppress_everyone: false,
  channel_overrides: [],
});

/** The user's settings of server `guildId`, or of the private channels when null. */
export const notifyOf = (s: Pick<GuildsState, "notify">, guildId: string | null): NotifySettings =>
  s.notify[guildId ?? PRIVATE_SCOPE] ?? defaultNotify(guildId);

/** A channel's own settings, if the user set any. */
export const overrideOf = (n: NotifySettings, channelId: string): ChannelOverride | undefined =>
  n.channel_overrides.find((o) => o.channel_id === channelId);

/** Whether a mute holds at `now`: on, and either for good or not yet over. */
export const muteHolds = (m: { muted: boolean; mute_until: string | null }, now = Date.now()): boolean =>
  m.muted && (m.mute_until === null || Date.parse(m.mute_until) > now);

/** Whether the whole scope (a server, or every private channel) is muted. */
export const isScopeMuted = (s: Pick<GuildsState, "notify">, guildId: string | null, now = Date.now()): boolean =>
  muteHolds(notifyOf(s, guildId), now);

/** Whether channel `channelId` is muted, by itself or with its scope. */
export const isChannelMuted = (
  s: Pick<GuildsState, "notify">,
  guildId: string | null,
  channelId: string,
  now = Date.now(),
): boolean => {
  const n = notifyOf(s, guildId);
  if (muteHolds(n, now)) return true;
  const o = overrideOf(n, channelId);
  return o !== undefined && muteHolds(o, now);
};

/** Whether only the channel itself is muted (its scope is not): the menu offers to unmute it. */
export const isChannelOwnMuted = (
  s: Pick<GuildsState, "notify">,
  guildId: string | null,
  channelId: string,
): boolean => {
  const o = overrideOf(notifyOf(s, guildId), channelId);
  return o !== undefined && muteHolds(o);
};

/** What alerts in channel `channelId`: its own level, else its scope's. */
export const channelLevel = (
  s: Pick<GuildsState, "notify">,
  guildId: string | null,
  channelId: string,
): NotifyLevel => {
  const n = notifyOf(s, guildId);
  const own = overrideOf(n, channelId)?.message_notifications;
  return own === undefined || own === NOTIFY_LEVEL.INHERIT ? n.message_notifications : own;
};

/**
 * The level that decides alerts in channel `channelId`, mutes counted. As
 * in Discord, a muted server or channel still alerts to mentions (unless
 * its level is «nothing»); a muted DM or group alerts to nothing.
 */
export const alertLevel = (s: Pick<GuildsState, "notify">, guildId: string | null, channelId: string): NotifyLevel => {
  const level = channelLevel(s, guildId, channelId);
  if (!isChannelMuted(s, guildId, channelId)) return level;
  if (guildId === null) return NOTIFY_LEVEL.NOTHING;
  return level === NOTIFY_LEVEL.NOTHING ? level : NOTIFY_LEVEL.MENTIONS;
};

/** The soonest moment a mute in force ends, to look again then; null when none will. */
export const nextMuteEnd = (s: Pick<GuildsState, "notify">, now = Date.now()): number | null => {
  let soonest: number | null = null;
  for (const n of Object.values(s.notify)) {
    for (const m of [n, ...n.channel_overrides]) {
      if (!m.muted || m.mute_until === null) continue;
      const end = Date.parse(m.mute_until);
      if (end > now && (soonest === null || end < soonest)) soonest = end;
    }
  }
  return soonest;
};
