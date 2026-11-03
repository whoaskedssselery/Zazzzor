import { create } from "zustand";
import type { CallLink } from "@/shared/lib";

export interface CallState {
  /** idle: in no voice channel. */
  status: "idle" | Exclude<CallLink, "disconnected">;
  guildId: string | null;
  channelId: string | null;
  /** The user's own microphone and sound switches; they outlive calls, as in Discord. */
  selfMute: boolean;
  selfDeaf: boolean;
  /** The microphone was refused or is missing in this call. */
  micDenied: boolean;
  /** Who speaks now, the user included. */
  speaking: string[];
  /** Who shares a screen in the call (others only). */
  streams: string[];
  /** Whose screen shares the user receives. */
  watching: string[];
  /** The user shares their screen. */
  sharing: boolean;
}

const CALL_FREE = {
  status: "idle",
  guildId: null,
  channelId: null,
  micDenied: false,
  speaking: [],
  streams: [],
  watching: [],
  sharing: false,
} as const satisfies Partial<CallState>;

const store = create<CallState>()(() => ({ ...CALL_FREE, selfMute: false, selfDeaf: false }));

/** A slice of the call; the component re-renders when it changes. */
export const useCall = <T>(selector: (s: CallState) => T): T => store(selector);

export const getCall = (): CallState => store.getState();

export const setCall = (patch: Partial<CallState>): void => {
  store.setState(patch);
};

/** Out of the call; the microphone and sound switches stay as they are. */
export const clearCall = (): void => {
  store.setState(CALL_FREE);
};

/** Everything back to the start, switches included (the session ended, tests). */
export const resetCall = (): void => {
  store.setState({ ...CALL_FREE, selfMute: false, selfDeaf: false });
};
