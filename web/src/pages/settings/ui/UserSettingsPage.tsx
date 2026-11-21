import { Link, useCanGoBack, useNavigate, useRouter } from "@tanstack/react-router";
import { type ReactNode, useCallback, useEffect, useState } from "react";
import { type User, useIsAdmin, useSession } from "@/entities/session";
import { LogoutItem } from "@/features/auth";
import { PasswordTab } from "@/features/change-password";
import { LanguageTab } from "@/features/choose-language";
import { AppearanceTab } from "@/features/choose-theme";
import { ProfileTab } from "@/features/edit-profile";
import { NotificationsTab } from "@/features/notify";
import { VoiceTab } from "@/features/voice-call";
import { TRANSLATED, useTranslation } from "@/shared/i18n";
import { cn, SettingsFrame, settingsNavItemClass, useLeaveGuard } from "@/shared/ui";

const TABS = ["profile", "account", "appearance", "language", "notifications", "voice"] as const;
type UserSettingsTab = (typeof TABS)[number];
/** The sections in the list: the language one only once the interface is translated (its page opens by address before). */
const LISTED = TABS.filter((t) => t !== "language" || TRANSLATED);

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
    case "appearance":
      return <AppearanceTab />;
    case "language":
      return <LanguageTab />;
    case "notifications":
      return <NotificationsTab />;
    case "voice":
      return <VoiceTab />;
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
  const isAdmin = useIsAdmin();
  const { t: tr } = useTranslation("settings");
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
        navLabel={tr("nav")}
        title={tr("title")}
        onClose={() => {
          guard(close);
        }}
        nav={
          <>
            {LISTED.map((t) => (
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
                {tr(`tabs.${t}`)}
              </Link>
            ))}
            <span aria-hidden className="mx-2.5 my-2 h-px bg-border max-md:hidden" />
            {isAdmin && (
              <Link
                to="/admin/$"
                params={{ _splat: "instance" }}
                replace
                className={settingsNavItemClass(false)}
                onClick={(e) => {
                  if (!dirty) return;
                  e.preventDefault();
                  guard(() => {
                    void navigate({ to: "/admin/$", params: { _splat: "instance" }, replace: true });
                  });
                }}
              >
                {tr("admin")}
              </Link>
            )}
            <LogoutItem
              className={cn(
                settingsNavItemClass(false),
                "flex items-center justify-between text-left text-fg-danger hover:text-fg-danger",
              )}
            />
          </>
        }
      >
        {user && current ? <TabContent tab={current} user={user} onDirtyChange={setDirty} /> : null}
      </SettingsFrame>
      {dialog}
    </>
  );
};
