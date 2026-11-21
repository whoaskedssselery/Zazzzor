import { useEffect, useState } from "react";
import { parseChatEvent, useGatewayEvent } from "@/shared/api";
import { t } from "@/shared/i18n";

/** A typing notice shows this long unless renewed. */
export const TYPING_SHOWN_MS = 10_000;

/** «Иван печатает…», «Иван и Пётр печатают…», «Несколько человек печатают…». */
export const typingText = (names: string[]): string | null => {
  const [a, b] = names;
  if (a === undefined) return null;
  if (b === undefined) return t("chat:typingOne", { name: a });
  if (names.length === 2) return t("chat:typingTwo", { a, b });
  return t("chat:typingMany");
};

/**
 * Who is typing in `channelId`, under the composer. A notice lasts
 * TYPING_SHOWN_MS and ends early when its author's message arrives; the
 * user's own notices never show. `nameOf` gives the name a user goes by here.
 */
export const TypingIndicator = ({
  channelId,
  meId,
  nameOf,
}: {
  channelId: string;
  meId: string | null;
  nameOf: (userId: string) => string;
}) => {
  // User ID → when their notice ends, in the order they started typing.
  const [typing, setTyping] = useState<ReadonlyMap<string, number>>(new Map());

  useGatewayEvent("TYPING_START", (d) => {
    const e = parseChatEvent("TYPING_START", d);
    if (e === null || e.channel_id !== channelId || e.user_id === meId) return;
    setTyping((m) => new Map(m).set(e.user_id, Date.now() + TYPING_SHOWN_MS));
  });
  useGatewayEvent("MESSAGE_CREATE", (d) => {
    const m = parseChatEvent("MESSAGE_CREATE", d);
    const author = m?.author?.id;
    if (m === null || m.channel_id !== channelId || author === undefined) return;
    setTyping((t) => {
      if (!t.has(author)) return t;
      const next = new Map(t);
      next.delete(author);
      return next;
    });
  });

  // The next notice to end takes itself off when it does.
  useEffect(() => {
    if (typing.size === 0) return;
    const soonest = Math.min(...typing.values());
    const timer = setTimeout(
      () => {
        const now = Date.now();
        setTyping((t) => new Map([...t].filter(([, end]) => end > now)));
      },
      Math.max(0, soonest - Date.now()),
    );
    return () => {
      clearTimeout(timer);
    };
  }, [typing]);

  // A switch of channel starts with nobody typing.
  const [shownFor, setShownFor] = useState(channelId);
  if (shownFor !== channelId) {
    setShownFor(channelId);
    setTyping(new Map());
  }

  const text = typingText([...typing.keys()].map(nameOf));
  return (
    <p aria-live="polite" className="-mt-5 mb-0 h-5 truncate px-5 pt-0.5 text-xs text-fg-muted">
      {text}
    </p>
  );
};
