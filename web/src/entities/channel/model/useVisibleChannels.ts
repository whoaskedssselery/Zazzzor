import { useMemo } from "react";
import { useGuilds } from "@/entities/guild/@x/channel";
import { type ChannelGroup, visibleChannelGroups } from "./visible";

/**
 * The channel list of server `guildId` as the signed-in user sees it (see
 * visibleChannelGroups), recomputed when the server or the user changes in
 * the store. Empty for a server the store does not hold.
 */
export function useVisibleChannels(guildId: string): { categories: ChannelGroup[] } {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const userId = useGuilds((s) => s.me?.id);
  return useMemo(() => ({ categories: entry && userId ? visibleChannelGroups(entry, userId) : [] }), [entry, userId]);
}
