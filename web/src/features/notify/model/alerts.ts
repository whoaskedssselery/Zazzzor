import { useRouter } from "@tanstack/react-router";
import { useEffect } from "react";
import {
  type GuildEntry,
  getGuildsState,
  mentionsMe,
  PRIVATE_TYPE,
  type PrivateEntry,
  privateName,
  RELATIONSHIP,
  userDisplay,
} from "@/entities/guild";
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

/** A notification's text: the message as plain text, a clip for a file alone, cut short. */
const bodyOf = (m: Message, mentionNames: MentionNames): string => {
  const body = markdownText(m.content, mentionNames);
  const text = body === "" && m.attachments.length > 0 ? "📎 Вложение" : body;
  return text.length > MAX_BODY ? `${text.slice(0, MAX_BODY - 1)}…` : text;
};

const iconOf = (avatarUrl: string | null | undefined): string =>
  new URL(avatarUrl ?? "/favicon.svg", window.location.origin).href;

interface Notice {
  title: string;
  body: string;
  icon: string;
}

/** The notification of message `m` in server `e`: «Аня (#общий, Вечерний сеанс)» and its text. */
export const notificationOf = (e: GuildEntry, m: Message): Notice => {
  const author = m.author ? userDisplay(e, m.author) : null;
  const channel = e.channels[m.channel_id]?.name ?? "канал";
  return {
    title: `${author?.name ?? "Кто-то"} (#${channel}, ${e.guild.name})`,
    body: bodyOf(m, names(e)),
    icon: iconOf(author?.avatarUrl),
  };
};

/** The notification of message `m` in a DM («Аня») or a group («Аня (Пятница)»). */
export const privateNotificationOf = (p: PrivateEntry, m: Message): Notice => {
  const author = m.author?.display_name ?? "Кто-то";
  const people = new Map(p.channel.recipients.map((r) => [r.id, r.display_name]));
  const mentionNames: MentionNames = {
    user: (id) => {
      const name = people.get(id);
      return name === undefined ? null : { name };
    },
    role: () => null,
  };
  return {
    title: p.channel.type === PRIVATE_TYPE.GROUP ? `${author} (${privateName(p.channel)})` : author,
    body: bodyOf(m, mentionNames),
    icon: iconOf(m.author?.avatar_url),
  };
};

/** The channel open at `pathname` (/channels/<guild or @me>/<channel>), if any. */
export const openChannelOf = (pathname: string): string | null =>
  /^\/channels\/(?:\d+|@me)\/(\d+)/.exec(pathname)?.[1] ?? null;

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
        const s = getGuildsState();
        const guildId = m.guild_id;
        const e = guildId === null ? undefined : s.guilds[guildId];
        const p = guildId === null ? s.privates[m.channel_id] : undefined;
        const notice = e ? () => notificationOf(e, m) : p ? () => privateNotificationOf(p, m) : null;
        if (notice === null) return;
        const authorId = m.author?.id;
        const alert = alertFor({
          // In a DM or a group every message is for the user, as in Discord; not one from someone they blocked.
          mentioned: e
            ? mentionsMe(e, m)
            : authorId === undefined || s.relationships[authorId]?.type !== RELATIONSHIP.BLOCKED,
          own: authorId !== undefined && authorId === s.me?.id,
          windowActive: windowActive(),
          openChannelId: openChannelOf(router.state.location.pathname),
          channelId: m.channel_id,
        });
        if (alert === null) return;
        const prefs = getNotifyPrefs();
        if (alert.sound && prefs.sound) playMentionSound();
        if (!alert.desktop || !prefs.desktop || !("Notification" in window)) return;
        if (Notification.permission !== "granted") return;
        const { title, body, icon } = notice();
        // One per channel: a newer mention there replaces the older one.
        const n = new Notification(title, { body, icon, tag: `channel-${m.channel_id}` });
        n.onclick = () => {
          window.focus();
          void (guildId === null
            ? router.navigate({ to: "/channels/@me/$channelId", params: { channelId: m.channel_id } })
            : router.navigate({ to: "/channels/$guildId/$channelId", params: { guildId, channelId: m.channel_id } }));
          n.close();
        };
      }),
    [router],
  );
};
