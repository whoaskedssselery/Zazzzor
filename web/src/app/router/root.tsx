import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import { t } from "@/shared/i18n";
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
      <h1 className="text-xl font-semibold">{t("common:crash.title")}</h1>
      <p className="mt-2 text-fg-muted">{t("common:crash.text")}</p>
      <Button
        className="mt-4"
        onClick={() => {
          window.location.reload();
        }}
      >
        {t("common:crash.reload")}
      </Button>
    </div>
  </main>
);

const NotFound = () => (
  <main className="grid min-h-dvh place-items-center p-6 text-center">
    <div>
      <h1 className="text-xl font-semibold">{t("common:notFound.title")}</h1>
      <p className="mt-2 text-fg-muted">{t("common:notFound.text")}</p>
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
