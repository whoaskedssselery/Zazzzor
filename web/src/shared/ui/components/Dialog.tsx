import { X } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "../cn";

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export const overlayClass = "fixed inset-0 z-(--z-dialog) animate-overlay bg-scrim";
export const dialogPanelClass =
  "fixed top-1/2 left-1/2 z-(--z-dialog) flex max-h-[calc(100dvh-2rem)] w-[calc(100vw-2rem)] -translate-x-1/2 -translate-y-1/2 " +
  "animate-dialog flex-col overflow-hidden rounded-lg bg-overlay text-fg shadow-dialog focus:outline-none";

interface DialogContentProps extends Omit<ComponentProps<typeof DialogPrimitive.Content>, "title"> {
  title: ReactNode;
  description?: ReactNode;
  /** Buttons row at the bottom, on a slightly darker band (Discord's modal footer). */
  footer?: ReactNode;
  size?: "sm" | "md" | "lg";
}

const widths = { sm: "max-w-110", md: "max-w-135", lg: "max-w-180" };

/**
 * A modal with a title, an optional description and a close button. Radix
 * traps focus, closes on Escape and outside click, and returns focus to the
 * trigger.
 */
export const DialogContent = ({
  title,
  description,
  footer,
  size = "md",
  className,
  children,
  ...props
}: DialogContentProps) => (
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay className={overlayClass} />
    <DialogPrimitive.Content
      className={cn(dialogPanelClass, widths[size], className)}
      {...(description ? {} : { "aria-describedby": undefined })}
      {...props}
    >
      <div className="flex items-start gap-4 px-4 pt-4 pb-3">
        <div className="min-w-0 flex-1">
          <DialogPrimitive.Title className="text-lg font-semibold">{title}</DialogPrimitive.Title>
          {description && (
            <DialogPrimitive.Description className="mt-1 text-sm text-fg-muted">
              {description}
            </DialogPrimitive.Description>
          )}
        </div>
        <DialogPrimitive.Close
          aria-label="Закрыть"
          className="-mt-1 -mr-1 inline-flex size-8 cursor-pointer items-center justify-center rounded-md text-fg-muted transition-colors hover:bg-hover hover:text-fg"
        >
          <X aria-hidden className="size-5" strokeWidth={1.75} />
        </DialogPrimitive.Close>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4">{children}</div>
      {footer && <DialogFooter>{footer}</DialogFooter>}
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
);

export const DialogFooter = ({ className, ...props }: ComponentProps<"div">) => (
  <div className={cn("flex justify-end gap-2 bg-panel px-4 py-3", className)} {...props} />
);
