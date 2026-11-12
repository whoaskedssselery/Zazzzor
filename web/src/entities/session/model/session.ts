import { type QueryClient, queryOptions, useQuery } from "@tanstack/react-query";
import type { RegisteredRouter } from "@tanstack/react-router";
import type { Middleware } from "openapi-fetch";
import { ApiError, api, type components, unwrap } from "@/shared/api";

/** The signed-in user's own account, with the fields only they see. */
export type User = components["schemas"]["CurrentUser"];

/** Where a signed-in user lands when there is no (valid) `next`. */
export const HOME_PATH = "/channels/@me";

/** Query key of the signed-in user (GET /users/@me); null means no session. */
export const ME_KEY = ["me"] as const;

/**
 * The account of the session. A 401 is not an error here: it means nobody is
 * signed in, so the query resolves to null. The session cookie is HttpOnly,
 * so this request is the only way to know; its result stays fresh until
 * sign-in, sign-out or a 401 elsewhere replaces it.
 */
export const sessionQuery = queryOptions({
  queryKey: ME_KEY,
  queryFn: async (): Promise<User | null> => {
    try {
      return unwrap(await api.GET("/users/@me"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return null;
      throw e;
    }
  },
  staleTime: Infinity,
});

/**
 * The user of the session for route guards: the cached answer, or one
 * GET /users/@me when nothing is cached yet.
 */
export function loadSession(queryClient: QueryClient): Promise<User | null> {
  return queryClient.query({ ...sessionQuery, staleTime: "static" });
}

export interface Session {
  user: User | null;
  status: "loading" | "authenticated" | "anonymous";
}

/** The signed-in user from the ["me"] query. A failed check reads as anonymous. */
export function useSession(): Session {
  const { data, isPending } = useQuery(sessionQuery);
  if (data) return { user: data, status: "authenticated" };
  return { user: null, status: isPending ? "loading" : "anonymous" };
}

/** Paths that are the sign-in flow itself: never a `next`, never a reason to redirect to sign-in. */
const AUTH_PATHS = new Set(["/login", "/register"]);

/**
 * The `next` of /login and /register as a path to go to after signing in:
 * only a same-origin relative path (one leading "/", no "//" or "/\", no
 * scheme, nothing the URL parser puts on another origin, not the sign-in
 * pages themselves). Anything else becomes HOME_PATH. The path is returned
 * as the URL parser normalised it, and the checks run again on that result:
 * dot segments ("/.//evil.com", "/%2e//evil.com", "/a/..//evil.com") and the
 * tabs and newlines the parser drops can turn a harmless input into "//…".
 */
/** "//host" and "/\host" mean another host to a browser. */
function isProtocolRelative(path: string): boolean {
  return path.startsWith("//") || path.startsWith("/\\");
}

export function safeNext(next: unknown): string {
  if (typeof next !== "string" || !next.startsWith("/") || isProtocolRelative(next)) {
    return HOME_PATH;
  }
  const origin = window.location.origin;
  let url: URL;
  try {
    url = new URL(next, origin);
  } catch {
    return HOME_PATH;
  }
  const path = url.pathname + url.search + url.hash;
  if (url.origin !== origin || AUTH_PATHS.has(url.pathname) || isProtocolRelative(path)) return HOME_PATH;
  return path;
}

interface Binding {
  router: RegisteredRouter;
  queryClient: QueryClient;
}

let binding: Binding | null = null;

/**
 * 401s the global handler leaves alone: a wrong password on the sign-in form
 * is the form's own error, and GET /users/@me (the session check) answers
 * 401 for anyone not signed in, which the session query and AuthGuard
 * handle. Keyed by method and path: PATCH /users/@me is an ordinary request.
 */
const OWN_401 = new Set(["POST /auth/login", "GET /users/@me"]);

const unauthorizedMiddleware: Middleware = {
  onResponse({ request, response, schemaPath }) {
    if (response.status === 401 && !OWN_401.has(`${request.method} ${schemaPath}`)) onUnauthorized();
  },
};

/**
 * Connects the session to the app's router and query cache, and installs the
 * 401 handler on the API client. main.tsx calls it once; the returned
 * function undoes it (tests).
 */
export function initSession(router: RegisteredRouter, queryClient: QueryClient): () => void {
  binding = { router, queryClient };
  api.use(unauthorizedMiddleware);
  return () => {
    api.eject(unauthorizedMiddleware);
    if (binding?.router === router) binding = null;
  };
}

/** Remembers the user a successful sign-in or registration answered with. */
export function setSignedIn(queryClient: QueryClient, user: User): void {
  queryClient.setQueryData(ME_KEY, user);
}

/**
 * Takes a newer copy of the signed-in user (a profile save, an event) only
 * while that same user is still signed in: an answer that arrives after a
 * sign-out, or after signing in as someone else, is dropped.
 */
export function updateSignedIn(queryClient: QueryClient, user: User): void {
  const now = queryClient.getQueryData<User | null>(ME_KEY);
  if (now?.id === user.id) queryClient.setQueryData(ME_KEY, user);
}

const sessionEndListeners = new Set<() => void>();

/**
 * Calls `fn` whenever the session ends (sign-out, or a 401 from any request
 * or the gateway), right after the query cache is cleared: the app stops the
 * gateway and empties its client stores there. Returns the unsubscribe.
 */
export function onSessionEnd(fn: () => void): () => void {
  sessionEndListeners.add(fn);
  return () => {
    sessionEndListeners.delete(fn);
  };
}

/**
 * Forgets everything of the session: every cached query goes and the user
 * reads as anonymous at once (no new GET /users/@me); then the listeners of
 * onSessionEnd reset the client stores.
 */
function resetSession(queryClient: QueryClient): void {
  queryClient.clear();
  queryClient.setQueryData(ME_KEY, null);
  for (const fn of [...sessionEndListeners]) fn();
}

/**
 * The session ended under us (a 401 from any request): drop its data and go
 * to sign-in, which brings the user back to the current page afterwards.
 * Does nothing on the sign-in pages themselves.
 */
export function onUnauthorized(): void {
  if (!binding) return;
  const { router, queryClient } = binding;
  const here = router.history.location;
  resetSession(queryClient);
  if (AUTH_PATHS.has(here.pathname)) return;
  void router.navigate({ to: "/login", search: { next: here.href }, replace: true });
}

/**
 * Signs out: POST /auth/logout, then the same cleanup as a 401 and the
 * sign-in screen. If the request fails the session is kept (the cookie would
 * still be valid) and the ApiError is thrown for the caller to show.
 */
export async function logout(): Promise<void> {
  if (!binding) throw new Error("initSession was not called");
  const { router, queryClient } = binding;
  unwrap(await api.POST("/auth/logout"));
  resetSession(queryClient);
  await router.navigate({ to: "/login", replace: true });
}
