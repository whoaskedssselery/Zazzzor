import { create } from "zustand";
import type { components } from "@/shared/api";
import type { Attachment } from "./attachment";

type MessageStub = components["schemas"]["MessageStub"];

/**
 * A message this client is sending, shown after the history until the
 * server confirms it. It is matched to the confirmed message by `nonce`,
 * whichever comes first: the response to the send or its MESSAGE_CREATE.
 */
export interface PendingMessage {
  nonce: string;
  channelId: string;
  content: string;
  /** The message it answers, as the reply preview shows it. */
  replyTo: MessageStub | null;
  /** The files it carries, uploaded already (pending on the server until it is sent). */
  attachments: Attachment[];
  status: "sending" | "failed";
  /** Why it failed, in the user's words. */
  error: string | null;
  /** When it was first sent (Date.now()): a retry with its nonce is safe only while the server remembers the nonce. */
  sentAt: number;
}

interface PendingStore {
  /** Pending messages by channel, oldest first; a channel with none has no key. */
  byChannel: Record<string, PendingMessage[]>;
}

const store = create<PendingStore>()(() => ({ byChannel: {} }));

const NONE: PendingMessage[] = [];

/** The pending messages of `channelId`, oldest first. */
export const usePendingMessages = (channelId: string): PendingMessage[] => store((s) => s.byChannel[channelId] ?? NONE);

/** The pending message with `nonce` in `channelId`, outside React. */
export const getPending = (channelId: string, nonce: string): PendingMessage | undefined =>
  store.getState().byChannel[channelId]?.find((p) => p.nonce === nonce);

const setChannel = (channelId: string, list: PendingMessage[]): void => {
  const { [channelId]: _old, ...rest } = store.getState().byChannel;
  store.setState({ byChannel: list.length === 0 ? rest : { ...rest, [channelId]: list } });
};

/** Shows `p` as sending (or failed) at the end of its channel. */
export const addPending = (p: PendingMessage): void => {
  setChannel(p.channelId, [...(store.getState().byChannel[p.channelId] ?? []), p]);
};

/** Changes the state of a pending message (a retry, a failure). */
export const updatePending = (channelId: string, nonce: string, patch: Partial<PendingMessage>): void => {
  const list = store.getState().byChannel[channelId];
  if (!list?.some((p) => p.nonce === nonce)) return;
  setChannel(
    channelId,
    list.map((p) => (p.nonce === nonce ? { ...p, ...patch } : p)),
  );
};

/** The message with `nonce` reached the server: it leaves the pending list (the history has it now). */
/** Whether a send with `nonce` is pending in `channelId`. */
export const isPending = (channelId: string, nonce: string): boolean =>
  store.getState().byChannel[channelId]?.some((p) => p.nonce === nonce) ?? false;

export const confirmPending = (channelId: string, nonce: string): void => {
  const list = store.getState().byChannel[channelId];
  if (!list?.some((p) => p.nonce === nonce)) return;
  setChannel(
    channelId,
    list.filter((p) => p.nonce !== nonce),
  );
};

/** Forgets every pending message (the session ended). */
export const resetPending = (): void => {
  store.setState({ byChannel: {} });
};

/** Drops a pending message the user gave up on. */
export const removePending = confirmPending;
