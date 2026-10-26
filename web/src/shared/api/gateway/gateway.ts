import { useEffect, useEffectEvent } from "react";
import { GatewayConnection } from "./connection";

let instance: GatewayConnection | null = null;

/**
 * The app's one gateway connection. The app starts it when the session
 * becomes authenticated and stops it when the session ends; everything else
 * only subscribes to it.
 */
export function getGateway(): GatewayConnection {
  instance ??= new GatewayConnection();
  return instance;
}

/**
 * Calls `fn` with the payload of every DISPATCH of type `t` while the
 * component is mounted, always the `fn` of the latest render.
 */
export function useGatewayEvent(t: string, fn: (d: unknown) => void): void {
  const handle = useEffectEvent(fn);
  useEffect(
    () =>
      getGateway().on(t, (d) => {
        handle(d);
      }),
    [t],
  );
}
