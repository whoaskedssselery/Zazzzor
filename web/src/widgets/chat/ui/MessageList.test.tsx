import { act, fireEvent, render, screen } from "@testing-library/react";
import { fakeListLayout, makeMessage, makeUser } from "@/shared/test";
import type { ChatRow } from "../model/rows";
import { estimateRow, MessageList, type MessageListProps } from "./MessageList";

// Every row is exactly as tall as the list estimates it, so measuring moves
// nothing: keeping the view still while rows are measured is TanStack
// Virtual's job, not this list's. Rows render their key as text; message
// rows here are all grouped.
const VIEW = 400;
const ROW = estimateRow({ kind: "message", key: "", message: makeMessage("1", "101", null), grouped: true });
const heightOf = (text: string): number =>
  text === "older"
    ? estimateRow({ kind: "older", key: "older" })
    : text === "start"
      ? estimateRow({ kind: "start", key: "start" })
      : ROW;

let restoreLayout: () => void = () => {};
beforeEach(() => {
  restoreLayout = fakeListLayout({ viewport: VIEW, row: (el) => heightOf(el.textContent) });
});
afterEach(() => {
  restoreLayout();
});

const anna = makeUser("1");
/** Message rows with IDs from..to-1, oldest first, after a row for older history. */
const rowsOf = (from: number, to: number, hasOlder = true): ChatRow[] => [
  hasOlder ? { kind: "older", key: "older" } : { kind: "start", key: "start" },
  ...Array.from({ length: to - from }, (_, i): ChatRow => {
    const id = String(from + i);
    return { kind: "message", key: id, message: makeMessage(id, "101", anna), grouped: true };
  }),
];

const props = (over: Partial<MessageListProps> = {}): MessageListProps => ({
  label: "Сообщения в #общий",
  rows: rowsOf(100, 200),
  renderRow: (row) => <p>{row.key}</p>,
  hasOlder: true,
  loadingOlder: false,
  loadOlder: () => {},
  scrollTo: null,
  ...over,
});

const scroller = () => {
  const el = document.querySelector<HTMLElement>("[data-virtual-scroller]");
  if (!el) throw new Error("no scroller");
  return el;
};
const scrollTo = (top: number) => {
  act(() => {
    scroller().scrollTop = top;
    fireEvent.scroll(scroller());
  });
};
const atBottom = () => scroller().scrollTop === scroller().scrollHeight;
/** A browser fires scroll after a programmatic scroll; jsdom does not, so the test does. */
const settle = () => {
  act(() => {
    fireEvent.scroll(scroller());
  });
};

describe("MessageList", () => {
  it("opens at the newest message", () => {
    render(<MessageList {...props()} />);
    expect(atBottom()).toBe(true);
    settle();
    expect(screen.getByText("199")).toBeTruthy();
    // Virtualised: the oldest rows are not in the DOM.
    expect(screen.queryByText("100")).toBeNull();
  });

  it("follows a new message while the user is at the bottom", () => {
    const { rerender } = render(<MessageList {...props()} />);
    rerender(<MessageList {...props({ rows: rowsOf(100, 201) })} />);
    expect(atBottom()).toBe(true);
    settle();
    expect(screen.getByText("200")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Новые сообщения" })).toBeNull();
  });

  it("stays put for a new message while the user reads history, and offers a way down", () => {
    const { rerender } = render(<MessageList {...props()} />);
    scrollTo(1000);
    rerender(<MessageList {...props({ rows: rowsOf(100, 201) })} />);
    expect(scroller().scrollTop).toBe(1000);
    fireEvent.click(screen.getByRole("button", { name: "Новые сообщения" }));
    expect(atBottom()).toBe(true);
    expect(screen.queryByRole("button", { name: "Новые сообщения" })).toBeNull();
  });

  it("goes down to the user's own message being sent, wherever they were reading", () => {
    const onAtBottom = vi.fn();
    const { rerender } = render(<MessageList {...props({ onAtBottom })} />);
    scrollTo(1000);
    const pending: ChatRow = {
      kind: "pending",
      key: "nonce-1",
      pending: {
        nonce: "nonce-1",
        channelId: "101",
        content: "привет",
        replyTo: null,
        attachments: [],
        status: "sending",
        error: null,
        sentAt: Date.now(),
      },
      grouped: true,
    };
    rerender(<MessageList {...props({ onAtBottom, rows: [...rowsOf(100, 200), pending] })} />);
    expect(atBottom()).toBe(true);
    expect(onAtBottom).toHaveBeenLastCalledWith(true);
    expect(screen.queryByRole("button", { name: "Новые сообщения" })).toBeNull();
  });

  it("keeps the first visible message in place when older history loads above", () => {
    const { rerender } = render(<MessageList {...props()} />);
    scrollTo(1000);
    rerender(<MessageList {...props({ rows: rowsOf(50, 200) })} />);
    // 50 rows came in above: the view moved down by exactly their height.
    expect(scroller().scrollTop).toBe(1000 + 50 * ROW);
  });

  it("loads the next older page near the top, once at a time", () => {
    const loadOlder = vi.fn();
    const { rerender } = render(<MessageList {...props({ loadOlder })} />);
    expect(loadOlder).not.toHaveBeenCalled();
    scrollTo(0);
    expect(loadOlder).toHaveBeenCalledTimes(1);
    rerender(<MessageList {...props({ loadOlder, loadingOlder: true })} />);
    scrollTo(10);
    expect(loadOlder).toHaveBeenCalledTimes(1);
  });

  it("asks for nothing more once the start of the channel is loaded", () => {
    const loadOlder = vi.fn();
    render(<MessageList {...props({ loadOlder, hasOlder: false, rows: rowsOf(100, 105, false) })} />);
    scrollTo(0);
    expect(loadOlder).not.toHaveBeenCalled();
    expect(screen.getByText("start")).toBeTruthy();
  });
});
