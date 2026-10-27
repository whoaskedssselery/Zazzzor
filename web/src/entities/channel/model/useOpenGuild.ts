import { useCallback, useEffect, useEffectEvent, useState } from "react";
import { useGuilds } from "@/entities/guild/@x/channel";
import { useNavigateToFallback } from "./useNavigateToFallback";

/** How long a new server may take to arrive over the gateway before the user is told. */
export const GUILD_ARRIVAL_MS = 15_000;

interface Pending {
  guildId: string;
  prefer: string | undefined;
}

export interface OpenGuild {
  /** Opens server `guildId` (on channel `prefer` if visible) as soon as the store holds it. */
  open: (guildId: string, prefer?: string) => void;
  /** A server is awaited and has not arrived yet. */
  waiting: boolean;
  /** It has been awaited longer than GUILD_ARRIVAL_MS (the gateway is likely reconnecting). */
  late: boolean;
}

/**
 * Opens a server the user just created or joined. The REST answer comes
 * before the server's GUILD_CREATE, and only a server the store holds can be
 * opened, so this waits for it. It keeps waiting while the component lives:
 * after GUILD_ARRIVAL_MS `late` turns true so the caller can say why nothing
 * happens yet, and the server still opens the moment it arrives (after a
 * reconnect, with the fresh READY). `onOpen` runs right before it opens.
 */
export function useOpenGuild(onOpen?: () => void): OpenGuild {
  const go = useNavigateToFallback();
  const [pending, setPending] = useState<Pending | null>(null);
  const [late, setLate] = useState(false);
  const arrived = useGuilds((s) => pending !== null && pending.guildId in s.guilds);
  const openNow = useEffectEvent((p: Pending) => {
    onOpen?.();
    go(p.guildId, { prefer: p.prefer });
  });

  useEffect(() => {
    if (pending === null) return;
    if (arrived) {
      openNow(pending);
      return;
    }
    const timer = setTimeout(() => {
      setLate(true);
    }, GUILD_ARRIVAL_MS);
    return () => {
      clearTimeout(timer);
    };
  }, [pending, arrived]);

  const open = useCallback((guildId: string, prefer?: string) => {
    setLate(false);
    setPending({ guildId, prefer });
  }, []);

  return { open, waiting: pending !== null && !arrived, late };
}
