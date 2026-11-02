import { createRoute } from "@tanstack/react-router";
import { LoginPage } from "@/pages/login";
import { RegisterPage } from "@/pages/register";
import { redirectSignedIn } from "./auth-guard";
import { rootRoute } from "./root";

/**
 * `?next=` of the sign-in pages: where to go afterwards. Kept only when it is
 * a string; safeNext checks it before it is ever opened.
 */
const validateAuthSearch = (search: Record<string, unknown>): { next?: string } =>
  typeof search.next === "string" ? { next: search.next } : {};

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: validateAuthSearch,
  beforeLoad: redirectSignedIn,
  component: () => <LoginPage next={loginRoute.useSearch().next} />,
});

export const registerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/register",
  validateSearch: validateAuthSearch,
  beforeLoad: redirectSignedIn,
  component: () => <RegisterPage next={registerRoute.useSearch().next} />,
});
