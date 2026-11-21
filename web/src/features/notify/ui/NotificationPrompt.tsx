import { Bell, X } from "lucide-react";
import { t } from "@/shared/i18n";
import { Button, IconButton } from "@/shared/ui";
import { useNotificationPermission } from "../model/permission";
import { setNotifyPrefs, useNotifyPrefs } from "../model/prefs";

/**
 * A bar at the top offering desktop notifications, as Discord's: while the
 * browser has not been asked yet and the user has neither closed the bar nor
 * turned notifications off. The browser's own question follows the click.
 */
export const NotificationPrompt = () => {
  const prefs = useNotifyPrefs();
  const { permission, request } = useNotificationPermission();
  if (permission !== "default" || !prefs.desktop || prefs.promptDismissed) return null;
  const dismiss = () => {
    setNotifyPrefs({ promptDismissed: true });
  };
  return (
    <section
      aria-label={t("notify:promptLabel")}
      className="flex shrink-0 items-center gap-3 border-b border-border bg-accent px-4 py-1.5 text-sm font-medium text-on-accent"
    >
      <Bell aria-hidden className="size-4 shrink-0 max-sm:hidden" />
      <p className="min-w-0 flex-1">{t("notify:promptText")}</p>
      <Button
        size="sm"
        variant="secondary"
        className="h-7"
        onClick={() => {
          void request().then(dismiss);
        }}
      >
        {t("notify:turnOn")}
      </Button>
      <IconButton
        size="sm"
        label={t("notify:notNow")}
        icon={X}
        className="text-on-accent hover:bg-[color-mix(in_srgb,var(--on-accent)_12%,transparent)] hover:text-on-accent"
        onClick={dismiss}
      />
    </section>
  );
};
