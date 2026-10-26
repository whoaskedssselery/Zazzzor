import type { QueryClient } from "@tanstack/react-query";
import { Outlet, redirect, type ParsedLocation } from "@tanstack/react-router";
import { loadSession, safeNext, useSession } from "./session";

/** `beforeLoad` of the protected layout: no session → /login?next=<this page>. */
export async function requireSession({ context, location }: { context: { queryClient: QueryClient }; location: ParsedLocation }) {
  const user = await loadSession(context.queryClient);
  if (!user) throw redirect({ to: "/login", search: { next: location.href }, replace: true });
  return { user };
}

/**
 * `beforeLoad` of /login and /register: a signed-in user goes on to `next`
 * straight away. When the check itself fails (server down) the form is shown
 * anyway; submitting it then explains the problem.
 */
export async function redirectSignedIn({ context, search }: { context: { queryClient: QueryClient }; search: { next?: string } }) {
  const user = await loadSession(context.queryClient).catch(() => null);
  if (user) throw redirect({ href: safeNext(search.next), replace: true });
}

/**
 * The protected layout. requireSession has already loaded the user; once the
 * session is reset (sign-out, a 401) nothing protected stays on screen while
 * the router moves to /login.
 */
export function AuthGuard() {
  const { status } = useSession();
  return status === "authenticated" ? <Outlet /> : null;
}
