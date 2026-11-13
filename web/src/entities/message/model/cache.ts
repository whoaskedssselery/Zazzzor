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
/** Catch-up pages (the loaded history and what was missed) before the history is reloaded from the newest instead. */
const CATCH_UP_PAGES = 10;

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

/**
 * Per channel, the `before` of every history page that came back short:
 * nothing older exists past it. Known when the page loads, because a
 * deletion later shortens the page in the cache without the history ending.
 */
const ends = new Map<string, Set<string | undefined>>();

const markEnd = (channelId: string, before: string | undefined, ended: boolean) => {
  const set = ends.get(channelId) ?? new Set();
  if (ended) set.add(before);
  else set.delete(before);
  ends.set(channelId, set);
};

/** The history page of `channelId` older than `before` (the newest without it), noting whether it is the last. */
export async function fetchHistoryPage(
  channelId: string,
  before: string | undefined,
  signal?: AbortSignal,
): Promise<Message[]> {
  const page = await fetchMessages(
    channelId,
    before === undefined ? { limit: PAGE_SIZE } : { before, limit: PAGE_SIZE },
    signal,
  );
  markEnd(channelId, before, page.length < PAGE_SIZE);
  return page;
}

/** Nothing older than the page loaded with `before` exists. */
const endedAt = (channelId: string, before: string | undefined): boolean => ends.get(channelId)?.has(before) ?? false;

/** The whole history of `channelId` is loaded: its oldest page came back short. */
const historyEnded = (channelId: string, data: Pick<MessagePages, "pageParams">): boolean =>
  endedAt(channelId, data.pageParams.at(-1));

/**
 * The `before` of the page after `last` (loaded with `lastBefore`), or
 * undefined when the history ended there. A page that deletions emptied
 * is followed from where it started.
 */
export const nextBefore = (channelId: string, last: Message[], lastBefore: string | undefined): string | undefined =>
  endedAt(channelId, lastBefore) ? undefined : (last.at(-1)?.id ?? lastBefore);

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
 * loaded is left out while older history is still to load (`ended` false):
 * the history fetch brings it. Without `ended`, a full last page means
 * there is more. The `nonce` of an own message is not kept, as in the
 * history.
 */
export function upsertMessage(data: MessagePages, incoming: Message, ended?: boolean): MessagePages {
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
  const more = ended === undefined ? page.length >= PAGE_SIZE : !ended;
  if (p === last && more && oldest !== undefined && compareIds(m.id, oldest.id) < 0) return data;
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
 * The changes the gateway made to each history being caught up, in order:
 * they are made again on what the server returned, which may predate them.
 */
const catchingUp = new Map<string, ((h: MessagePages) => MessagePages)[]>();

/** Applies `fn` to the history of `channelId` now, and again after a catch-up in flight. */
const applyLive = (qc: QueryClient, channelId: string, fn: (d: MessagePages) => MessagePages): void => {
  catchingUp.get(channelId)?.push(fn);
  updateHistory(qc, channelId, fn);
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
      applyLive(qc, m.channel_id, (h) => upsertMessage(h, m, historyEnded(m.channel_id, h)));
      if (m.nonce !== undefined) confirmPending(m.channel_id, m.nonce);
      return;
    }
    case "MESSAGE_UPDATE": {
      const m = parseChatEvent(t, d);
      if (m !== null) applyLive(qc, m.channel_id, (h) => replaceMessage(h, m));
      return;
    }
    case "MESSAGE_DELETE": {
      const e = parseChatEvent(t, d);
      if (e !== null) applyLive(qc, e.channel_id, (h) => removeMessage(h, e.id));
      return;
    }
    case "MESSAGE_REACTION_ADD":
    case "MESSAGE_REACTION_REMOVE": {
      const e = parseChatEvent(t, d);
      if (e === null) return;
      const add = t === "MESSAGE_REACTION_ADD";
      applyLive(qc, e.channel_id, (h) =>
        updateMessage(h, e.message_id, (m) => withReaction(m, e.emoji, e.user_id === meId, add)),
      );
      return;
    }
    case "TYPING_START":
      return;
  }
}

/** The oldest loaded message, if any. */
const oldestId = (data: Pick<MessagePages, "pages">): string | undefined => data.pages.flat().at(-1)?.id;

/** The ID just below `id`: `after` it takes `id` itself in. */
const justBefore = (id: string): string => (BigInt(id) - 1n).toString();

/**
 * `messages` (oldest first) as history pages from the newest, PAGE_SIZE
 * each, every page's parameter the `before` that would load it. Whether
 * anything older exists carries over from `ended`.
 */
const asPages = (channelId: string, messages: Message[], ended: boolean): MessagePages => {
  const newestFirst = [...messages].reverse();
  const pages: Message[][] = [];
  const pageParams: (string | undefined)[] = [];
  for (let i = 0; i < newestFirst.length || pages.length === 0; i += PAGE_SIZE) {
    pageParams.push(pages.at(-1)?.at(-1)?.id);
    pages.push(newestFirst.slice(i, i + PAGE_SIZE));
  }
  markEnd(channelId, pageParams.at(-1), ended);
  return { pages, pageParams };
};

/**
 * Brings the loaded histories up to date after a reconnect. Events missed
 * while away touch messages already loaded too (edits, deletions,
 * reactions), so the whole loaded span and everything after it is fetched
 * again with `after`, page by page until a short one, and replaces the
 * history as a snapshot: what the server no longer has goes, and the
 * gateway's changes made while the fetch was in flight (new messages,
 * edits, deletions, reactions) are made again on top of it. A history too long for that (more than CATCH_UP_PAGES
 * pages), or empty, loads again from the newest page instead; a history no
 * screen shows is dropped, to load fresh when opened. A channel that cannot
 * be read any more is left to the page, which leaves it.
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
      const oldest = oldestId(data);
      const ended = historyEnded(channelId, data);
      const fetched: Message[] = [];
      const live: ((h: MessagePages) => MessagePages)[] = [];
      catchingUp.set(channelId, live);
      try {
        let after = oldest === undefined ? undefined : justBefore(oldest);
        for (let n = 0; after !== undefined && n < CATCH_UP_PAGES; n++) {
          let page: Message[];
          try {
            page = await fetchMessages(channelId, { after, limit: CATCH_UP_SIZE });
          } catch {
            return;
          }
          fetched.push(...page);
          if (page.length < CATCH_UP_SIZE) {
            updateHistory(qc, channelId, () => live.reduce((h, fn) => fn(h), asPages(channelId, fetched, ended)));
            return;
          }
          after = page.at(-1)?.id;
        }
      } finally {
        if (catchingUp.get(channelId) === live) catchingUp.delete(channelId);
      }
      await qc.resetQueries({ queryKey: q.queryKey, exact: true });
    }),
  );
}
