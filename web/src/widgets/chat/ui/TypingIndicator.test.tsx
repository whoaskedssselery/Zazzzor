import { act, render, screen, waitFor } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { getGateway } from "@/shared/api";
import { dispatchFrame, makeMessage, makeReady, makeUser, readyGateway, server } from "@/shared/test";
import { TYPING_SHOWN_MS, TypingIndicator, typingText } from "./TypingIndicator";

const names: Record<string, string> = { "1": "Иван", "2": "Пётр", "3": "Аня", "100": "Я" };
const C = "101";

let client: WebSocketHandlerConnection["client"] | undefined;
let seq = 1;

/** Sends a DISPATCH over the test gateway; the connection hands it to its subscribers. */
const dispatch = (t: string, d: unknown) => {
  act(() => {
    seq += 1;
    client?.send(dispatchFrame(t, seq, d));
  });
};

beforeEach(async () => {
  server.use(
    readyGateway(makeReady(makeUser("100")), (c) => {
      client = c;
    }),
  );
  getGateway().start();
  await waitFor(() => {
    expect(getGateway().status).toBe("ready");
  });
});
const typingStart = (userId: string, channelId = C) => {
  dispatch("TYPING_START", {
    channel_id: channelId,
    guild_id: "10",
    user_id: userId,
    timestamp: "2026-10-23T21:00:00Z",
  });
};

const renderIndicator = () => render(<TypingIndicator channelId={C} meId="100" nameOf={(id) => names[id] ?? "?"} />);

afterEach(() => {
  getGateway().stop();
  client = undefined;
  vi.useRealTimers();
});

describe("typingText", () => {
  it("names one or two typists and counts more", () => {
    expect(typingText([])).toBeNull();
    expect(typingText(["Иван"])).toBe("Иван печатает…");
    expect(typingText(["Иван", "Пётр"])).toBe("Иван и Пётр печатают…");
    expect(typingText(["Иван", "Пётр", "Аня"])).toBe("Несколько человек печатают…");
  });
});

describe("TypingIndicator", () => {
  it("shows who types here, never the user and never another channel", async () => {
    renderIndicator();
    typingStart("100");
    typingStart("2", "999");
    typingStart("1");
    // Frames arrive in order: once Иван shows, the two before were seen and left out.
    expect(await screen.findByText("Иван печатает…")).toBeTruthy();
    typingStart("2");
    expect(await screen.findByText("Иван и Пётр печатают…")).toBeTruthy();
  });

  it(`drops a notice after ${String(TYPING_SHOWN_MS / 1000)} s unless renewed`, async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderIndicator();
    typingStart("1");
    await screen.findByText("Иван печатает…");
    await act(() => vi.advanceTimersByTimeAsync(TYPING_SHOWN_MS - 1000));
    typingStart("1");
    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(screen.getByText("Иван печатает…")).toBeTruthy();
    await act(() => vi.advanceTimersByTimeAsync(TYPING_SHOWN_MS));
    expect(screen.queryByText(/печатает/)).toBeNull();
  });

  it("drops a notice when its author's message arrives", async () => {
    renderIndicator();
    typingStart("1");
    await screen.findByText("Иван печатает…");
    dispatch("MESSAGE_CREATE", makeMessage("9", C, makeUser("1")));
    await waitFor(() => {
      expect(screen.queryByText(/печатает/)).toBeNull();
    });
  });
});
