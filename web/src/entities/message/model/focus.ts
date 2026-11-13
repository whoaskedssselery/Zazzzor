import { useSyncExternalStore } from "react";

/**
 * Requests to show one message of a channel (a search result): the chat of
 * that channel loads older pages until the message is there, scrolls to it,
 * marks it and clears the request. One request per channel at a time.
 */
const requests = new Map<string, string>();
const listeners = new Set<() => void>();

const notify = () => {
  for (const fn of listeners) fn();
};

/** Asks the chat of `channelId` to show message `messageId`. */
export const requestFocus = (channelId: string, messageId: string): void => {
  requests.set(channelId, messageId);
  notify();
};

/** Drops the request of `channelId` (done, or the message is gone). */
export const clearFocus = (channelId: string): void => {
  if (requests.delete(channelId)) notify();
};

const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
};

/** The message the chat of `channelId` is asked to show, or null. */
export const useFocusRequest = (channelId: string): string | null =>
  useSyncExternalStore(
    subscribe,
    () => requests.get(channelId) ?? null,
    () => null,
  );
