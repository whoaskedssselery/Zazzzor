import { useMemo } from "react";
import { useGuilds } from "@/entities/guild/@x/channel";
import { useChannelUi } from "./ui";
import { channelToOpen } from "./visible";

/**
 * The text channel a click on server `guildId` opens (its last opened
 * channel, else the first visible one), or null when it has none the user
 * may view. A link to it goes there in one navigation.
 */
export function useChannelToOpen(guildId: string): string | null {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const userId = useGuilds((s) => s.me?.id);
  const last = useChannelUi((s) => s.lastChannel[guildId]);
  return useMemo(() => (entry && userId ? channelToOpen(entry, userId, { last }) : null), [entry, userId, last]);
}
