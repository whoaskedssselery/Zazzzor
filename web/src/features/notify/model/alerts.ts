import { useRouter } from "@tanstack/react-router";
import { useEffect } from "react";
import {
  alertLevel,
  type GuildEntry,
  getGuildsState,
  mentionsMe,
  NOTIFY_LEVEL,
  type NotifyLevel,
  notifyOf,
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
 * Discord's rule. A message by someone else alerts when the channel's level
 * (alertLevel) lets it: every message, or only one that mentions the user
 * (@everyone and @here count unless they hid them). It alerts with a sound
 * unless they are looking at its channel, and with a system notification
 * too when the window is in the background.
 */
export const alertFor = ({
  level,
  mentioned,
  everyone = false,
  suppressEveryone = false,
  own,
  windowActive,
  openChannelId,
  channelId,
}: {
  level: NotifyLevel;
  /** The message names the user or one of their roles. */
  mentioned: boolean;
  /** The message pings @everyone or @here. */
  everyone?: boolean;
  suppressEveryone?: boolean;
  own: boolean;
  windowActive: boolean;
  openChannelId: string | null;
  channelId: string;
}): Alert | null => {
  if (own || level === NOTIFY_LEVEL.NOTHING) return null;
  const pinged = mentioned || (everyone && !suppressEveryone);
  if (level === NOTIFY_LEVEL.MENTIONS && !pinged) return null;
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
 * Alerts the user to new messages, as their notification settings say, as long as the app runs
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
        // Nothing from someone the user blocked.
        if (authorId !== undefined && s.relationships[authorId]?.type === RELATIONSHIP.BLOCKED) return;
        const alert = alertFor({
          level: alertLevel(s, guildId, m.channel_id),
          // In a DM or a group every message is for the user, as in Discord.
          mentioned: e ? mentionsMe(e, { ...m, mention_everyone: false }) : true,
          everyone: m.mention_everyone,
          suppressEveryone: notifyOf(s, guildId).suppress_everyone,
          own: authorId !== undefined && authorId === s.me?.id,
          windowActive: windowActive(),
          openChannelId: openChannelOf(router.state.location.pathname),
          channelId: m.channel_id,
        });
        // «Не беспокоить» silences every alert; the counts still show.
        if (alert === null || s.me?.status === "dnd") return;
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
