import type { QueryClient } from "@tanstack/react-query";
import { createRouter, type RouterHistory } from "@tanstack/react-router";
import { appRoutes } from "./app-routes";
import { loginRoute, registerRoute } from "./auth-routes";
import { createDevRoutes } from "./dev-routes";
import { rootRoute } from "./root";
import { indexRoute } from "./start-route";

/**
 * Code-based routing: every route is a `createRoute` in src/app/router/ and is
 * listed here; a route only points at a page's public API. No generated route tree.
 */
const routeTree = rootRoute.addChildren([
  indexRoute,
  loginRoute,
  registerRoute,
  appRoutes,
  ...(import.meta.env.DEV ? createDevRoutes() : []),
]);

export function createAppRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
    ...(history ? { history } : {}),
  });
}

export type AppRouter = ReturnType<typeof createAppRouter>;

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
