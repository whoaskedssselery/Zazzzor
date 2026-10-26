import { createRoute } from "@tanstack/react-router";
import { AdminPage } from "@/pages/admin";
import { ChannelPage } from "@/pages/channel";
import { ChannelsHomePage } from "@/pages/channels-home";
import { InvitePage } from "@/pages/invite";
import { SettingsPage } from "@/pages/settings";
import { AuthGuard, requireSession } from "./auth-guard";
import { rootRoute } from "./root";

/**
 * Everything behind sign-in. The pathless layout checks the session before
 * any child loads; the screens are placeholders until the app shell (Task 5)
 * and the settings and admin screens (Task 8) replace them.
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

export const channelRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/channels/$guildId/$channelId",
  component: function ChannelRoute() {
    return <ChannelPage channelId={channelRoute.useParams().channelId} />;
  },
});

export const inviteRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/invite/$code",
  component: function InviteRoute() {
    return <InvitePage code={inviteRoute.useParams().code} />;
  },
});

export const settingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/$",
  component: SettingsPage,
});

export const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/admin/$",
  component: AdminPage,
});

export const appRoutes = appRoute.addChildren([homeRoute, channelRoute, inviteRoute, settingsRoute, adminRoute]);
