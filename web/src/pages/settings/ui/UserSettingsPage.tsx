import { Link, useCanGoBack, useNavigate, useRouter } from "@tanstack/react-router";
import { type ReactNode, useCallback, useEffect, useState } from "react";
import { type User, useSession } from "@/entities/session";
import { PasswordTab } from "@/features/change-password";
import { ProfileTab } from "@/features/edit-profile";
import { SettingsFrame, settingsNavItemClass, useLeaveGuard } from "@/shared/ui";

const TABS = ["profile", "account"] as const;
type UserSettingsTab = (typeof TABS)[number];

export const USER_SETTINGS_TITLES: Record<UserSettingsTab, string> = {
  profile: "Профиль",
  account: "Учётная запись",
};

const isTab = (tab: string): tab is UserSettingsTab => (TABS as readonly string[]).includes(tab);

const TabContent = ({
  tab,
  user,
  onDirtyChange,
}: {
  tab: UserSettingsTab;
  user: User;
  onDirtyChange: (dirty: boolean) => void;
}): ReactNode => {
  switch (tab) {
    case "profile":
      return <ProfileTab user={user} onDirtyChange={onDirtyChange} />;
    case "account":
      return <PasswordTab />;
  }
};

/**
 * /settings/$tab: the user's own settings over the whole window, as in
 * Discord. Switching sections replaces the address, so leaving (the button
 * or Esc) goes back to where the settings were opened from, or home when
 * they were opened directly. Unsaved changes are asked about first; an
 * unknown section opens the first one.
 */
export const UserSettingsPage = ({ tab }: { tab: string }) => {
  const { user } = useSession();
  const navigate = useNavigate();
  const router = useRouter();
  const canGoBack = useCanGoBack();
  const current = isTab(tab) ? tab : undefined;
  const [dirty, setDirty] = useState(false);
  const { guard, dialog } = useLeaveGuard(dirty, () => {
    setDirty(false);
  });

  const close = useCallback(() => {
    if (canGoBack) router.history.back();
    else void navigate({ to: "/channels/@me" });
  }, [canGoBack, router, navigate]);

  useEffect(() => {
    if (current === undefined) void navigate({ to: "/settings/$", params: { _splat: TABS[0] }, replace: true });
  }, [current, navigate]);

  const go = (t: UserSettingsTab) => {
    void navigate({ to: "/settings/$", params: { _splat: t }, replace: true });
  };

  return (
    <>
      <SettingsFrame
        navLabel="Настройки пользователя"
        title="Настройки"
        onClose={() => {
          guard(close);
        }}
        nav={TABS.map((t) => (
          <Link
            key={t}
            to="/settings/$"
            params={{ _splat: t }}
            replace
            aria-current={t === current ? "page" : undefined}
            className={settingsNavItemClass(t === current)}
            onClick={(e) => {
              if (!dirty || t === current) return;
              e.preventDefault();
              guard(() => {
                go(t);
              });
            }}
          >
            {USER_SETTINGS_TITLES[t]}
          </Link>
        ))}
      >
        {user && current ? <TabContent tab={current} user={user} onDirtyChange={setDirty} /> : null}
      </SettingsFrame>
      {dialog}
    </>
  );
};
