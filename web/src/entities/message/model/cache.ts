import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import { api, type components, isChatEvent, parseChatEvent, unwrap } from "@/shared/api";
import { compareIds } from "@/shared/lib";
import { confirmPending } from "./pending";
import { withReaction } from "./reactions";

export type Message = components["schemas"]["Message"];
export type MessageStub = components["schemas"]["MessageStub"];

/**
 * The cached history of a channel: pages from the newest, each newest first,
 * as GET /messages returns them without `after`. A page parameter is the
 * `before` it was loaded with (undefined for the newest page).
 */
export type MessagePages = InfiniteData<Message[], string | undefined>;

/** Messages per history page. */
export const PAGE_SIZE = 50;
/** Messages per page while catching up after a reconnect (the API maximum). */
const CATCH_UP_SIZE = 100;
/** Catch-up pages before the history is reloaded from the newest instead. */
const CATCH_UP_PAGES = 5;

export const messagesKey = (channelId: string) => ["messages", channelId] as const;

/** One page of GET /channels/{id}/messages. */
export async function fetchMessages(
  channelId: string,
  query: { before?: string; after?: string; limit?: number },
  signal?: AbortSignal,
): Promise<Message[]> {
  return unwrap(
    await api.GET("/channels/{channel_id}/messages", {
      params: { path: { channel_id: channelId }, query },
      ...(signal ? { signal } : {}),
    }),
  );
}

/** The newest loaded message, if any. */
export const newestId = (data: Pick<MessagePages, "pages">): string | undefined =>
  data.pages.find((p) => p.length > 0)?.[0]?.id;

/** The history oldest first, as the chat shows it. */
export const inOrder = (data: Pick<MessagePages, "pages">): Message[] => data.pages.flat().reverse();

/** The stub a reply carries of message `m`. */
const stubOf = (m: Message): MessageStub => ({ id: m.id, author: m.author, content: m.content });

/** Every message through `fn`, keeping the pages (and the data) whose messages it left alone. */
const mapMessages = (data: MessagePages, fn: (m: Message) => Message | null): MessagePages => {
  const pages = data.pages.map((page) => {
    const next = page.map(fn);
    return next.every((m, i) => m === page[i]) ? page : next.filter((m): m is Message => m !== null);
  });
  return pages.every((page, i) => page === data.pages[i]) ? data : { ...data, pages };
};

/**
 * The history with `m` in it: a message already there is replaced, a new one
 * goes into the page whose span holds it. A message older than everything
 * loaded is left out while older history is still to load: the history
 * fetch brings it. The `nonce` of an own message is not kept, as in the
 * history.
 */
export function upsertMessage(data: MessagePages, incoming: Message): MessagePages {
  const { nonce: _nonce, ...m } = incoming;
  if (data.pages.some((page) => page.some((x) => x.id === m.id))) {
    return withReplies(
      mapMessages(data, (x) => (x.id === m.id ? m : x)),
      m,
    );
  }
  const last = data.pages.length - 1;
  const p = data.pages.findIndex((page, i) => {
    const oldest = page.at(-1);
    return i === last || oldest === undefined || compareIds(m.id, oldest.id) > 0;
  });
  const page = data.pages[p];
  if (page === undefined) return data;
  const oldest = page.at(-1);
  if (p === last && page.length >= PAGE_SIZE && oldest !== undefined && compareIds(m.id, oldest.id) < 0) return data;
  const at = page.findIndex((x) => compareIds(x.id, m.id) < 0);
  const next = at === -1 ? [...page, m] : [...page.slice(0, at), m, ...page.slice(at)];
  return { ...data, pages: data.pages.map((x, i) => (i === p ? next : x)) };
}

/** The replies to `m` showing its current text. */
const withReplies = (data: MessagePages, m: Message): MessagePages =>
  mapMessages(data, (x) =>
    x.reply_to_id === m.id && x.referenced_message !== null ? { ...x, referenced_message: stubOf(m) } : x,
  );

/**
 * An edit of a loaded message; an edit of one not loaded changes nothing.
 * MESSAGE_UPDATE carries no reactions (one payload for everyone cannot say
 * whose they are), so the loaded ones stay.
 */
