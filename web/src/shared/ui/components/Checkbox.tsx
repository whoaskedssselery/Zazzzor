import { Check } from "lucide-react";
import { Checkbox as CheckboxPrimitive } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../cn";

/** A square tick box (Radix Checkbox, role="checkbox"). Pair it with a <label htmlFor>. */
export const Checkbox = ({ className, ...props }: ComponentProps<typeof CheckboxPrimitive.Root>) => (
  <CheckboxPrimitive.Root
    className={cn(
      "inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-sm ring-2 ring-border-control ring-inset transition-colors duration-(--duration-fast)",
      "data-[state=checked]:bg-accent data-[state=checked]:ring-0",
      "disabled:cursor-not-allowed disabled:opacity-50",
      className,
    )}
    {...props}
  >
    <CheckboxPrimitive.Indicator className="text-on-accent">
      <Check aria-hidden className="size-4" strokeWidth={3} />
    </CheckboxPrimitive.Indicator>
  </CheckboxPrimitive.Root>
);
