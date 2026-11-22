import { create } from "zustand";
import { getCall } from "@/entities/call";
import { joinVoice } from "./session";

/** A call the user asked to move to while already in another. */
export interface PendingJoin {
  guildId: string | null;
  channelId: string;
}

const store = create<{ pending: PendingJoin | null }>()(() => ({ pending: null }));

/** The move waiting for the user's yes (SwitchCallDialog shows it). */
export const useSwitchCall = store;

/**
 * Joins `channelId`, as Discord does: straight away from outside a call,
 * and only after a yes while in another one, which this would leave.
 */
export const requestJoin = (guildId: string | null, channelId: string): void => {
  const { status, channelId: current } = getCall();
  if (status !== "idle" && current !== null && current !== channelId) {
    store.setState({ pending: { guildId, channelId } });
    return;
  }
  void joinVoice(guildId, channelId);
};

/** The user agreed: leave the call for the one they asked for. */
export const confirmSwitch = (): void => {
  const { pending } = store.getState();
  store.setState({ pending: null });
  if (pending) void joinVoice(pending.guildId, pending.channelId);
};

export const cancelSwitch = (): void => {
  store.setState({ pending: null });
};
