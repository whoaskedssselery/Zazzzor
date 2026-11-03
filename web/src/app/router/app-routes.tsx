import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { ChannelPage } from "@/pages/channel";
import { ChannelsHomePage } from "@/pages/channels-home";
import { AuthGuard, requireSession } from "./auth-guard";
import { rootRoute } from "./root";
import { ShellLayout } from "./shell-layout";

/**
 * Everything behind sign-in. The pathless layout checks the session before
 * any child loads. Home and the channels are in the main bundle, so the
 * screen people live in opens at once; the invite page and the settings and
 * admin screens load on first use (and are preloaded on hover of a link).
 * Their chunks are small; most of the entry's weight is the libraries.
 */
export const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: requireSession,
  component: AuthGuard,
});

/** The app shell around home and the servers, mounted once for all of them. */
export const shellRoute = createRoute({
  getParentRoute: () => appRoute,
  id: "shell",
  component: ShellLayout,
});

export const homeRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/channels/@me",
  component: ChannelsHomePage,
});

/**
 * A server without a channel (a typed or old address; the rail links to the
 * channel itself): the page opens its last or first text channel.
 */
export const guildRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/channels/$guildId",
  component: () => <ChannelPage guildId={guildRoute.useParams().guildId} />,
});

export const channelRoute = createRoute({
  getParentRoute: () => shellRoute,
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

const GuildSettingsPage = lazyRouteComponent(() => import("@/pages/guild-settings"), "GuildSettingsPage");

/** A server's settings, over the whole window (outside the shell, like the user settings). */
export const guildSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/guilds/$guildId/settings/$tab",
  loader: async () => {
    await GuildSettingsPage.preload?.();
  },
  component: () => {
    const { guildId, tab } = guildSettingsRoute.useParams();
    return <GuildSettingsPage guildId={guildId} tab={tab} />;
  },
});

const ChannelSettingsPage = lazyRouteComponent(() => import("@/pages/channel-settings"), "ChannelSettingsPage");

/** A channel's settings, over the whole window like the server's. */
export const channelSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/guilds/$guildId/channels/$channelId/settings/$tab",
  loader: async () => {
    await ChannelSettingsPage.preload?.();
  },
  component: () => {
    const { guildId, channelId, tab } = channelSettingsRoute.useParams();
    return <ChannelSettingsPage guildId={guildId} channelId={channelId} tab={tab} />;
  },
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
  shellRoute.addChildren([homeRoute, guildRoute, channelRoute]),
  inviteRoute,
  guildSettingsRoute,
  channelSettingsRoute,
  settingsRoute,
  adminRoute,
]);
