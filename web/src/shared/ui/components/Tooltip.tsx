import { Tooltip as TooltipPrimitive } from "radix-ui";
import type { ReactNode } from "react";

interface TooltipProps {
  content: ReactNode;
  children: ReactNode;
  side?: "top" | "right" | "bottom" | "left";
  /** Rail tooltips (server names) open beside the icon without waiting. */
  delayDuration?: number;
}

/**
 * A short label shown on hover and on keyboard focus (Radix), linked to the
 * trigger with aria-describedby. The trigger must be focusable; the child is
 * used as the trigger element. Needs the TooltipProvider in AppProviders.
 */
export const Tooltip = ({ content, children, side = "top", delayDuration }: TooltipProps) => (
  <TooltipPrimitive.Root {...(delayDuration === undefined ? {} : { delayDuration })}>
    <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        side={side}
        sideOffset={8}
        collisionPadding={8}
        className="z-(--z-tooltip) max-w-72 animate-fade-in rounded-md bg-tooltip px-2.5 py-1.5 text-sm font-medium text-fg shadow-float ring-1 ring-border"
      >
        {content}
      </TooltipPrimitive.Content>
    </TooltipPrimitive.Portal>
  </TooltipPrimitive.Root>
);
