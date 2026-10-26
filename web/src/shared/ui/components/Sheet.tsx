import { Dialog as DialogPrimitive, VisuallyHidden } from "radix-ui";
import type { ReactNode } from "react";
import { cn } from "../cn";
import { overlayClass } from "./Dialog";

export const Sheet = DialogPrimitive.Root;

interface SheetContentProps {
  /** The edge the panel slides in from. */
  side: "left" | "right";
  /** Spoken name of the panel («Навигация», «Участники»); not shown. */
  title: string;
  children: ReactNode;
  className?: string;
}

/**
 * A panel over the screen from one edge, for what has no room on a narrow
 * window (the server and channel lists, the member list). A Radix dialog:
 * focus stays inside, Escape and a tap on the scrim close it, and focus goes
 * back to what opened it.
 */
export const SheetContent = ({ side, title, children, className }: SheetContentProps) => (
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay className={overlayClass} />
    <DialogPrimitive.Content
      aria-describedby={undefined}
      className={cn(
        "fixed inset-y-0 z-(--z-dialog) flex max-w-[calc(100vw-3rem)] animate-fade-in overflow-hidden shadow-dialog focus:outline-none",
        side === "left" ? "left-0" : "right-0",
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