export function replaceMessage(data: MessagePages, m: Message): MessagePages {
  const held = data.pages.flat().find((x) => x.id === m.id);
  if (held === undefined) return data;
  return upsertMessage(
    data,
    m.reactions === undefined && held.reactions !== undefined ? { ...m, reactions: held.reactions } : m,
  );
}

/** Message `id` through `fn`, if it is loaded. */
export const updateMessage = (data: MessagePages, id: string, fn: (m: Message) => Message): MessagePages =>
  mapMessages(data, (x) => (x.id === id ? fn(x) : x));

/** The history without message `id`; replies to it keep `reply_to_id` and lose the preview, as the server says. */
export function removeMessage(data: MessagePages, id: string): MessagePages {
  return mapMessages(data, (x) => {
    if (x.id === id) return null;
    return x.reply_to_id === id && x.referenced_message !== null ? { ...x, referenced_message: null } : x;
  });
}

/** Applies `fn` to the cached history of `channelId`, if it is loaded. */
export const updateHistory = (qc: QueryClient, channelId: string, fn: (d: MessagePages) => MessagePages): void => {
  qc.setQueryData<MessagePages>(messagesKey(channelId), (old) => (old === undefined ? old : fn(old)));
};

/**
 * Applies a chat DISPATCH to the message caches: MESSAGE_CREATE, _UPDATE,
 * _DELETE and the reactions change the history of their channel when it is
 * loaded (a channel never opened is left to load fresh). An own
 * MESSAGE_CREATE with the nonce of a pending send confirms it. `meId` tells
 * the user's own reactions. Anything else is ignored.
 */
export function applyMessageDispatch(qc: QueryClient, t: string, d: unknown, meId: string | null = null): void {
  if (!isChatEvent(t)) return;
  switch (t) {
    case "MESSAGE_CREATE": {
      const m = parseChatEvent(t, d);
      if (m === null) return;
      updateHistory(qc, m.channel_id, (h) => upsertMessage(h, m));
      if (m.nonce !== undefined) confirmPending(m.channel_id, m.nonce);
      return;
    }
    case "MESSAGE_UPDATE": {
      const m = parseChatEvent(t, d);
      if (m !== null) updateHistory(qc, m.channel_id, (h) => replaceMessage(h, m));
      return;
    }
    case "MESSAGE_DELETE": {
      const e = parseChatEvent(t, d);
      if (e !== null) updateHistory(qc, e.channel_id, (h) => removeMessage(h, e.id));
      return;
    }
    case "MESSAGE_REACTION_ADD":
    case "MESSAGE_REACTION_REMOVE": {
      const e = parseChatEvent(t, d);
      if (e === null) return;
      const add = t === "MESSAGE_REACTION_ADD";
      updateHistory(qc, e.channel_id, (h) =>
        updateMessage(h, e.message_id, (m) => withReaction(m, e.emoji, e.user_id === meId, add)),
      );
      return;
    }
    case "TYPING_START":
      return;
  }
}

/**
 * Brings the loaded histories up to date after a reconnect: the events
 * missed while away are fetched with `after=<newest>`, page by page until a
 * short one, and merged by ID (no duplicates). A history too far behind
 * (more than CATCH_UP_PAGES pages), or empty, loads again from the newest
 * page instead; a history no screen shows is dropped, to load fresh when
 * opened. A channel that cannot be read any more is left to the page,
 * which leaves it.
 */
export async function catchUpMessages(qc: QueryClient): Promise<void> {
  const queries = qc.getQueryCache().findAll({ queryKey: ["messages"] });
  await Promise.all(
    queries.map(async (q) => {
      const [, channelId] = q.queryKey;
      const data = q.state.data as MessagePages | undefined;
      if (typeof channelId !== "string" || data === undefined) return;
      if (!q.isActive()) {
        qc.removeQueries({ queryKey: q.queryKey, exact: true });
        return;
      }
      let after = newestId(data);
      for (let n = 0; after !== undefined && n < CATCH_UP_PAGES; n++) {
        let page: Message[];
        try {
          page = await fetchMessages(channelId, { after, limit: CATCH_UP_SIZE });
        } catch {
          return;
        }
        for (const m of page) updateHistory(qc, channelId, (h) => upsertMessage(h, m));
        if (page.length < CATCH_UP_SIZE) return;
        after = page.at(-1)?.id;
      }
      await qc.resetQueries({ queryKey: q.queryKey, exact: true });
    }),
  );
}
