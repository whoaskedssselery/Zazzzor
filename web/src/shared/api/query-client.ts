import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "./client";

/**
 * Query defaults for the app. A 4xx answer will not change on retry (no
 * permission, not found, bad input), so only transport and 5xx errors are
 * retried, twice.
 */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: (failures, e) => failures < 2 && !(e instanceof ApiError && e.status < 500),
        refetchOnWindowFocus: false,
      },
      mutations: { retry: false },
    },
  });
}
