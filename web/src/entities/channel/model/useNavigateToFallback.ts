import { useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";
import { getGuildsState } from "@/entities/guild/@x/channel";
import { useChannelUi } from "./ui";
import { fallbackPath } from "./visible";

export interface FallbackOptions {
  /** A channel to open if the user may view it (an invite's channel). */
  prefer?: string | undefined;
  /** Replace the current history entry (leaving a page that is gone). */
  replace?: boolean;
}

/**
 * A function that opens server `guildId` where it makes sense (see
 * fallbackPath): the preferred channel, the last one opened there or the
 * first visible text channel, else home. It reads the store when called, so
 * it is right even straight after an event changed it.
 */
export function useNavigateToFallback(): (guildId: string | null, options?: FallbackOptions) => void {
  const navigate = useNavigate();
  return useCallback(
    (guildId, { prefer, replace = false } = {}) => {
      const last = guildId === null ? undefined : useChannelUi.getState().lastChannel[guildId];
      void navigate({ href: fallbackPath(getGuildsState(), guildId, { prefer, last }), replace });
    },
    [navigate],
  );
}
