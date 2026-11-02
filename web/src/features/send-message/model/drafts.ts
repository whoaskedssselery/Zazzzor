import { create } from "zustand";

interface Drafts {
  /** Unsent text by channel ID; a channel without a draft has no key. */
  drafts: Record<string, string>;
}

/**
 * Drafts of the composer, one per channel. They survive switching channels
 * and reconnects, but live in memory only: never written to storage.
 */
export const useDrafts = create<Drafts>()(() => ({ drafts: {} }));

/** Keeps `text` as the draft of `channelId`; an empty text forgets it. */
export const setDraft = (channelId: string, text: string): void => {
  const { [channelId]: _old, ...rest } = useDrafts.getState().drafts;
  useDrafts.setState({ drafts: text === "" ? rest : { ...rest, [channelId]: text } });
};

/** The draft of `channelId` ("" when there is none). */
export const useDraft = (channelId: string): string => useDrafts((s) => s.drafts[channelId] ?? "");

/** Forgets every draft (the session ended). */
export const resetDrafts = (): void => {
  useDrafts.setState({ drafts: {} });
};

/** The draft of `channelId` now, outside React (read at the moment of sending, not from a render). */
export const getDraft = (channelId: string): string => useDrafts.getState().drafts[channelId] ?? "";
