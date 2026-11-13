import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse } from "msw";
import { initSession } from "@/entities/session";
import { type components, createQueryClient } from "@/shared/api";
import { apiHandler, problem } from "@/shared/test";
import { AppProviders } from "../providers";
import { type AppRouter, createAppRouter } from "../router";

export type User = components["schemas"]["CurrentUser"];

/** The signed-in account of the app tests. */
export const testUser: User = {
  id: "1234567890123456789",
  username: "vasya",
  display_name: "Вася",
  avatar_url: null,
  created_at: "2026-10-21T12:00:00.000Z",
  bio: "",
  status: "online",
  custom_status: null,
};

/** GET /users/@me answers as for a signed-in `user`, or 401 when it is null. */
export const sessionHandler = (user: User | null) =>
  apiHandler("get", "/users/@me", () => (user ? HttpResponse.json(user) : problem(401, "unauthenticated")));

/**
 * Renders the whole app (router, providers, session wiring) at `path`, as
 * main.tsx does, with an in-memory history. The session wiring is undone when
 * the test ends.
 */
export const renderApp = (path: string) => {
  const queryClient = createQueryClient();
  const router = createAppRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  onTestFinished(initSession(router, queryClient));
  const user = userEvent.setup();
  render(
    <AppProviders queryClient={queryClient}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return { user, router, queryClient };
};

/** Pathname plus search of the router's current location, e.g. "/login?next=%2Finvite%2Fabc". */
export const currentHref = (router: AppRouter): string => router.state.location.href;
