import { create } from "zustand";
import type { NativeShareStats, ShareSource } from "@/entities/call";

/** The desktop app's screen share as the page sees it. */
export interface NativeShareState {
  /** The picker of screens and windows is open. */
  picking: boolean;
  /** What the app shares now; null when it shares nothing. */
  source: ShareSource | null;
  /** The share's latest figures; null in its first two seconds. */
  stats: NativeShareStats | null;
}

const IDLE = { picking: false, source: null, stats: null } as const satisfies NativeShareState;

const store = create<NativeShareState>()(() => IDLE);

/** A slice of the native share; the component re-renders when it changes. */
export const useNativeShare = <T>(selector: (s: NativeShareState) => T): T => store(selector);

export const getNativeShare = (): NativeShareState => store.getState();

export const setNativeShare = (patch: Partial<NativeShareState>): void => {
  store.setState(patch);
};

export const resetNativeShareState = (): void => {
  store.setState(IDLE);
};
