import { useConnectionStatus } from "@/entities/guild";
import { t, textTable } from "@/shared/i18n";
import { cn, Spinner } from "@/shared/ui";

const TEXT = textTable({
  connecting: () => t("common:connecting"),
  reconnecting: () => t("common:reconnecting"),
  ready: () => "",
});

/**
 * A thin bar at the top of the app while the gateway is not ready. It takes
 * its own row above the screen, so it never covers anything, and the
 * composer at the bottom stays where it is. The live region itself stays in
 * the page (empty once ready), so every change is announced.
 */
export const ConnectionBanner = () => {
  const status = useConnectionStatus();
  const text = TEXT[status];
  return (
    <div
      role="status"
      data-connection-banner
      className={cn(
        text &&
          "flex h-6 shrink-0 items-center justify-center gap-2 border-b border-border bg-panel px-4 text-xs font-medium",
        status === "reconnecting" ? "text-fg-warning" : "text-fg-muted",
      )}
    >
      {text && <Spinner size={12} />}
      {text}
    </div>
  );
};
