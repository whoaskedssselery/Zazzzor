import { X } from "lucide-react";
import { type ReactNode, useEffect, useEffectEvent, useState } from "react";
import { cn } from "../cn";
import { ConfirmDialog } from "./ConfirmDialog";
import { IconButton } from "./IconButton";
import { Skeleton } from "./Skeleton";

export const LEAVE_UNSAVED = "Уйти без сохранения?";

/** The look of one section in a settings frame's list. */
export const settingsNavItemClass = (active: boolean): string =>
  cn(
    "shrink-0 rounded-md px-2.5 py-1.5 text-base text-fg-muted transition-colors hover:bg-hover hover:text-fg",
    active && "bg-selected text-fg",
  );

/**
 * Asks before unsaved changes are left: `guard(go)` runs `go` at once while
 * nothing is unsaved, else after «Уйти» in the returned `dialog`.
 */
export const useLeaveGuard = (dirty: boolean, onLeave: () => void) => {
  const [pending, setPending] = useState<(() => void) | null>(null);
  const guard = (go: () => void) => {
    if (dirty) setPending(() => go);
    else go();
  };
  const dialog = (
    <ConfirmDialog
      open={pending !== null}
      onOpenChange={(open) => {
        if (!open) setPending(null);
      }}
      title={LEAVE_UNSAVED}
      description="Несохранённые изменения пропадут."
      confirmLabel="Уйти"
      tone="danger"
      onConfirm={() => {
        const go = pending;
        setPending(null);
        onLeave();
        go?.();
      }}
    />
  );
  return { guard, dialog };
};

/**
 * A settings screen over the whole window, as in Discord: the sections on
 * the left under `title` (a skeleton while null), the open one on the
 * right, and a way out — the button, or Esc unless a dialog is open over
 * it. `onClose` decides what leaving means (and asks first if it must).
 */
export const SettingsFrame = ({
  navLabel,
  title,
  nav,
  onClose,
  children,
}: {
  navLabel: string;
  title: string | null;
  nav: ReactNode;
  onClose: () => void;
  children: ReactNode;
}) => {
  const onKey = useEffectEvent((e: KeyboardEvent) => {
    // A dialog over the settings closes itself first.
    if (e.key !== "Escape" || e.defaultPrevented || document.querySelector("[role=dialog],[role=alertdialog]")) return;
    onClose();
  });
  useEffect(() => {
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, []);

  return (
    <div className="flex h-full min-h-0 bg-chat max-md:flex-col">
      <nav
        aria-label={navLabel}
        className="flex shrink-0 justify-end bg-sidebar md:w-[max(14rem,calc((100%-46rem)/2))] max-md:overflow-x-auto"
      >
        <div className="flex w-full flex-col gap-0.5 px-4 py-6 md:w-56 md:py-14 max-md:flex-row max-md:items-center">
          {title === null ? (
            <Skeleton className="mx-2.5 mb-2 h-3 w-24 rounded-sm" />
          ) : (
            <h2 className="truncate px-2.5 pb-1.5 text-xs font-semibold text-fg-faint uppercase max-md:hidden">
              {title}
            </h2>
          )}
          {nav}
        </div>
      </nav>
      <main className="min-h-0 min-w-0 flex-1 overflow-y-auto">
        <div className="flex max-w-[46rem] gap-6 px-10 py-14 max-md:px-4 max-md:py-6">
          <div className="min-w-0 flex-1">{children}</div>
          <div className="flex shrink-0 flex-col items-center gap-1 max-md:fixed max-md:top-2 max-md:right-2">
            <IconButton
              label="Закрыть настройки"
              icon={X}
              className="size-9 rounded-full border-2 border-border-strong text-fg-muted"
              onClick={onClose}
            />
            <span aria-hidden className="text-xs font-semibold text-fg-faint max-md:hidden">
              ESC
            </span>
          </div>
        </div>
      </main>
    </div>
  );
};
