import { useMutation } from "@tanstack/react-query";
import { createRoute } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { errorText } from "../api/client";
import { Button } from "../design/components/Button";
import { useToast } from "../design/components/Toast";
import { AuthGuard, requireSession } from "../features/auth/AuthGuard";
import { logout, useSession } from "../features/auth/session";
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
  component: function HomeRoute() {
    const { user } = useSession();
    return (
      <Placeholder title="Личные сообщения">
        Вы вошли как {user?.display_name}. Серверы и каналы появятся здесь совсем скоро.
      </Placeholder>
    );
  },
});

export const channelRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/channels/$guildId/$channelId",
  component: function ChannelRoute() {
    const { channelId } = channelRoute.useParams();
    return <Placeholder title="Канал">Канал {channelId} откроется здесь в следующем обновлении.</Placeholder>;
  },
});

export const inviteRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/invite/$code",
  component: function InviteRoute() {
    const { code } = inviteRoute.useParams();
    return (
      <Placeholder title="Приглашение">
        Код приглашения: <code>{code}</code>. Принять его можно будет в следующем обновлении.
      </Placeholder>
    );
  },
});

export const settingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/$",
  component: () => <Placeholder title="Настройки">Профиль и пароль появятся здесь в следующем обновлении.</Placeholder>,
});

export const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/admin/$",
  component: () => <Placeholder title="Администрирование">Настройки сервера Zazzzor появятся здесь в следующем обновлении.</Placeholder>,
});

export const appRoutes = appRoute.addChildren([homeRoute, channelRoute, inviteRoute, settingsRoute, adminRoute]);

/** A protected screen that is not built yet: its name, a line about it and the way out. */
function Placeholder({ title, children }: { title: string; children: ReactNode }) {
  const toast = useToast();
  const signOut = useMutation({
    mutationFn: logout,
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
  return (
    <main className="grid min-h-dvh place-items-center p-6 text-center">
      <div className="max-w-md">
        <h1 className="text-xl font-semibold">{title}</h1>
        <p className="mt-2 text-fg-muted">{children}</p>
        <Button
          variant="secondary"
          className="mt-6"
          loading={signOut.isPending}
          onClick={() => {
            signOut.mutate();
          }}
        >
          Выйти
        </Button>
      </div>
    </main>
  );
}
