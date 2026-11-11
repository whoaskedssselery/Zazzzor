import { useRouter } from "@tanstack/react-router";
import { useEffect } from "react";
import { type GuildEntry, getGuildsState, mentionsMe, userDisplay } from "@/entities/guild";
import type { Message } from "@/entities/message";
import { getGateway, parseChatEvent } from "@/shared/api";
import { type MentionNames, markdownText } from "@/shared/lib";
import { getNotifyPrefs } from "./prefs";
import { playMentionSound } from "./sound";

/** What a new message calls for: nothing, a sound, or a sound and a system notification. */
export interface Alert {
  sound: boolean;
  desktop: boolean;
}

/**
 * Discord's rule. A message that mentions the user, by someone else, alerts:
 * with a sound unless they are looking at its channel, and with a system
 * notification too when the window is in the background.
 */
export const alertFor = ({
  mentioned,
  own,
  windowActive,
  openChannelId,
  channelId,
}: {
  mentioned: boolean;
  own: boolean;
  windowActive: boolean;
  openChannelId: string | null;
  channelId: string;
}): Alert | null => {
  if (!mentioned || own) return null;
  if (windowActive && openChannelId === channelId) return null;
  return { sound: true, desktop: !windowActive };
};

/** The longest notification text; the rest is the chat's to show. */
const MAX_BODY = 200;

const names = (e: GuildEntry): MentionNames => ({
  user: (id) => {
    const member = e.members[id];
    return member ? { name: userDisplay(e, member.user).name } : null;
  },
  role: (id) => {
    const role = e.roles[id];
    return role ? { name: role.name } : null;
  },
});

/** The notification of message `m` in server `e`: «Аня (#общий, Вечерний сеанс)» and its text. */
export const notificationOf = (e: GuildEntry, m: Message): { title: string; body: string; icon: string } => {
  const author = m.author ? userDisplay(e, m.author) : null;
  const channel = e.channels[m.channel_id]?.name ?? "канал";
  let body = markdownText(m.content, names(e));
  if (body === "" && m.attachments.length > 0) body = "📎 Вложение";
  if (body.length > MAX_BODY) body = `${body.slice(0, MAX_BODY - 1)}…`;
  return {
    title: `${author?.name ?? "Кто-то"} (#${channel}, ${e.guild.name})`,
    body,
    icon: new URL(author?.avatarUrl ?? "/favicon.svg", window.location.origin).href,
  };
};

/** The channel open at `pathname` (/channels/<guild>/<channel>), if any. */
export const openChannelOf = (pathname: string): string | null => /^\/channels\/\d+\/(\d+)/.exec(pathname)?.[1] ?? null;

const windowActive = () => document.visibilityState === "visible" && document.hasFocus();

/**
 * Alerts the user to messages that mention them, as long as the app runs
 * behind sign-in. A click on the notification brings the window to the
 * front at that channel.
 */
export const useMentionAlerts = (): void => {
  const router = useRouter();
  useEffect(
    () =>
      getGateway().on("MESSAGE_CREATE", (d) => {
        const m = parseChatEvent("MESSAGE_CREATE", d);
        if (m === null) return;
        const e = getGuildsState().guilds[m.guild_id];
        if (!e) return;
        const alert = alertFor({
          mentioned: mentionsMe(e, m),
          own: m.author?.id === e.myMember.user.id,
          windowActive: windowActive(),
          openChannelId: openChannelOf(router.state.location.pathname),
          channelId: m.channel_id,
        });
        if (alert === null) return;
        const prefs = getNotifyPrefs();
        if (alert.sound && prefs.sound) playMentionSound();
        if (!alert.desktop || !prefs.desktop || !("Notification" in window)) return;
        if (Notification.permission !== "granted") return;
        const { title, body, icon } = notificationOf(e, m);
        // One per channel: a newer mention there replaces the older one.
        const n = new Notification(title, { body, icon, tag: `channel-${m.channel_id}` });
        n.onclick = () => {
          window.focus();
          void router.navigate({
            to: "/channels/$guildId/$channelId",
            params: { guildId: m.guild_id, channelId: m.channel_id },
          });
          n.close();
        };
      }),
    [router],
  );
};
