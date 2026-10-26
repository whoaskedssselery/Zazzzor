import { create } from "zustand";
import { type ConnectionStatus, parseEvent } from "@/shared/api";
import { applyEvent, applyReady, EMPTY_GUILDS, type GuildsState } from "./reducer";

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
 * other events change it (see applyEvent). Messages and typing do not touch it.
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

/**
 * Resolves true once the store holds server `guildId` (at once if it does
 * already), or false after `timeoutMs`. A server the user creates or joins
 * arrives over the gateway (GUILD_CREATE) a moment after the REST answer;
 * this is how the UI waits for it before opening the server.
 */
export const waitForGuild = (guildId: string, timeoutMs = 15_000): Promise<boolean> =>
  new Promise((resolve) => {
    if (guildId in store.getState().guilds.guilds) {
      resolve(true);
      return;
    }
    const done = (arrived: boolean): void => {
      clearTimeout(timer);
      unsubscribe();
      resolve(arrived);
    };
    const unsubscribe = store.subscribe((st) => {
      if (guildId in st.guilds.guilds) done(true);
    });
    const timer = setTimeout(() => {
      done(false);
    }, timeoutMs);
  });
