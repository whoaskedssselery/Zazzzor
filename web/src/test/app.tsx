import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse } from "msw";
import { createQueryClient } from "../api/query-client";
import type { components } from "../api/schema.gen";
import { AppProviders } from "../design/providers";
import { initSession } from "../features/auth/session";
import { createAppRouter, type AppRouter } from "../routes/router";
import { apiHandler, problem } from "./msw";

export type User = components["schemas"]["User"];

/** The signed-in account of the app tests. */
export const testUser: User = {
  id: "1234567890123456789",
  username: "vasya",
  display_name: "Вася",
  avatar_url: null,
  created_at: "2026-10-21T12:00:00.000Z",
};

/** GET /users/@me answers as for a signed-in `user`, or 401 when it is null. */
export function sessionHandler(user: User | null) {
  return apiHandler("get", "/users/@me", () => (user ? HttpResponse.json(user) : problem(401, "unauthenticated")));
}

/**
 * Renders the whole app (router, providers, session wiring) at `path`, as
 * main.tsx does, with an in-memory history. The session wiring is undone when
 * the test ends.
 */
export function renderApp(path: string) {
  const queryClient = createQueryClient();
  const router = createAppRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  onTestFinished(initSession(router, queryClient));
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={queryClient}>
      <AppProviders>
        <RouterProvider router={router} />
      </AppProviders>
    </QueryClientProvider>,
  );
  return { user, router, queryClient };
}

/** Pathname plus search of the router's current location, e.g. "/login?next=%2Finvite%2Fabc". */
export function currentHref(router: AppRouter): string {
  return router.state.location.href;
}
