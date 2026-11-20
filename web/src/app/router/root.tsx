import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import { Button } from "@/shared/ui";
import { useBootSplashHandOver } from "./splash";

export interface RouterContext {
  queryClient: QueryClient;
}

const RootLayout = () => {
  useBootSplashHandOver();
  return (
    <div className="min-h-dvh bg-chat text-fg">
      <Outlet />
    </div>
  );
};

/** Last-resort screen for an error no page handled. No technical details for users. */
const RootError = () => (
  <main role="alert" className="grid min-h-dvh place-items-center p-6 text-center">
    <div>
      <h1 className="text-xl font-semibold">Что-то сломалось</h1>
      <p className="mt-2 text-fg-muted">Перезагрузите страницу. Если не поможет, попробуйте позже.</p>
      <Button
        className="mt-4"
        onClick={() => {
          window.location.reload();
        }}
      >
        Перезагрузить
      </Button>
    </div>
  </main>
);

const NotFound = () => (
  <main className="grid min-h-dvh place-items-center p-6 text-center">
    <div>
      <h1 className="text-xl font-semibold">Страница не найдена</h1>
      <p className="mt-2 text-fg-muted">Такого адреса в Zazzzor нет.</p>
    </div>
  </main>
);

/**
 * The root of the route tree (code-based routing, see router.ts). Providers
 * live in main.tsx around the router; loaders get the query client from the
 * route context.
 */
export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
  errorComponent: RootError,
  notFoundComponent: NotFound,
});
