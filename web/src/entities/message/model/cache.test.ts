import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { HttpResponse } from "msw";
import { apiHandler, makeMessage, makeUser, problem, server } from "@/shared/test";
import {
  applyMessageDispatch,
  catchUpMessages,
  fetchHistoryPage,
  inOrder,
  type Message,
  type MessagePages,
  messagesKey,
  nextBefore,
  PAGE_SIZE,
  removeMessage,
  replaceMessage,
  upsertMessage,
} from "./cache";
import { addPending, getPending, resetPending } from "./pending";

const anna = makeUser("1", { display_name: "Аня" });
const C = "101";
const msg = (id: number, over: Partial<Message> = {}) => makeMessage(String(id), C, anna, over);
/** History pages from the newest, each newest first, of the given IDs (oldest first in each array). */
const pages = (...ids: number[][]): MessagePages => ({
  pages: ids.map((page) => page.map((id) => msg(id)).reverse()),
  pageParams: ids.map((_, i) => (i === 0 ? undefined : String(ids[i - 1]?.[0]))),
});
const order = (d: MessagePages) => inOrder(d).map((m) => Number(m.id));

afterEach(() => {
  resetPending();
});

describe("upsertMessage", () => {
  it("adds a new message at the bottom and keeps the order by ID", () => {
    expect(order(upsertMessage(pages([1, 2, 3]), msg(5)))).toEqual([1, 2, 3, 5]);
    expect(order(upsertMessage(pages([1, 2, 5]), msg(3)))).toEqual([1, 2, 3, 5]);
  });

  it("replaces a message it already has, without a duplicate", () => {
    const d = upsertMessage(pages([1, 2]), msg(2, { content: "новый" }));
    expect(order(d)).toEqual([1, 2]);
    expect(inOrder(d)[1]?.content).toBe("новый");
  });

  it("puts a message into the older page whose span holds it", () => {
    expect(order(upsertMessage(pages([10, 20], [1, 5]), msg(3)))).toEqual([1, 3, 5, 10, 20]);
  });

  it("leaves out a message older than everything while older history is still to load", () => {
    const full = Array.from({ length: PAGE_SIZE }, (_, i) => 100 + i);
    const d = pages(full);
    expect(upsertMessage(d, msg(5))).toBe(d);
  });

  it("drops the nonce, as the history never has one", () => {
    expect(inOrder(upsertMessage(pages([1]), msg(2, { nonce: "n" })))[1]).not.toHaveProperty("nonce");
  });

  it("shows the new text in the replies to an edited message", () => {
    const reply = msg(3, { reply_to_id: "1", referenced_message: { id: "1", author: anna, content: "старое" } });
    const d: MessagePages = { pages: [[reply, msg(2), msg(1)]], pageParams: [undefined] };
    const after = upsertMessage(d, msg(1, { content: "новое" }));
    expect(inOrder(after)[2]?.referenced_message?.content).toBe("новое");
  });
});

describe("replaceMessage and removeMessage", () => {
  it("ignores an edit of a message that is not loaded", () => {
    const d = pages([5, 6]);
    expect(replaceMessage(d, msg(1, { content: "x" }))).toBe(d);
  });

  it("removes a message and clears the preview of its replies", () => {
    const reply = msg(3, { reply_to_id: "1", referenced_message: { id: "1", author: anna, content: "текст" } });
    const d: MessagePages = { pages: [[reply, msg(2), msg(1)]], pageParams: [undefined] };
    const after = removeMessage(d, "1");
    expect(order(after)).toEqual([2, 3]);
    expect(inOrder(after)[1]).toMatchObject({ reply_to_id: "1", referenced_message: null });
  });

  it("keeps the same data when nothing changes", () => {
    const d = pages([1, 2]);
    expect(removeMessage(d, "9")).toBe(d);
  });
});

