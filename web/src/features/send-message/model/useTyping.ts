import { useCallback, useRef } from "react";
import { api } from "@/shared/api";

/** At most one typing notice per this long while the user keeps typing. */
export const TYPING_EVERY_MS = 8_000;

export interface Typing {
  /** The user typed in the composer: tell the channel, unless told within TYPING_EVERY_MS. */
  typed: () => void;
  /** The message went out: the next keystroke may tell the channel again at once. */
  sent: () => void;
}

/**
 * Typing notices of the composer in `channelId` (POST /typing). The server
 * shows one for about ten seconds, so one every eight seconds keeps it on
 * while the user types. A failed notice is not worth a word to the user.
 */
export const useTyping = (channelId: string): Typing => {
  // When the last notice went out, and to which channel: a switch starts over.
  const last = useRef({ channelId: "", at: Number.NEGATIVE_INFINITY });
  const typed = useCallback(() => {
    const now = Date.now();
    if (last.current.channelId === channelId && now - last.current.at < TYPING_EVERY_MS) return;
    last.current = { channelId, at: now };
    void api.POST("/channels/{channel_id}/typing", { params: { path: { channel_id: channelId } } }).catch(() => {});
  }, [channelId]);
  const sent = useCallback(() => {
    last.current = { channelId: "", at: Number.NEGATIVE_INFINITY };
  }, []);
  return { typed, sent };
};
