import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse } from "msw";
import type { ReactNode } from "react";
import { resetGuilds, setConnectionStatus } from "@/entities/guild";
import {
  applyMessageDispatch,
  inOrder,
  type Message,
  type MessagePages,
  messagesKey,
  resetPending,
  usePendingMessages,
} from "@/entities/message";
import { apiHandler, makeMessage, makeUser, problem, server } from "@/shared/test";
import { OFFLINE, SEND_WAIT_MS, TOO_FAST, useSendMessage } from "./useSendMessage";

const me = makeUser("100");
const C = "101";

/** A client with the channel's history loaded (one older message). */
const setup = () => {
  const qc = new QueryClient();
  qc.setQueryData<MessagePages>(messagesKey(C), { pages: [[makeMessage("1", C, me)]], pageParams: [undefined] });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const hook = renderHook(() => ({ sender: useSendMessage(C), pending: usePendingMessages(C) }), { wrapper });
  const history = () => inOrder(qc.getQueryData<MessagePages>(messagesKey(C)) ?? { pages: [], pageParams: [] });
  return { qc, hook, history };
};

/** The server's copy of a sent message. */
const echo = (id: string, content: string, nonce?: string): Message =>
  makeMessage(id, C, me, { content, ...(nonce === undefined ? {} : { nonce }) });

beforeEach(() => {
  setConnectionStatus("ready");
});

afterEach(() => {
  resetPending();
  resetGuilds();
  vi.useRealTimers();
});

describe("useSendMessage", () => {
  it("shows the message as sending, then puts the confirmed one in the history", async () => {
    let release: (() => void) | null = null;
    server.use(
      apiHandler("post", "/channels/{channel_id}/messages", async ({ request }) => {
        const body = await request.json();
        await new Promise<void>((r) => {
          release = r;
        });
        return HttpResponse.json(echo("2", body.content ?? "", body.nonce), { status: 201 });
      }),
    );
    const { hook, history } = setup();
    act(() => {
      hook.result.current.sender.send("привет", null);
    });
    expect(hook.result.current.pending).toMatchObject([{ content: "привет", status: "sending" }]);
    await waitFor(() => {
      expect(release).not.toBeNull();
    });
    act(() => {
      release?.();
    });
    await waitFor(() => {
      expect(hook.result.current.pending).toEqual([]);
    });
    expect(history().map((m) => m.content)).toEqual(["сообщение 1", "привет"]);
  });

  it.each(["response first", "event first"])("matches by nonce with the %s, without a duplicate", async (order) => {
    let nonce = "";
    let respond: () => void = () => {};
    server.use(
      apiHandler("post", "/channels/{channel_id}/messages", async ({ request }) => {
        const body = await request.json();
        nonce = body.nonce ?? "";
        await new Promise<void>((r) => {
          respond = r;
        });
        return HttpResponse.json(echo("2", body.content ?? "", body.nonce), { status: 201 });
      }),
    );
    const { qc, hook, history } = setup();
    act(() => {
      hook.result.current.sender.send("раз", null);
    });
    await waitFor(() => {
      expect(nonce).not.toBe("");
    });
    const event = () => {
      applyMessageDispatch(qc, "MESSAGE_CREATE", echo("2", "раз", nonce));
    };
    if (order === "event first") act(event);
    act(() => {
      respond();
    });
    await waitFor(() => {
      expect(hook.result.current.pending).toEqual([]);
    });
    if (order === "response first") act(event);
    expect(history().map((m) => m.id)).toEqual(["1", "2"]);
  });

  it("marks a rate-limited send failed with «Слишком часто, подождите», and a retry keeps its nonce", async () => {
    const nonces: string[] = [];
    let limited = true;
    server.use(
      apiHandler("post", "/channels/{channel_id}/messages", async ({ request }) => {
        const body = await request.json();
        nonces.push(body.nonce ?? "");
        if (limited) return problem(429, "rate_limited", undefined, { "Retry-After": "1" });
        return HttpResponse.json(echo("2", body.content ?? "", body.nonce), { status: 201 });
      }),
    );
    const { hook, history } = setup();
    act(() => {
      hook.result.current.sender.send("быстро", null);
    });
    await waitFor(() => {
      expect(hook.result.current.pending).toMatchObject([{ status: "failed", error: TOO_FAST }]);
    });
    limited = false;
    const failed = hook.result.current.pending[0];
    act(() => {
      if (failed) hook.result.current.sender.retry(failed.nonce);
    });
    await waitFor(() => {
      expect(hook.result.current.pending).toEqual([]);
    });
    expect(nonces).toHaveLength(2);
    expect(nonces[0]).toBe(nonces[1]);
    expect(history().map((m) => m.content)).toEqual(["сообщение 1", "быстро"]);
  });

  it("holds a send made while reconnecting until READY", async () => {
    let posts = 0;
    server.use(
      apiHandler("post", "/channels/{channel_id}/messages", async ({ request }) => {
        posts += 1;
        const body = await request.json();
        return HttpResponse.json(echo("2", body.content ?? "", body.nonce), { status: 201 });
      }),
    );
    setConnectionStatus("reconnecting");
    const { hook } = setup();
    act(() => {
      hook.result.current.sender.send("после", null);
    });
    await new Promise((r) => setTimeout(r, 30));
    expect(posts).toBe(0);
    act(() => {
      setConnectionStatus("ready");
    });
    await waitFor(() => {
      expect(hook.result.current.pending).toEqual([]);
    });
    expect(posts).toBe(1);
  });

  it("fails a send when the connection does not come back in time", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    setConnectionStatus("reconnecting");
    const { hook } = setup();
    act(() => {
      hook.result.current.sender.send("в пустоту", null);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(SEND_WAIT_MS);
    });
    expect(hook.result.current.pending).toMatchObject([{ status: "failed", error: OFFLINE }]);
  });

  it("forgets a failed message the user discards", async () => {
    server.use(apiHandler("post", "/channels/{channel_id}/messages", () => problem(500, "internal")));
    const { hook } = setup();
    act(() => {
      hook.result.current.sender.send("x", null);
    });
    await waitFor(() => {
      expect(hook.result.current.pending[0]?.status).toBe("failed");
    });
    act(() => {
      const p = hook.result.current.pending[0];
      if (p) hook.result.current.sender.discard(p.nonce);
    });
    expect(hook.result.current.pending).toEqual([]);
  });
});
