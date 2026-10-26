import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { fallbackPath, isOpenableChannel, useChannelUi, useNavigateToFallback } from "@/entities/channel";
import { getGuildsState, useConnectionStatus, useGuilds } from "@/entities/guild";
import { useToast } from "@/shared/ui";

export const CHANNEL_LOST = "Канал больше недоступен";
export const GUILD_LOST = "Сервер больше недоступен";

/** What this page has seen of its server and channel so far. */
interface Seen {
  key: string;
  guild: boolean;
  channel: boolean;
}

/**
 * Keeps /channels/$guildId[/$channelId] on something the user may open.
 *
 * Loss of access (Review Focus): only a change from visible to gone counts.
 * When the open channel was visible and is no longer (a GUILD_SYNC without
 * it, CHANNEL_DELETE, a fresh READY), the user moves to the nearest visible
 * channel of the server with «Канал больше недоступен»; when the server goes
 * (GUILD_DELETE, kicked, banned) they go home with «Сервер больше
 * недоступен». An event that adds access never moves anyone.
 *
 * An address that was never valid (an old link, a hidden channel) is left
 * quietly for the nearest valid place, but only once the store can tell:
 * before the first READY nothing is decided. Without a channel in the
 * address, the server opens on its last or first text channel; a server
 * with no visible text channel stays on its empty view.
 *
 * Returns whether the channel in the address can be shown.
 */
export function useChannelAccess(guildId: string, channelId: string | null): boolean {
  const status = useConnectionStatus();
  const entry = useGuilds((s) => s.guilds[guildId]);
  const openable = useGuilds((s) => {
    const e = s.guilds[guildId];
    return channelId !== null && e !== undefined && s.me !== null && isOpenableChannel(e, channelId, s.me.id);
  });
  const setLastChannel = useChannelUi((s) => s.setLastChannel);
  const goToFallback = useNavigateToFallback();
  const navigate = useNavigate();
  const toast = useToast();
  const seen = useRef<Seen>({ key: "", guild: false, channel: false });

  useEffect(() => {
    const key = `${guildId}/${channelId ?? ""}`;
    if (seen.current.key !== key) seen.current = { key, guild: false, channel: false };
    const was = seen.current;

    if (!entry) {
      if (was.guild) {
        was.guild = false;
        toast.show({ text: GUILD_LOST });
        goToFallback(null, { replace: true });
      } else if (status === "ready") {
        goToFallback(null, { replace: true });
      }
      return;
    }
    was.guild = true;

    if (channelId === null) {
      const last = useChannelUi.getState().lastChannel[guildId];
      const target = fallbackPath(getGuildsState(), guildId, { last });
      if (target.startsWith(`/channels/${guildId}/`)) void navigate({ href: target, replace: true });
      return;
    }
    if (openable) {
      was.channel = true;
      if (useChannelUi.getState().lastChannel[guildId] !== channelId) setLastChannel(guildId, channelId);
      return;
    }
    if (was.channel) {
      was.channel = false;
      toast.show({ text: CHANNEL_LOST });
    }
    goToFallback(guildId, { replace: true });
  }, [guildId, channelId, entry, openable, status, goToFallback, navigate, setLastChannel, toast]);

  return openable;
}
