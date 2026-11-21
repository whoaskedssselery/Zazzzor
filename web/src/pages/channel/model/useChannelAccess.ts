import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import {
  fallbackPath,
  isOpenableChannel,
  isVisitableChannel,
  useChannelUi,
  useNavigateToFallback,
} from "@/entities/channel";
import { getGuildsState, useConnectionStatus, useGuilds } from "@/entities/guild";
import { onLanguage, t } from "@/shared/i18n";
import { useToast } from "@/shared/ui";

export let CHANNEL_LOST = "";
export let GUILD_LOST = "";
onLanguage(() => {
  CHANNEL_LOST = t("guild:channelLost");
  GUILD_LOST = t("guild:guildLost");
});

/** What this page has seen of its server and channel so far. */
interface Seen {
  key: string;
  guild: boolean;
  channel: boolean;
  /** The channel-less address has been decided (opened a channel, or kept its empty view). */
  resolved: boolean;
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
 * with no visible text channel stays on its empty view. That is decided
 * once per address: a GUILD_SYNC that later adds a text channel there does
 * not move the user either.
 *
 * A store emptied by the end of the session (sign-out, a 401) is not a loss
 * of access: the user is leaving for sign-in, so nothing is said or done.
 *
 * Returns whether the channel in the address can be shown.
 */
export function useChannelAccess(guildId: string, channelId: string | null): boolean {
  const status = useConnectionStatus();
  // The store has a user from READY until the session ends and empties it.
  const signedIn = useGuilds((s) => s.me !== null);
  const entry = useGuilds((s) => s.guilds[guildId]);
  const openable = useGuilds((s) => {
    const e = s.guilds[guildId];
    return channelId !== null && e !== undefined && s.me !== null && isVisitableChannel(e, channelId, s.me.id);
  });
  const setLastChannel = useChannelUi((s) => s.setLastChannel);
  const goToFallback = useNavigateToFallback();
  const navigate = useNavigate();
  const toast = useToast();
  const seen = useRef<Seen>({ key: "", guild: false, channel: false, resolved: false });

  useEffect(() => {
    const key = `${guildId}/${channelId ?? ""}`;
    if (seen.current.key !== key) seen.current = { key, guild: false, channel: false, resolved: false };
    const was = seen.current;
    if (!signedIn) return;

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
      if (was.resolved) return;
      was.resolved = true;
      const last = useChannelUi.getState().lastChannel[guildId];
      const target = fallbackPath(getGuildsState(), guildId, { last });
      if (target.startsWith(`/channels/${guildId}/`)) void navigate({ href: target, replace: true });
      return;
    }
    if (openable) {
      was.channel = true;
      // A server reopens on its last text channel; a voice channel is not one.
      const me = getGuildsState().me;
      const text = me !== null && isOpenableChannel(entry, channelId, me.id);
      if (text && useChannelUi.getState().lastChannel[guildId] !== channelId) setLastChannel(guildId, channelId);
      return;
    }
    if (was.channel) {
      was.channel = false;
      toast.show({ text: CHANNEL_LOST });
    }
    goToFallback(guildId, { replace: true });
  }, [guildId, channelId, signedIn, entry, openable, status, goToFallback, navigate, setLastChannel, toast]);

  return openable;
}
