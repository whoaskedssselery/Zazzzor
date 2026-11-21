import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { getConnectionStatus, subscribeConnectionStatus } from "@/entities/guild";
import {
  type Attachment,
  addPending,
  confirmPending,
  getPending,
  type MessageStub,
  type PendingMessage,
  removePending,
  updateHistory,
  updatePending,
  upsertMessage,
} from "@/entities/message";
import { ApiError, api, errorText, unwrap } from "@/shared/api";
import { onLanguage, t } from "@/shared/i18n";

/** How long a send waits for the gateway to come back before it fails. */
export const SEND_WAIT_MS = 10_000;

export let TOO_FAST = "";
export let OFFLINE = "";
export let MAYBE_SENT = "";
onLanguage(() => {
  TOO_FAST = t("chat:send.tooFast");
  OFFLINE = t("chat:send.offline");
  MAYBE_SENT = t("chat:send.maybeSent");
});

/**
 * How long a retry with the message's nonce cannot send it twice: the
 * server answers a nonce with the message sent with it for 10 minutes
 * (NonceTTL); a minute is left for a request still in flight.
 */
export const RETRY_WINDOW_MS = 9 * 60_000;

/** Whether sending pending message `p` again with its nonce is still safe. */
export const canRetrySafely = (p: Pick<PendingMessage, "sentAt">, now = Date.now()): boolean =>
  now - p.sentAt < RETRY_WINDOW_MS;

/** A nonce for one message: a random UUID without dashes (32 characters, the contract allows 64). */
const newNonce = (): string => crypto.randomUUID().replaceAll("-", "");

class OfflineError extends Error {}

/**
 * Resolves once the gateway is ready, or rejects after `ms`: a message sent
 * while it reconnects goes out after the fresh READY, so it never lands in a
 * history the client is about to replace.
 */
const whenConnected = (ms: number): Promise<void> =>
  new Promise((resolve, reject) => {
    if (getConnectionStatus() === "ready") {
      resolve();
      return;
    }
    const timer = setTimeout(() => {
      stop();
      reject(new OfflineError());
    }, ms);
    const stop = subscribeConnectionStatus((status) => {
      if (status !== "ready") return;
      clearTimeout(timer);
      stop();
      resolve();
    });
  });

/** Why a send failed, in the words the failed message shows. */
const failureText = (e: unknown): string => {
  if (e instanceof OfflineError) return OFFLINE;
  if (e instanceof ApiError && e.status === 429) return TOO_FAST;
  return errorText(e);
};

/** Sends pending message `nonce` (again); the message stays pending until confirmed, or is marked failed. */
const deliver = async (qc: QueryClient, channelId: string, nonce: string): Promise<void> => {
  try {
    await whenConnected(SEND_WAIT_MS);
    const p = getPending(channelId, nonce);
    // Confirmed by its MESSAGE_CREATE meanwhile (a retry of a lost response), or discarded.
    if (p === undefined) return;
    const message = unwrap(
      await api.POST("/channels/{channel_id}/messages", {
        params: { path: { channel_id: channelId } },
        body: {
          content: p.content,
          reply_to_id: p.replyTo?.id ?? null,
          nonce,
          attachment_ids: p.attachments.map((a) => a.id),
        },
      }),
    );
    updateHistory(qc, channelId, (h) => upsertMessage(h, message));
    confirmPending(channelId, nonce);
  } catch (e) {
    updatePending(channelId, nonce, { status: "failed", error: failureText(e) });
  }
};

export interface SendMessage {
  /** Sends `content` (already trimmed and checked) with uploaded `attachments`, answering `replyTo` if set. */
  send: (content: string, replyTo: MessageStub | null, attachments?: Attachment[]) => void;
  /**
   * Sends a failed message again with its nonce, so a send that did arrive
   * is not sent twice. Past RETRY_WINDOW_MS the server may not know the
   * nonce any more: the message is marked MAYBE_SENT instead.
   */
  retry: (nonce: string) => void;
  /** Sends a failed message as a new one, with a new nonce: the user checked it did not arrive. */
  resend: (nonce: string) => void;
  /** Gives up on a failed message. */
  discard: (nonce: string) => void;
}

/**
 * Sending in `channelId`. A message shows at once as sending; the response
 * or its MESSAGE_CREATE, whichever comes first, puts it in the history by
 * its nonce, never twice. One call sends one message: the composer clears
 * its text in the same event, so a second Enter has nothing to send.
 */
export const useSendMessage = (channelId: string): SendMessage => {
  const qc = useQueryClient();
  const send = useCallback(
    (content: string, replyTo: MessageStub | null, attachments: Attachment[] = []) => {
      const nonce = newNonce();
      addPending({
        nonce,
        channelId,
        content,
        replyTo,
        attachments,
        status: "sending",
        error: null,
        sentAt: Date.now(),
      });
      void deliver(qc, channelId, nonce);
    },
    [qc, channelId],
  );
  const retry = useCallback(
    (nonce: string) => {
      const p = getPending(channelId, nonce);
      if (p?.status !== "failed") return;
      if (!canRetrySafely(p)) {
        updatePending(channelId, nonce, { error: MAYBE_SENT });
        return;
      }
      updatePending(channelId, nonce, { status: "sending", error: null });
      void deliver(qc, channelId, nonce);
    },
    [qc, channelId],
  );
  const resend = useCallback(
    (nonce: string) => {
      const p = getPending(channelId, nonce);
      if (p?.status !== "failed") return;
      removePending(channelId, nonce);
      send(p.content, p.replyTo, p.attachments);
    },
    [channelId, send],
  );
  const discard = useCallback(
    (nonce: string) => {
      removePending(channelId, nonce);
    },
    [channelId],
  );
  return { send, retry, resend, discard };
};
