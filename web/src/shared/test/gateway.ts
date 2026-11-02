import { type WebSocketHandlerConnection, ws } from "msw";
import { type components, gatewayUrl } from "@/shared/api";

type GatewayFrame = components["schemas"]["GatewayFrame"];
type GatewayHello = components["schemas"]["GatewayHello"];
type GatewayReady = components["schemas"]["GatewayReady"];

/**
 * The gateway WebSocket of the page as MSW intercepts it (jsdom's location,
 * so the same URL the app computes). Handlers: `gateway.addEventListener(
 * "connection", ({ client }) => ...)`, or the helpers below.
 */
export const gateway = ws.link(gatewayUrl(window.location));

/** A server frame of the contract (components.schemas.GatewayFrame) as the text the socket carries. */
export function frame(f: GatewayFrame): string {
  return JSON.stringify(f);
}

export function helloFrame(heartbeatIntervalMs = 30000): string {
  const d: GatewayHello = { heartbeat_interval_ms: heartbeatIntervalMs };
  return frame({ op: 10, d });
}

/**
 * A DISPATCH frame as raw text. `d` is deliberately untyped so tests can send
 * payloads the contract forbids and check that the decode boundary drops them.
 */
export function dispatchFrame(t: string, s: number, d: unknown): string {
  return JSON.stringify({ op: 0, t, s, d });
}

/**
 * The gateway every test starts with (the default handler of `server`): it
 * accepts the connection and sends nothing, so the app stays "connecting".
 * Tests that need HELLO and READY add `readyGateway(...)` with `server.use`.
 */
export const silentGateway = gateway.addEventListener("connection", () => {});

type GatewayClient = WebSocketHandlerConnection["client"];

/**
 * A gateway that answers each connection with HELLO and READY (`ready(n)`
 * for the n-th connection, from 1), then calls `after(client, n)` if given.
 */
export function readyGateway(
  ready: GatewayReady | ((connection: number) => GatewayReady),
  after?: (client: GatewayClient, connection: number) => void,
) {
  let connections = 0;
  return gateway.addEventListener("connection", ({ client }) => {
    connections += 1;
    client.send(helloFrame());
    client.send(dispatchFrame("READY", 1, typeof ready === "function" ? ready(connections) : ready));
    after?.(client, connections);
  });
}
