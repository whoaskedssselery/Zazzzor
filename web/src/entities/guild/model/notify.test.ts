import { makeChannel, makeDMChannel, makeGuildState, makeReady, makeUser } from "@/shared/test";
import { alertLevel, channelLevel, isChannelMuted, isScopeMuted, NOTIFY_LEVEL, nextMuteEnd, notifyOf } from "./notify";
import { homeUnreadCount } from "./private";
import { applyEvent, applyReady, type GuildsState, type NotifySettings } from "./reducer";
import { isGuildUnread } from "./unread";

const me = makeUser("1");
const anya = makeUser("2", { display_name: "Аня" });
const G = "10";
const TEXT = "101";
const LOUD = "102";
const DM = "900";
const NOW = Date.parse("2026-11-12T18:00:00Z");
const inAnHour = new Date(NOW + 3_600_000).toISOString();
const anHourAgo = new Date(NOW - 3_600_000).toISOString();

const settings = (over: Partial<NotifySettings> = {}): NotifySettings => ({
  guild_id: G,
  version: 1,
  muted: false,
  mute_until: null,
  message_notifications: 1,
  suppress_everyone: false,
  channel_overrides: [],
  ...over,
});

/** READY with server 10 (both channels unread), a DM with Аня (one unread), and `notify`. */
const ready = (notify: NotifySettings[] = []): GuildsState =>
  applyReady({
    ...makeReady(me, [
      makeGuildState(G, me, {
        channels: [makeChannel(TEXT, G, { last_message_id: "5" }), makeChannel(LOUD, G, { last_message_id: "6" })],
      }),
    ]),
    private_channels: [makeDMChannel(DM, [anya], { last_message_id: "7" })],
    read_states: [{ channel_id: DM, last_read_id: "3", mention_count: 1 }],
    user_guild_settings: notify,
  });

describe("notification settings", () => {
  it("READY and USER_GUILD_SETTINGS_UPDATE keep them by server, the DMs under @me", () => {
    let s = ready([settings({ muted: true })]);
    expect(isScopeMuted(s, G, NOW)).toBe(true);
    s = applyEvent(s, "USER_GUILD_SETTINGS_UPDATE", settings({ guild_id: null, message_notifications: 2 }));
    expect(notifyOf(s, null).message_notifications).toBe(NOTIFY_LEVEL.NOTHING);
    expect(isScopeMuted(s, G, NOW)).toBe(true);
  });

  it("drops a snapshot older than the one it holds", () => {
    let s = ready([settings({ version: 5, muted: true })]);
    s = applyEvent(s, "USER_GUILD_SETTINGS_UPDATE", settings({ version: 4, muted: false }));
    expect(isScopeMuted(s, G, NOW)).toBe(true);
    s = applyEvent(s, "USER_GUILD_SETTINGS_UPDATE", settings({ version: 6, muted: false }));
    expect(isScopeMuted(s, G, NOW)).toBe(false);
  });

  it("falls back to the defaults: a server alerts to mentions, the DMs to everything", () => {
    const s = ready();
    expect(channelLevel(s, G, TEXT)).toBe(NOTIFY_LEVEL.MENTIONS);
    expect(channelLevel(s, null, DM)).toBe(NOTIFY_LEVEL.ALL);
    expect(isChannelMuted(s, G, TEXT, NOW)).toBe(false);
  });

  it("mutes a channel by itself or with its server, until the end passes", () => {
    const loud = { channel_id: LOUD, muted: true, mute_until: inAnHour, message_notifications: 3 as const };
    let s = ready([settings({ channel_overrides: [loud] })]);
    expect(isChannelMuted(s, G, LOUD, NOW)).toBe(true);
    expect(isChannelMuted(s, G, TEXT, NOW)).toBe(false);
    expect(isChannelMuted(s, G, LOUD, NOW + 2 * 3_600_000)).toBe(false);
    expect(nextMuteEnd(s, NOW)).toBe(Date.parse(inAnHour));
    s = ready([settings({ muted: true, mute_until: anHourAgo })]);
    expect(isScopeMuted(s, G, NOW)).toBe(false);
    expect(nextMuteEnd(s, NOW)).toBeNull();
  });

  it("takes a channel's own level over the server's, unless it inherits", () => {
    const s = ready([
      settings({
        message_notifications: 0,
        channel_overrides: [{ channel_id: LOUD, muted: false, mute_until: null, message_notifications: 2 }],
      }),
    ]);
    expect(channelLevel(s, G, LOUD)).toBe(NOTIFY_LEVEL.NOTHING);
    expect(channelLevel(s, G, TEXT)).toBe(NOTIFY_LEVEL.ALL);
  });

  it("lets mentions through a muted server, nothing through a muted DM", () => {
    const s = ready([settings({ muted: true, message_notifications: 0 }), settings({ guild_id: null, muted: true })]);
    expect(alertLevel(s, G, TEXT)).toBe(NOTIFY_LEVEL.MENTIONS);
    expect(alertLevel(s, null, DM)).toBe(NOTIFY_LEVEL.NOTHING);
  });

  it("keeps muted servers, channels and DMs from lighting up", () => {
    const loud = { channel_id: LOUD, muted: true, mute_until: null, message_notifications: 3 as const };
    let s = ready([settings({ channel_overrides: [loud] })]);
    const e = s.guilds[G];
    if (!e) throw new Error("no server");
    expect(isGuildUnread(e, s)).toBe(true);
    s = ready([
      settings({
        channel_overrides: [loud, { channel_id: TEXT, muted: true, mute_until: null, message_notifications: 3 }],
      }),
    ]);
    expect(isGuildUnread(e, s)).toBe(false);
    expect(isGuildUnread(e, ready([settings({ muted: true })]))).toBe(false);
    expect(homeUnreadCount(ready())).toBe(1);
    const dmMuted = settings({
      guild_id: null,
      message_notifications: 0,
      channel_overrides: [{ channel_id: DM, muted: true, mute_until: null, message_notifications: 3 }],
    });
    expect(homeUnreadCount(ready([dmMuted]))).toBe(0);
  });
});
