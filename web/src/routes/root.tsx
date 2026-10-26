import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";

export interface RouterContext {
  queryClient: QueryClient;
}

/**
 * The root of the route tree (code-based routing, see router.ts): app-wide
 * providers around every page.
 */
export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
  notFoundComponent: NotFound,
});

function RootLayout() {
  const { queryClient } = rootRoute.useRouteContext();
  return (
    <QueryClientProvider client={queryClient}>
      {/* Theme placeholder: the tokens and the theme switch come with the design system (Task 2). */}
      <div className="min-h-dvh bg-neutral-900 text-neutral-100">
        <Outlet />
      </div>
    </QueryClientProvider>
  );
}

function NotFound() {
  return (
    <main className="grid min-h-dvh place-items-center p-6 text-center">
      <div>
        <h1 className="text-2xl font-semibold">Страница не найдена</h1>
        <p className="mt-2 text-neutral-300">Такого адреса в Zazzzor нет.</p>
      </div>
    </main>
  );
}
