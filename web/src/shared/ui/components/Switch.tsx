import { Switch as SwitchPrimitive } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../cn";

/** An on/off setting that applies at once (Radix Switch, role="switch"). Pair it with a <label htmlFor>. */
export const Switch = ({ className, ...props }: ComponentProps<typeof SwitchPrimitive.Root>) => (
  <SwitchPrimitive.Root
    className={cn(
      "relative inline-flex h-6 w-10 shrink-0 cursor-pointer items-center rounded-full bg-offline transition-colors duration-(--duration-base)",
      "data-[state=checked]:bg-accent disabled:cursor-not-allowed disabled:opacity-50",
      className,
    )}
    {...props}
  >
    <SwitchPrimitive.Thumb className="block size-4.5 translate-x-0.75 rounded-full bg-switch-thumb shadow-float transition-transform duration-(--duration-base) ease-(--ease-out) data-[state=checked]:translate-x-4.75" />
  </SwitchPrimitive.Root>
);
