import type { ComponentProps } from "react";
import { cn } from "../cn";
import { fieldClass, useFieldControl } from "./Input";

/** Multi-line text (a channel topic, a ban reason). Same look, states and Field wiring as Input. */
export const Textarea = ({
  className,
  rows,
  id,
  "aria-describedby": describedBy,
  "aria-invalid": invalid,
  ...props
}: ComponentProps<"textarea">) => {
  const wiring = useFieldControl({ id, "aria-describedby": describedBy, "aria-invalid": invalid });
  return <textarea rows={rows ?? 3} className={cn(fieldClass, "resize-y py-2.5", className)} {...wiring} {...props} />;
};
