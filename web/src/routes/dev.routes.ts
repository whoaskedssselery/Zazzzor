import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { rootRoute } from "./root";

/**
 * Routes that exist only in dev builds. router.ts calls this behind
 * `import.meta.env.DEV`, which Vite replaces with `false` in production, so
 * the call, this module and the lazily imported page drop out of the bundle.
 * The module has no top-level side effects for the same reason.
 */
export function createDevRoutes() {
  return [
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/dev/design",
      component: lazyRouteComponent(() => import("./dev.design"), "DesignPage"),
    }),
  ];
}
