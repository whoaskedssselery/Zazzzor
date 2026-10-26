import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";

export interface RouterContext {
  queryClient: QueryClient;
}

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

function RootLayout() {
  return (
    // Theme placeholder: the tokens and the theme switch come with the design system (Task 2).
    <div className="min-h-dvh bg-neutral-900 text-neutral-100">
      <Outlet />
    </div>
  );
}

/** Last-resort screen for an error no page handled. No technical details for users. */
function RootError() {
  return (
    <main role="alert" className="grid min-h-dvh place-items-center p-6 text-center">
      <div>
        <h1 className="text-2xl font-semibold">Что-то сломалось</h1>
        <p className="mt-2 text-neutral-300">Перезагрузите страницу. Если не поможет, попробуйте позже.</p>
        <button
          type="button"
          className="mt-4 rounded bg-indigo-600 px-4 py-2 font-medium text-white hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white"
          onClick={() => {
            window.location.reload();
          }}
        >
          Перезагрузить
        </button>
      </div>
    </main>
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
