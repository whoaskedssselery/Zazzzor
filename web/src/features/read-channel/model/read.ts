import { useEffect, useState } from "react";
import { isUnread, markRead, useGuilds } from "@/entities/guild";
import { api } from "@/shared/api";

/** Acks wait this long, so a burst of messages read at once is one request. */
export const ACK_DELAY_MS = 300;

/** Whether the user can be reading the page: its tab is shown and its window has the focus. */
const useWindowActive = (): boolean => {
  const read = () => document.visibilityState === "visible" && document.hasFocus();
  const [active, setActive] = useState(read);
  useEffect(() => {
    const update = () => {
      setActive(read());
    };
    document.addEventListener("visibilitychange", update);
    window.addEventListener("focus", update);
    window.addEventListener("blur", update);
    return () => {
      document.removeEventListener("visibilitychange", update);
      window.removeEventListener("focus", update);
      window.removeEventListener("blur", update);
    };
  }, []);
  return active;
};

/**
 * Marks channel `channelId` read as the user reads it, as Discord does:
 * while it is open in an active window and its list is at the bottom
 * (`atBottom`), every new message is read. The mark moves at once; the server
 * hears it shortly after (POST …/ack), and its other tabs from the server.
 * A failed ack is not worth a word: the next one carries it.
 */
export const useReadChannel = (guildId: string, channelId: string, atBottom: boolean): void => {
  const active = useWindowActive();
  const unread = useGuilds((s) => {
    const e = s.guilds[guildId];
    return e !== undefined && isUnread(e, channelId);
  });
  const last = useGuilds((s) => s.guilds[guildId]?.channels[channelId]?.last_message_id ?? null);
  useEffect(() => {
    if (!unread || !active || !atBottom || last === null) return;
    const timer = setTimeout(() => {
      markRead(channelId, last);
      void api
        .POST("/channels/{channel_id}/messages/{message_id}/ack", {
          params: { path: { channel_id: channelId, message_id: last } },
        })
        .catch(() => {});
    }, ACK_DELAY_MS);
    return () => {
      clearTimeout(timer);
    };
  }, [unread, active, atBottom, last, channelId]);
};

/** Marks every channel of server `guildId` read («Отметить как прочитанное»). */
export const readGuild = async (guildId: string): Promise<void> => {
  await api.POST("/guilds/{guild_id}/ack", { params: { path: { guild_id: guildId } } });
};
