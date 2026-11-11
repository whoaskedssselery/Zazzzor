import { Popover as PopoverPrimitive } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../cn";

export const Popover = PopoverPrimitive.Root;
export const PopoverTrigger = PopoverPrimitive.Trigger;
export const PopoverAnchor = PopoverPrimitive.Anchor;

/**
 * A floating panel with its own controls (a search field, a grid), unlike
 * a menu: it takes the focus, Esc and a click outside close it.
 */
export const PopoverContent = ({
  className,
  sideOffset = 6,
  ...props
}: ComponentProps<typeof PopoverPrimitive.Content>) => (
  <PopoverPrimitive.Portal>
    <PopoverPrimitive.Content
      sideOffset={sideOffset}
      collisionPadding={8}
      className={cn(
        "z-(--z-dropdown) animate-float overflow-hidden rounded-lg bg-overlay text-fg shadow-float ring-1 ring-border outline-none",
        className,
      )}
      {...props}
    />
  </PopoverPrimitive.Portal>
);
