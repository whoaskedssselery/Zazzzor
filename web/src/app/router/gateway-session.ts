import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { resetChannelUi } from "@/entities/channel";
import { applyGatewayDispatch, getGuildsState, resetGuilds, setConnectionStatus } from "@/entities/guild";
import { onSessionEnd, onUnauthorized, setSignedIn } from "@/entities/session";
import { resetDrafts } from "@/features/send-message";
import { getGateway } from "@/shared/api";

/**
 * Runs the gateway while `active` (the session is authenticated): its events
 * go into the guild store and its status into the connection banner, a 4004
 * ends the session like a 401. When the gateway changes the signed-in user
 * (READY, USER_UPDATE), the session query ["me"] gets the new user too, so
 * everything that reads useSession() follows. When the session ends (sign-out, a 401, a
 * 4004) the gateway stops and the client stores are emptied; leaving the
 * protected screens stops the gateway too.
 */
export const useGatewaySession = (active: boolean): void => {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (!active) return;
    const gateway = getGateway();
    const unlisten = gateway.listen({
      onDispatch: (t, d) => {
        const before = getGuildsState().me;
        applyGatewayDispatch(t, d);
        const me = getGuildsState().me;
        if (me && me !== before) setSignedIn(queryClient, me);
      },
      onStatus: setConnectionStatus,
      onUnauthorized,
    });
    const offSessionEnd = onSessionEnd(() => {
      gateway.stop();
      resetGuilds();
      resetChannelUi();
      resetDrafts();
    });
    gateway.start();
    return () => {
      offSessionEnd();
      unlisten();
      gateway.stop();
      setConnectionStatus("connecting");
    };
  }, [active, queryClient]);
};
