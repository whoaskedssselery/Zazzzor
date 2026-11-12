import { useEffect } from "react";
import { guildMentionCount, isGuildUnread, useGuilds } from "@/entities/guild";

export const APP_TITLE = "Zazzzor";

/** The tab's title: unread mentions first, as Discord shows them. */
export const titleFor = (mentions: number): string => (mentions > 0 ? `(${String(mentions)}) ${APP_TITLE}` : APP_TITLE);

/** The app's icon (public/favicon.svg), with a mark when something waits. */
const ICON_BODY =
  '<rect width="32" height="32" rx="9" fill="#3dba86"/><path d="M9 8.5h14v3.3L13.9 20.5H23v3H9v-3.3l9.1-8.7H9z" fill="#0b1a13"/>';

/**
 * The favicon: the plain icon; with a red dot for mentions or a white one
 * for unread messages, ringed in dark so it reads on any tab bar.
 */
export const faviconFor = (state: "none" | "unread" | "mentions"): string => {
  const dot =
    state === "none"
      ? ""
      : `<circle cx="25" cy="7" r="7.5" fill="#1a1917"/><circle cx="25" cy="7" r="5.5" fill="${state === "mentions" ? "#e5484d" : "#f5f1ea"}"/>`;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">${ICON_BODY}${dot}</svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
};

/** The tab's title and favicon follow unread mentions and messages across all servers. */
export const useUnreadBadge = (): void => {
  const mentions = useGuilds((s) => Object.values(s.guilds).reduce((n, e) => n + guildMentionCount(e), 0));
  const unread = useGuilds((s) => Object.values(s.guilds).some((e) => isGuildUnread(e, s)));
  useEffect(() => {
    document.title = titleFor(mentions);
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (link) link.href = faviconFor(mentions > 0 ? "mentions" : unread ? "unread" : "none");
    return () => {
      document.title = APP_TITLE;
      if (link) link.href = "/favicon.svg";
    };
  }, [mentions, unread]);
};
