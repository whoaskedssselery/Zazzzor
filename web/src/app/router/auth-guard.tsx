import type { QueryClient } from "@tanstack/react-query";
import { Outlet, type ParsedLocation, redirect } from "@tanstack/react-router";
import { useEffect } from "react";
import { loadSession, onUnauthorized, safeNext, useSession } from "@/entities/session";
import { CallNotices } from "@/features/voice-call";
import { ConnectionBanner } from "@/widgets/connection-banner";
import { useGatewaySession } from "./gateway-session";

/** `beforeLoad` of the protected layout: no session → /login?next=<this page>. */
export const requireSession = async ({
  context,
  location,
}: {
  context: { queryClient: QueryClient };
  location: ParsedLocation;
}) => {
  const user = await loadSession(context.queryClient);
  if (!user) throw redirect({ to: "/login", search: { next: location.href }, replace: true });
  return { user };
};

/**
 * `beforeLoad` of /login and /register: a signed-in user goes on to `next`
 * straight away. When the check itself fails (server down) the form is shown
 * anyway; submitting it then explains the problem.
 */
export const redirectSignedIn = async ({
  context,
  search,
}: {
  context: { queryClient: QueryClient };
  search: { next?: string };
}) => {
  const user = await loadSession(context.queryClient).catch(() => null);
  if (user) throw redirect({ href: safeNext(search.next), replace: true });
};

/**
 * The protected layout. requireSession has already loaded the user before
 * the first render. Later the session check can come back anonymous (a
 * refetch of ["me"] after the session expired): the guard then does what a
 * 401 elsewhere does, onUnauthorized(): the cache is cleared and the router
 * goes to /login?next=<this page>, rendering nothing protected meanwhile.
 *
 * While the session is authenticated the gateway runs (useGatewaySession)
 * and the connection banner sits in its own row above the screen.
 *
 * Sign-out and a 401 from another request clear the cache and navigate
 * themselves. queryClient.clear() does not notify mounted observers, so the
 * page stays as it was until that navigation replaces it; nothing new is
 * shown in between.
 */
export const AuthGuard = () => {
  const { status } = useSession();
  const anonymous = status === "anonymous";
  const authenticated = status === "authenticated";
  useEffect(() => {
    if (anonymous) onUnauthorized();
  }, [anonymous]);
  useGatewaySession(authenticated);
  if (!authenticated) return null;
  return (
    <div className="flex h-dvh flex-col">
      <ConnectionBanner />
      <div className="min-h-0 flex-1 overflow-auto">
        <Outlet />
      </div>
      {/* Call toasts on every screen behind sign-in, the settings included. */}
      <CallNotices />
    </div>
  );
};
