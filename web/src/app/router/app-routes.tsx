import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { ChannelPage } from "@/pages/channel";
import { ChannelsHomePage } from "@/pages/channels-home";
import { DirectMessagePage } from "@/pages/direct-message";
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

/** A DM or a group: `@me` is a word, so this wins over a server's channel address. */
export const privateRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/channels/@me/$channelId",
  component: () => <DirectMessagePage channelId={privateRoute.useParams().channelId} />,
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

const UserSettingsPage = lazyRouteComponent(() => import("@/pages/settings"), "UserSettingsPage");

/** The user's own settings, over the whole window; the splat is the section. */
export const settingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/$",
  loader: async () => {
    await UserSettingsPage.preload?.();
  },
  component: () => <UserSettingsPage tab={settingsRoute.useParams()._splat ?? ""} />,
});

const AdminPage = lazyRouteComponent(() => import("@/pages/admin"), "AdminPage");

/** The instance administration, for its administrators; the splat is the section. */
export const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/admin/$",
  loader: async () => {
    await AdminPage.preload?.();
  },
  component: () => <AdminPage tab={adminRoute.useParams()._splat ?? ""} />,
});

export const appRoutes = appRoute.addChildren([
  shellRoute.addChildren([homeRoute, privateRoute, guildRoute, channelRoute]),
  inviteRoute,
  guildSettingsRoute,
  channelSettingsRoute,
  settingsRoute,
  adminRoute,
]);
