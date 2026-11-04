import { Dialog as DialogPrimitive, VisuallyHidden } from "radix-ui";
import { type ReactNode, useRef } from "react";
import { cn } from "../cn";
import { overlayClass } from "./Dialog";

export const Sheet = DialogPrimitive.Root;

interface SheetContentProps {
  /** The edge the panel slides in from. */
  side: "left" | "right";
  /** Spoken name of the panel («Серверы и каналы», «Участники»); not shown. */
  title: string;
  children: ReactNode;
  className?: string;
  /**
   * The element to focus when the panel opens, picked from the panel (e.g.
   * the open server); Radix's default, the first focusable, when it finds none.
   */
  initialFocus?: (panel: HTMLElement) => HTMLElement | null;
}

/**
 * A panel over the screen from one edge, for what has no room on a narrow
 * window (the server and channel lists, the member list). A Radix dialog:
 * focus stays inside, Escape and a tap on the scrim close it, and focus goes
 * back to what opened it.
 */
export const SheetContent = ({ side, title, children, className, initialFocus }: SheetContentProps) => {
  const panel = useRef<HTMLDivElement>(null);
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className={overlayClass} />
      <DialogPrimitive.Content
        ref={panel}
        aria-describedby={undefined}
        onOpenAutoFocus={(e) => {
          const target = panel.current && initialFocus ? initialFocus(panel.current) : null;
          if (!target) return;
          e.preventDefault();
          target.focus();
        }}
        className={cn(
          "fixed inset-y-0 z-(--z-dialog) flex max-w-[calc(100vw-3rem)] overflow-hidden shadow-dialog focus:outline-none",
          side === "left" ? "animate-sheet-left left-0" : "animate-sheet-right right-0",
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
