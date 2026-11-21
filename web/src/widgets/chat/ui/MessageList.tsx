import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown } from "lucide-react";
import { type ReactNode, useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from "react";
import { t } from "@/shared/i18n";
import type { ChatRow } from "../model/rows";

/** Within this many pixels of the end the list counts as read to the bottom and follows new messages. */
const BOTTOM_SLACK_PX = 24;
/** Older history loads once the user scrolls within this many pixels of the top. */
const LOAD_OLDER_PX = 600;

/** The height a row is assumed to have until it is measured. */
export const estimateRow = (row: ChatRow | undefined): number => {
  switch (row?.kind) {
    case "start":
      return 160;
    case "older":
      return 96;
    case "day":
      return 40;
    case "new":
      return 24;
    case "message":
    case "pending":
      return row.grouped ? 26 : 64;
    default:
      return 48;
  }
};

/** The first row on screen and how far below the top of the viewport it starts. */
interface Anchor {
  key: string;
  offset: number;
}

export interface MessageListProps {
  /** «Сообщения в #общий». */
  label: string;
  rows: ChatRow[];
  renderRow: (row: ChatRow) => ReactNode;
  hasOlder: boolean;
  loadingOlder: boolean;
  loadOlder: () => void;
  /** Scroll request to a message, by ID; a new object for every request. */
  scrollTo: { id: string } | null;
  /** Told when the list reaches its bottom or leaves it (it starts there). */
  onAtBottom?: (atBottom: boolean) => void;
}

/**
 * The chat's message list, virtualised (only the rows near the viewport are
 * in the DOM), from the start of the channel at the top to the newest
 * message at the bottom.
 *
 * Scrolling follows Discord:
 * - it opens at the bottom and, while the user is there, follows new messages;
 * - while they read history, new messages do not move it: a «Новые
 *   сообщения» button appears and takes them down; their own message being
 *   sent does take them down;
 * - older history loads as they near the top, and the row they were looking
 *   at stays exactly where it was.
 * Both come from one rule: after any change, a list at the bottom goes to
 * the bottom, any other keeps its first visible row at its offset.
 */
export const MessageList = ({
  label,
  rows,
  renderRow,
  hasOlder,
  loadingOlder,
  loadOlder,
  scrollTo,
  onAtBottom,
}: MessageListProps) => {
  const scroller = useRef<HTMLDivElement>(null);
  const atBottom = useRef(true);
  const anchor = useRef<Anchor | null>(null);
  const lastKey = useRef(rows.at(-1)?.key);
  const [unseen, setUnseen] = useState(false);
  /** Notes whether the list is at the bottom, telling the caller when that changes. */
  const setAtBottom = (bottom: boolean) => {
    if (atBottom.current === bottom) return;
    atBottom.current = bottom;
    onAtBottom?.(bottom);
  };

  const v = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scroller.current,
    estimateSize: (i) => estimateRow(rows[i]),
    getItemKey: (i) => rows[i]?.key ?? i,
    overscan: 10,
  });
  const items = v.getVirtualItems();
  const total = v.getTotalSize();

  /** Notes where the user is: at the bottom or not, and the first row they see. */
  const remember = () => {
    const el = scroller.current;
    if (!el) return;
    setAtBottom(el.scrollHeight - el.scrollTop - el.clientHeight <= BOTTOM_SLACK_PX);
    if (atBottom.current) setUnseen(false);
    // From every row, not only the rendered ones: those may lag the scroll.
    const first = v.measurementsCache.find((i) => i.end > el.scrollTop);
    anchor.current = first ? { key: String(first.key), offset: first.start - el.scrollTop } : null;
  };

  const toBottom = () => {
    const el = scroller.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    setAtBottom(true);
    setUnseen(false);
  };

  const followOwn = useEffectEvent(() => {
    setAtBottom(true);
    setUnseen(false);
  });

  // After the rows change: follow the bottom, or keep the first visible row in place.
  useLayoutEffect(() => {
    const el = scroller.current;
    const newest = rows.at(-1)?.key;
    const grew = newest !== lastKey.current;
    lastKey.current = newest;
    if (!el) return;
    // A message the user just sent takes them down to it, wherever they were reading.
    if (grew && rows.at(-1)?.kind === "pending") followOwn();
    if (atBottom.current) {
      el.scrollTop = el.scrollHeight;
      return;
    }
    if (grew) setUnseen(true);
    const a = anchor.current;
    if (a === null) return;
    const index = rows.findIndex((r) => r.key === a.key);
    const item = index === -1 ? undefined : v.measurementsCache[index];
    if (item) el.scrollTop = item.start - a.offset;
  }, [rows, v]);

  // Rows measured after they render change the height; a list at the bottom stays there.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && atBottom.current) el.scrollTop = el.scrollHeight;
  }, [total]);

  // Older history loads when the user nears the top, or at once while the
  // messages do not fill the view (there is nothing to scroll yet).
  const loadIfNeeded = () => {
    const el = scroller.current;
    if (!el || !hasOlder || loadingOlder) return;
    if (el.scrollTop < LOAD_OLDER_PX && !atBottom.current) loadOlder();
  };
  useEffect(() => {
    const el = scroller.current;
    if (el && hasOlder && !loadingOlder && el.scrollHeight <= el.clientHeight) loadOlder();
  }, [total, hasOlder, loadingOlder, loadOlder]);

  // Only a new request scrolls, not later changes of the rows.
  const jump = useEffectEvent((id: string) => {
    const index = rows.findIndex((r) => r.key === id);
    if (index === -1) return;
    setAtBottom(false);
    v.scrollToIndex(index, { align: "center" });
  });
  useEffect(() => {
    if (scrollTo !== null) jump(scrollTo.id);
  }, [scrollTo]);

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={scroller}
        data-virtual-scroller
        className="flex min-h-0 flex-1 flex-col overflow-y-auto overflow-x-hidden [overflow-anchor:none]"
        onScroll={() => {
          remember();
          loadIfNeeded();
        }}
      >
        <ol aria-label={label} className="relative mt-auto w-full shrink-0" style={{ height: total }}>
          {items.map((item) => {
            const row = rows[item.index];
            if (row === undefined) return null;
            return (
              <li
                key={item.key}
                ref={v.measureElement}
                data-index={item.index}
                className="absolute top-0 left-0 w-full"
                style={{ transform: `translateY(${String(item.start)}px)` }}
              >
                {renderRow(row)}
              </li>
            );
          })}
        </ol>
        <div aria-hidden className="h-4 shrink-0" />
      </div>
      {unseen && (
        <button
          type="button"
          className="absolute bottom-3 left-1/2 flex -translate-x-1/2 cursor-pointer items-center gap-1.5 rounded-full bg-accent px-4 py-1.5 text-sm font-semibold text-on-accent shadow-float hover:bg-accent-hover"
          onClick={toBottom}
        >
          {t("chat:newMessages")}
          <ArrowDown aria-hidden className="size-4" />
        </button>
      )}
    </div>
  );
};
