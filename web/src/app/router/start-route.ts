import { createRoute, redirect } from "@tanstack/react-router";
import { loadSession } from "@/entities/session";
import { rootRoute } from "./root";

/** The start page is a fork: home with a session, sign-in without one. */
export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: async ({ context }) => {
    const user = await loadSession(context.queryClient);
    if (user) throw redirect({ to: "/channels/@me", replace: true });
    throw redirect({ to: "/login", replace: true });
  },
});
