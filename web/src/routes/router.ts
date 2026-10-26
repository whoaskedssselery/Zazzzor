import type { QueryClient } from "@tanstack/react-query";
import { createRouter, type RouterHistory } from "@tanstack/react-router";
import { indexRoute } from "./index";
import { rootRoute } from "./root";

/**
 * Code-based routing: every route is a `createRoute` in src/routes/ (or in its
 * feature folder) and is listed here. No generated route tree.
 */
const routeTree = rootRoute.addChildren([indexRoute]);

export function createAppRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
    ...(history ? { history } : {}),
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
