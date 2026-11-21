import { useInfiniteQuery } from "@tanstack/react-query";
import { api, type components, unwrap } from "@/shared/api";
import { t } from "@/shared/i18n";

export type SearchResult = components["schemas"]["MessageSearchResult"];
export type FoundMessage = SearchResult["messages"][number];

/** Where to search: a whole server, or one channel (a DM or a group too). */
export type SearchScope = { kind: "guild"; guildId: string } | { kind: "channel"; channelId: string };

/** What was asked: the words, and the author and channel filters ("" for any). */
export interface SearchAsk {
  content: string;
  authorId: string;
  channelId: string;
}

/** Results per page; the server's largest. */
export const SEARCH_PAGE = 25;

const scopeKey = (scope: SearchScope) => (scope.kind === "guild" ? `g${scope.guildId}` : `c${scope.channelId}`);

const fetchPage = async (scope: SearchScope, ask: SearchAsk, before: string | null): Promise<SearchResult> => {
  const query = {
    limit: SEARCH_PAGE,
    ...(ask.content.trim() === "" ? {} : { content: ask.content.trim() }),
    ...(ask.authorId === "" ? {} : { author_id: ask.authorId }),
    ...(before === null ? {} : { before }),
  };
  if (scope.kind === "guild") {
    return unwrap(
      await api.GET("/guilds/{guild_id}/messages/search", {
        params: {
          path: { guild_id: scope.guildId },
          query: { ...query, ...(ask.channelId === "" ? {} : { channel_id: ask.channelId }) },
        },
      }),
    );
  }
  return unwrap(
    await api.GET("/channels/{channel_id}/messages/search", {
      params: { path: { channel_id: scope.channelId }, query },
    }),
  );
};

/**
 * The results of `ask` in `scope`, a page at a time (newest first); null
 * asks nothing yet. Kept a minute: new messages do not join old results.
 */
export const useSearch = (scope: SearchScope, ask: SearchAsk | null) =>
  useInfiniteQuery({
    queryKey: ["search", scopeKey(scope), ask?.content.trim(), ask?.authorId, ask?.channelId],
    queryFn: ({ pageParam }) => {
      if (ask === null) throw new Error("no search asked");
      return fetchPage(scope, ask, pageParam);
    },
    initialPageParam: null as string | null,
    getNextPageParam: (last) => (last.messages.length === SEARCH_PAGE ? (last.messages.at(-1)?.id ?? null) : null),
    enabled: ask !== null,
    staleTime: 60_000,
    retry: false,
  });

/** Whether `ask` has something to search by: a word or an author. */
export const canSearch = (ask: SearchAsk): boolean => /[\p{L}\p{N}]/u.test(ask.content) || ask.authorId !== "";

/** «Найдено: 3», «Найдено: 1000+». */
export const foundText = (total: number): string =>
  total === 0 ? t("chat:search.none") : t("chat:search.found", { count: total, plus: total >= 1000 ? "+" : "" });
