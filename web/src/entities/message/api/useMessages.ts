import { useInfiniteQuery } from "@tanstack/react-query";
import { fetchHistoryPage, messagesKey, nextBefore } from "../model/cache";

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
    queryFn: ({ pageParam, signal }) => fetchHistoryPage(channelId, pageParam, signal),
    initialPageParam: undefined as string | undefined,
    // Whether the history ended is known from the page as it came, not as deletions left it.
    getNextPageParam: (last, _all, lastBefore) => nextBefore(channelId, last, lastBefore),
    enabled,
    staleTime: Number.POSITIVE_INFINITY,
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
  });