describe("applyMessageDispatch", () => {
  const client = (data?: MessagePages) => {
    const qc = new QueryClient();
    if (data) qc.setQueryData(messagesKey(C), data);
    return qc;
  };
  const history = (qc: QueryClient) => qc.getQueryData<MessagePages>(messagesKey(C));

  it("writes MESSAGE_CREATE, _UPDATE and _DELETE into a loaded history, idempotently", () => {
    const qc = client(pages([1, 2]));
    applyMessageDispatch(qc, "MESSAGE_CREATE", msg(3));
    applyMessageDispatch(qc, "MESSAGE_CREATE", msg(3));
    applyMessageDispatch(qc, "MESSAGE_UPDATE", msg(2, { content: "правка", edited_at: "2026-10-23T18:10:00Z" }));
    applyMessageDispatch(qc, "MESSAGE_DELETE", { id: "1", channel_id: C, guild_id: "10", last_message_id: null });
    const d = history(qc);
    if (!d) throw new Error("history expected");
    expect(order(d)).toEqual([2, 3]);
    expect(inOrder(d)[0]?.content).toBe("правка");
  });

  it("applies reactions, the user's own once, and keeps them through an edit", () => {
    const qc = client(pages([1, 2]));
    const event = (userId: string) => ({
      channel_id: C,
      guild_id: "10",
      message_id: "2",
      user_id: userId,
      emoji: { id: null, name: "🔥" },
    });
    applyMessageDispatch(qc, "MESSAGE_REACTION_ADD", event("7"), "9");
    applyMessageDispatch(qc, "MESSAGE_REACTION_ADD", event("9"), "9");
    applyMessageDispatch(qc, "MESSAGE_REACTION_ADD", event("9"), "9");
    const { reactions: _none, ...edit } = msg(2, { content: "правка" });
    applyMessageDispatch(qc, "MESSAGE_UPDATE", edit, "9");
    const d = history(qc);
    if (!d) throw new Error("history expected");
    const m = inOrder(d)[1];
    expect(m?.content).toBe("правка");
    expect(m?.reactions).toEqual([{ emoji: { id: null, name: "🔥" }, count: 2, me: true }]);
    applyMessageDispatch(qc, "MESSAGE_REACTION_REMOVE", event("7"), "9");
    expect(inOrder(history(qc) ?? { pages: [] })[1]?.reactions).toEqual([
      { emoji: { id: null, name: "🔥" }, count: 1, me: true },
    ]);
  });

  it("leaves a channel that was never loaded alone", () => {
    const qc = client();
    applyMessageDispatch(qc, "MESSAGE_CREATE", msg(3));
    expect(history(qc)).toBeUndefined();
  });

  it("confirms the pending send with the nonce of an own MESSAGE_CREATE", () => {
    const qc = client(pages([1]));
    addPending({
      nonce: "abc",
      channelId: C,
      content: "привет",
      replyTo: null,
      attachments: [],
      status: "sending",
      error: null,
    });
    applyMessageDispatch(qc, "MESSAGE_CREATE", msg(2, { nonce: "abc", content: "привет" }));
    expect(getPending(C, "abc")).toBeUndefined();
  });

  it("drops a malformed payload", () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const qc = client(pages([1]));
    applyMessageDispatch(qc, "MESSAGE_CREATE", { id: 2 });
    expect(order(history(qc) ?? pages())).toEqual([1]);
  });
});

