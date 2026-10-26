import { createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";

/** Placeholder start page until sign-in and the app shell exist (Task 3). */
export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: IndexPage,
});

function IndexPage() {
  return (
    <main className="grid min-h-dvh place-items-center p-6 text-center">
      <div>
        <h1 className="text-xl font-bold">Zazzzor</h1>
        <p className="mt-2 text-fg-muted">Веб-клиент в разработке.</p>
      </div>
    </main>
  );
}
