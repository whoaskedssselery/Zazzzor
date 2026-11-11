import { create } from "zustand";
import { type ConnectionStatus, parseEvent } from "@/shared/api";
import { advanceRead, applyEvent, applyReady, EMPTY_GUILDS, type GuildsState } from "./reducer";

interface GuildStore {
  guilds: GuildsState;
  status: ConnectionStatus;
}

const INITIAL: GuildStore = { guilds: EMPTY_GUILDS, status: "connecting" };

const store = create<GuildStore>()(() => INITIAL);

/**
 * A slice of the guild store; the component re-renders when it changes.
 * The selector must return stored values (or primitives), not new objects
 * or arrays built on each call.
 */
export const useGuilds = <T>(selector: (s: GuildsState) => T): T => store((st) => selector(st.guilds));

/** The state of the gateway connection, for the banner and for re-fetching after a reconnect. */
export const useConnectionStatus = (): ConnectionStatus => store((st) => st.status);

/** The store as it is now, outside React. */
export const getGuildsState = (): GuildsState => store.getState().guilds;

export const setConnectionStatus = (status: ConnectionStatus): void => {
  store.setState({ status });
};

/**
 * Applies one DISPATCH of the gateway: READY replaces the store wholesale,
 * other events change it (see applyEvent). Of messages it keeps only unread.
 */
export const applyGatewayDispatch = (t: string, d: unknown): void => {
  const before = store.getState().guilds;
  let after: GuildsState;
  if (t === "READY") {
    const ready = parseEvent("READY", d);
    after = ready ? applyReady(ready) : before;
  } else {
    after = applyEvent(before, t, d);
  }
  if (after !== before) store.setState({ guilds: after });
};

/** Forgets every server and starts over at "connecting" (the session ended). */
export const resetGuilds = (): void => {
  store.setState(INITIAL, true);
};

/** The state of the gateway connection now, outside React. */
export const getConnectionStatus = (): ConnectionStatus => store.getState().status;

/** Calls `fn` with every change of the connection state; returns the unsubscribe. */
export const subscribeConnectionStatus = (fn: (status: ConnectionStatus) => void): (() => void) =>
  store.subscribe((st, prev) => {
    if (st.status !== prev.status) fn(st.status);
  });

/**
 * Marks `channelId` read through `messageId` at once, before the server
 * confirms (its MESSAGE_ACK then changes nothing). Never moves a mark back.
 */
export const markRead = (channelId: string, messageId: string): void => {
  const before = store.getState().guilds;
  const after = advanceRead(before, channelId, { lastReadId: messageId, mentions: 0 });
  if (after !== before) store.setState({ guilds: after });
};