describe("catchUpMessages", () => {
  /** A history shown on screen: a query with an observer. */
  const shown = (qc: QueryClient, channelId: string, data: MessagePages) => {
    qc.setQueryData(messagesKey(channelId), data);
    // Enabled (that is what makes it active) but never stale, so it fetches nothing itself.
    const observer = new QueryObserver(qc, {
      queryKey: messagesKey(channelId),
      queryFn: () => Promise.reject(new Error("the test fetches nothing through the observer")),
      staleTime: Number.POSITIVE_INFINITY,
    });
    return observer.subscribe(() => {});
  };

  /** The server's history: GET /messages with `after`, oldest first, as the API pages it. */
  const serve = (all: () => Message[], afters: string[] = []) => {
    server.use(
      apiHandler("get", "/channels/{channel_id}/messages", ({ request }) => {
        const url = new URL(request.url);
        const after = Number(url.searchParams.get("after"));
        afters.push(String(after));
        const limit = Number(url.searchParams.get("limit"));
        return HttpResponse.json(
          all()
            .filter((m) => Number(m.id) > after)
            .slice(0, limit),
        );
      }),
    );
    return afters;
  };

  it("fetches the loaded span and what was missed page by page, without duplicates", async () => {
    const all = Array.from({ length: 150 }, (_, i) => msg(i + 1));
    const afters = serve(() => all);
    const qc = new QueryClient();
    const stop = shown(qc, C, pages([1, 2, 3]));
    await catchUpMessages(qc);
    stop();
    const d = qc.getQueryData<MessagePages>(messagesKey(C));
    if (!d) throw new Error("history expected");
    expect(order(d)).toEqual(all.map((m) => Number(m.id)));
    // From just before the oldest loaded message.
    expect(afters).toEqual(["0", "100"]);
  });

  it("brings edits, deletions and reactions made while away into the loaded messages", async () => {
    const reacted = msg(1, { reactions: [{ emoji: { id: null, name: "👍" }, count: 1, me: false }] });
    const edited = msg(2, { content: "исправлено", edited_at: "2026-10-23T19:00:00.000Z" });
    serve(() => [reacted, edited, msg(4)]);
    const qc = new QueryClient();
    const stop = shown(qc, C, pages([1, 2, 3]));
    await catchUpMessages(qc);
    stop();
    const d = qc.getQueryData<MessagePages>(messagesKey(C));
    if (!d) throw new Error("history expected");
    expect(order(d)).toEqual([1, 2, 4]);
    const [one, two] = inOrder(d);
    expect(two?.content).toBe("исправлено");
    expect(one?.reactions).toEqual(reacted.reactions);
  });

  it("keeps a message that arrived while catching up", async () => {
    let qc = new QueryClient();
    server.use(
      apiHandler("get", "/channels/{channel_id}/messages", () => {
        // The gateway delivers message 5 while the history is in flight.
        applyMessageDispatch(qc, "MESSAGE_CREATE", msg(5));
        return HttpResponse.json([msg(1), msg(2)]);
      }),
    );
    qc = new QueryClient();
    const stop = shown(qc, C, pages([1, 2]));
    await catchUpMessages(qc);
    stop();
    expect(order(qc.getQueryData<MessagePages>(messagesKey(C)) ?? pages())).toEqual([1, 2, 5]);
  });

  it("drops a history no screen shows, to load fresh when opened", async () => {
    const qc = new QueryClient();
    qc.setQueryData(messagesKey(C), pages([1]));
    await catchUpMessages(qc);
    expect(qc.getQueryData(messagesKey(C))).toBeUndefined();
  });

  it("leaves a history it may not read any more as it is", async () => {
    server.use(apiHandler("get", "/channels/{channel_id}/messages", () => problem(403, "missing_permission")));
    const qc = new QueryClient();
    const stop = shown(qc, C, pages([1]));
    await catchUpMessages(qc);
    stop();
    expect(order(qc.getQueryData<MessagePages>(messagesKey(C)) ?? pages())).toEqual([1]);
  });
});

describe("the end of the history", () => {
  it("is known from the page that came back short, not from what deletions left", async () => {
    const older = Array.from({ length: 120 }, (_, i) => msg(i + 1)).reverse();
    server.use(
      apiHandler("get", "/channels/{channel_id}/messages", ({ request }) => {
        const before = new URL(request.url).searchParams.get("before");
        const rest = before === null ? older : older.filter((m) => Number(m.id) < Number(before));
        return HttpResponse.json(rest.slice(0, PAGE_SIZE));
      }),
    );
    const newest = await fetchHistoryPage(C, undefined);
    // A deletion leaves 49 of the 50: older history is still there.
    const left = removeMessage({ pages: [newest], pageParams: [undefined] }, "100").pages[0] ?? [];
    expect(left).toHaveLength(PAGE_SIZE - 1);
    expect(nextBefore(C, left, undefined)).toBe("71");

    const third = await fetchHistoryPage(C, "21");
    expect(third).toHaveLength(20);
    expect(nextBefore(C, third, "21")).toBeUndefined();
  });

  it("lets an older message in only when nothing older is left to load", () => {
    const shrunk: MessagePages = {
      pages: [Array.from({ length: 49 }, (_, i) => msg(100 - i))],
      pageParams: [undefined],
    };
    expect(order(upsertMessage(shrunk, msg(10), false))).not.toContain(10);
    expect(order(upsertMessage(shrunk, msg(10), true))).toContain(10);
  });
});
