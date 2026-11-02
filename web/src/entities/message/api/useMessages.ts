import { useInfiniteQuery } from "@tanstack/react-query";
import { fetchMessages, messagesKey, PAGE_SIZE } from "../model/cache";

/**
 * The history of `channelId`, loaded from the newest page; `fetchNextPage`
 * loads the next older one (`before` its oldest message) and `hasNextPage`
 * says whether there is one. The gateway keeps it current (see
 * applyMessageDispatch) and a reconnect catches it up (catchUpMessages), so
 * it never goes stale and is never refetched on its own. `enabled` is false
 * while the user may not read the history.
 */
export const useMessages = (channelId: string, enabled: boolean) =>
  useInfiniteQuery({
    queryKey: messagesKey(channelId),
    queryFn: ({ pageParam, signal }) =>
      fetchMessages(
        channelId,
        pageParam === undefined ? { limit: PAGE_SIZE } : { before: pageParam, limit: PAGE_SIZE },
        signal,
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => (last.length < PAGE_SIZE ? undefined : last.at(-1)?.id),
    enabled,
    staleTime: Number.POSITIVE_INFINITY,
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
  });
