import { QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { type RenderOptions, render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";
import { createQueryClient } from "@/shared/api";
import { UiProviders } from "@/shared/ui";

/**
 * Renders `ui` inside the app-wide providers (theme, toasts, tooltips), as
 * main.tsx does, and returns a user-event instance bound to the document.
 */
export const renderUi = (ui: ReactElement, options?: Omit<RenderOptions, "wrapper">) => {
  const user = userEvent.setup();
  return { user, ...render(ui, { wrapper: UiProviders, ...options }) };
};

/**
 * Renders `ui` as a slice sees it in the app: inside the providers, a query
 * cache and a router whose history starts at `path`. The router has no
 * screens of its own: `ui` stays on screen at every path, so a test can
 * click its links and read where they led (`router.state.location`). Await it.
 */
export const renderRouted = async (ui: ReactElement, { path = "/" }: { path?: string } = {}) => {
  const queryClient = createQueryClient();
  const rootRoute = createRootRoute({ component: () => ui });
  const anyPath = createRoute({ getParentRoute: () => rootRoute, path: "$", component: () => null });
  const router = createRouter({
    routeTree: rootRoute.addChildren([anyPath]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  // Resolved before the first render, so the screen is there when this returns.
  await router.load();
  const user = userEvent.setup();
  const result = render(
    <QueryClientProvider client={queryClient}>
      <UiProviders>
        <RouterProvider router={router} />
      </UiProviders>
    </QueryClientProvider>,
  );
  return { user, router, queryClient, ...result };
};
