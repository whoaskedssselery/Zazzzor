import { Dialog as DialogPrimitive, VisuallyHidden } from "radix-ui";
import { type ReactNode, useRef } from "react";
import { cn } from "../cn";
import { overlayClass } from "./Dialog";

export const Sheet = DialogPrimitive.Root;

interface SheetContentProps {
  /** The edge the panel slides in from; "bottom" is a phone's action sheet. */
  side: "left" | "right" | "bottom";
  /** Spoken name of the panel («Серверы и каналы», «Участники»); not shown. */
  title: string;
  children: ReactNode;
  className?: string;
  /**
   * The element to focus when the panel opens, picked from the panel (e.g.
   * the open server); Radix's default, the first focusable, when it finds none.
   */
  initialFocus?: (panel: HTMLElement) => HTMLElement | null;
  /** When the panel is gone and would give the focus back; preventDefault keeps it where it is. */
  onCloseAutoFocus?: (e: Event) => void;
}

/**
 * A panel over the screen from one edge, for what has no room on a narrow
 * window (the server and channel lists, the member list, a message's
 * actions on a phone). A Radix dialog:
 * focus stays inside, Escape and a tap on the scrim close it, and focus goes
 * back to what opened it.
 */
export const SheetContent = ({
  side,
  title,
  children,
  className,
  initialFocus,
  onCloseAutoFocus,
}: SheetContentProps) => {
  const panel = useRef<HTMLDivElement>(null);
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className={overlayClass} />
      <DialogPrimitive.Content
        ref={panel}
        aria-describedby={undefined}
        {...(onCloseAutoFocus ? { onCloseAutoFocus } : {})}
        onOpenAutoFocus={(e) => {
          const target = panel.current && initialFocus ? initialFocus(panel.current) : null;
          if (!target) return;
          e.preventDefault();
          target.focus();
        }}
        className={cn(
          "fixed z-(--z-dialog) flex overflow-hidden shadow-dialog focus:outline-none",
          side === "bottom"
            ? "animate-sheet-bottom inset-x-0 bottom-0 max-h-[85dvh] rounded-t-xl"
            : "inset-y-0 max-w-[calc(100vw-3rem)]",
          side === "left" && "animate-sheet-left left-0",
          side === "right" && "animate-sheet-right right-0",
          className,
        )}
      >
        <VisuallyHidden.Root asChild>
          <DialogPrimitive.Title>{title}</DialogPrimitive.Title>
        </VisuallyHidden.Root>
        {children}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
};
