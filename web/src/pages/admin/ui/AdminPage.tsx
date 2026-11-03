import { useQuery } from "@tanstack/react-query";
import { Link, useCanGoBack, useNavigate, useRouter } from "@tanstack/react-router";
import { type ReactNode, useCallback, useEffect } from "react";
import { adminSettingsQuery, type InstanceSettings as Settings } from "@/entities/session";
import { UserLookup } from "@/features/admin-users";
import { InstanceSettings } from "@/features/instance-settings";
import { errorText } from "@/shared/api";
import { FormAlert, SettingsFrame, Skeleton, settingsNavItemClass } from "@/shared/ui";

const TABS = ["instance", "users"] as const;
type AdminTab = (typeof TABS)[number];

const TITLES: Record<AdminTab, string> = {
  instance: "Сервер Zazzzor",
  users: "Пользователи",
};

const isTab = (tab: string): tab is AdminTab => (TABS as readonly string[]).includes(tab);

const TabContent = ({ tab, settings }: { tab: AdminTab; settings: Settings }): ReactNode => {
  switch (tab) {
    case "instance":
      return <InstanceSettings settings={settings} />;
    case "users":
      return <UserLookup />;
  }
};

/**
 * /admin/$tab: the instance administration, over the whole window like the
 * user settings it is opened from. Only administrators get it; anyone else
 * is sent home. Leaving goes back to where it was opened from.
 */
export const AdminPage = ({ tab }: { tab: string }) => {
  const settings = useQuery(adminSettingsQuery);
  const navigate = useNavigate();
  const router = useRouter();
  const canGoBack = useCanGoBack();
  const current = isTab(tab) ? tab : undefined;

  const close = useCallback(() => {
    if (canGoBack) router.history.back();
    else void navigate({ to: "/channels/@me" });
  }, [canGoBack, router, navigate]);

  const denied = settings.data === null;
  useEffect(() => {
    if (denied) void navigate({ to: "/channels/@me", replace: true });
    else if (current === undefined) void navigate({ to: "/admin/$", params: { _splat: TABS[0] }, replace: true });
  }, [denied, current, navigate]);

  let content: ReactNode = null;
  if (settings.isError) content = <FormAlert>{errorText(settings.error)}</FormAlert>;
  else if (settings.isPending) content = <Skeleton className="h-24 w-full rounded-lg" />;
  else if (settings.data && current) content = <TabContent tab={current} settings={settings.data} />;

  return (
    <SettingsFrame
      navLabel="Администрирование"
      title="Администрирование"
      onClose={close}
      nav={TABS.map((t) => (
        <Link
          key={t}
          to="/admin/$"
          params={{ _splat: t }}
          replace
          aria-current={t === current ? "page" : undefined}
          className={settingsNavItemClass(t === current)}
        >
          {TITLES[t]}
        </Link>
      ))}
    >
      {content}
    </SettingsFrame>
  );
};
