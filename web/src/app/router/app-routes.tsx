import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { ChannelPage } from "@/pages/channel";
import { ChannelsHomePage } from "@/pages/channels-home";
import { AuthGuard, requireSession } from "./auth-guard";
import { rootRoute } from "./root";

/**
 * Everything behind sign-in. The pathless layout checks the session before
 * any child loads. Home and the channels are in the main bundle, so the
 * screen people live in opens at once; the invite page and the settings and
 * admin screens load on first use (and are preloaded on hover of a link).
 */
export const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: requireSession,
  component: AuthGuard,
});

export const homeRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/channels/@me",
  component: ChannelsHomePage,
});

/** A server without a channel: the page opens its last or first text channel. */
export const guildRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/channels/$guildId",
  component: () => <ChannelPage guildId={guildRoute.useParams().guildId} />,
});

export const channelRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/channels/$guildId/$channelId",
  component: () => {
    const { guildId, channelId } = channelRoute.useParams();
    return <ChannelPage guildId={guildId} channelId={channelId} />;
  },
});

const InvitePage = lazyRouteComponent(() => import("@/pages/invite"), "InvitePage");

export const inviteRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/invite/$code",
  // The page is wrapped to pass the code, so it is loaded here rather than on render.
  loader: async () => {
    await InvitePage.preload?.();
  },
  component: () => <InvitePage code={inviteRoute.useParams().code} />,
});

export const settingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/$",
  component: lazyRouteComponent(() => import("@/pages/settings"), "SettingsPage"),
});

export const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/admin/$",
  component: lazyRouteComponent(() => import("@/pages/admin"), "AdminPage"),
});

export const appRoutes = appRoute.addChildren([
  homeRoute,
  guildRoute,
  channelRoute,
  inviteRoute,
  settingsRoute,
  adminRoute,
]);
