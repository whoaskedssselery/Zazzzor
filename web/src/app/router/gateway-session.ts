import { useEffect } from "react";
import { resetChannelUi } from "@/entities/channel";
import { applyGatewayDispatch, resetGuilds, setConnectionStatus } from "@/entities/guild";
import { onSessionEnd, onUnauthorized } from "@/entities/session";
import { resetDrafts } from "@/features/send-message";
import { getGateway } from "@/shared/api";

/**
 * Runs the gateway while `active` (the session is authenticated): its events
 * go into the guild store and its status into the connection banner, a 4004
 * ends the session like a 401. When the session ends (sign-out, a 401, a
 * 4004) the gateway stops and the client stores are emptied; leaving the
 * protected screens stops the gateway too.
 */
export const useGatewaySession = (active: boolean): void => {
  useEffect(() => {
    if (!active) return;
    const gateway = getGateway();
    const unlisten = gateway.listen({
      onDispatch: applyGatewayDispatch,
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
  }, [active]);
};
