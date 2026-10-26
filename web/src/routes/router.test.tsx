import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { createQueryClient } from "../api/query-client";
import { rootRoute } from "./root";
import { createAppRouter } from "./router";

/** Renders the app's router at `path`, wrapped like main.tsx does. */
function renderAt(path: string) {
  const queryClient = createQueryClient();
  const router = createAppRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe("router", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders the start page at /", async () => {
    renderAt("/");
    expect(await screen.findByRole("heading", { name: "Zazzzor" })).toBeTruthy();
  });

  it("renders a Russian not-found page for unknown paths", async () => {
    renderAt("/channels/1/2/nope");
    expect(await screen.findByRole("heading", { name: "Страница не найдена" })).toBeTruthy();
  });

  it("shows the root error screen with a reload button when a page throws", async () => {
    // React reports the caught error on console.error and the router warns
    // «Error in route match» (dev builds); keep the output clean and check
    // that both were reported. afterEach restores the spies.
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const consoleWarn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    // A throwaway tree with the app's root error component: the app's own
    // route tree has no page that throws.
    const root = createRootRoute({ component: Outlet, errorComponent: rootRoute.options.errorComponent });
    const broken = createRoute({
      getParentRoute: () => root,
      path: "/",
      component: () => {
        throw new Error("boom");
      },
    });
    const router = createRouter({
      routeTree: root.addChildren([broken]),
      history: createMemoryHistory({ initialEntries: ["/"] }),
    });

    render(<RouterProvider router={router} />);

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Что-то сломалось");
    expect(alert.textContent).not.toContain("boom");
    const reload = screen.getByRole("button", { name: "Перезагрузить" });
    reload.focus();
    expect(document.activeElement).toBe(reload);
    expect(consoleError).toHaveBeenCalled();
    expect(consoleWarn).toHaveBeenCalledWith("Warning: Error in route match: __root__/");
  });
});
