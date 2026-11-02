import { AlertDialog } from "radix-ui";
import type { ReactNode } from "react";
import { cn } from "../cn";
import { Button } from "./Button";
import { dialogPanelClass, overlayClass } from "./Dialog";

interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  /** danger for destructive actions (delete, kick, ban). */
  tone?: "primary" | "danger";
  /** Called from the confirm button only. Close the dialog yourself when the action is done. */
  onConfirm: () => void;
  /** The action is running: the confirm button spins and both buttons are locked. */
  pending?: boolean;
}

/**
 * Asks before an irreversible action (role="alertdialog"). Cancel, Escape and
 * the outside click close it without confirming; the cancel button has the
 * initial focus, so an Enter on open never confirms. While `pending`, it
 * cannot be closed and repeat confirms are ignored.
 */
export const ConfirmDialog = ({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  cancelLabel = "Отмена",
  tone = "primary",
  onConfirm,
  pending = false,
}: ConfirmDialogProps) => (
  <AlertDialog.Root
    open={open}
    onOpenChange={(next) => {
      // While the action runs the dialog stays put: closing it would hide the result.
      if (!next && pending) return;
      onOpenChange(next);
    }}
  >
    <AlertDialog.Portal>
      <AlertDialog.Overlay className={overlayClass} />
      <AlertDialog.Content
        className={cn(dialogPanelClass, "max-w-110")}
        onEscapeKeyDown={(e) => {
          if (pending) e.preventDefault();
        }}
        {...(description ? {} : { "aria-describedby": undefined })}
      >
        <div className="px-4 pt-4 pb-5">
          <AlertDialog.Title className="text-lg font-semibold">{title}</AlertDialog.Title>
          {description && (
            <AlertDialog.Description className="mt-2 text-base text-fg-muted">{description}</AlertDialog.Description>
          )}
        </div>
        <div className="flex justify-end gap-2 bg-panel px-4 py-3">
          <AlertDialog.Cancel asChild>
            <Button variant="ghost" disabled={pending}>
              {cancelLabel}
            </Button>
          </AlertDialog.Cancel>
          {/* Not AlertDialog.Action: that closes the dialog before the action has run. */}
          <Button
            variant={tone}
            loading={pending}
            onClick={() => {
              if (!pending) onConfirm();
            }}
          >
            {confirmLabel}
          </Button>
        </div>
      </AlertDialog.Content>
    </AlertDialog.Portal>
  </AlertDialog.Root>
);
