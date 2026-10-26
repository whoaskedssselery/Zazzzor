import type { ComponentProps } from "react";
import { cn } from "../cn";
import { fieldClass } from "./Input";

/** Multi-line text (a channel topic, a ban reason). Same look and states as Input. */
export function Textarea({ className, rows, ...props }: ComponentProps<"textarea">) {
  return <textarea rows={rows ?? 3} className={cn(fieldClass, "resize-y py-2.5", className)} {...props} />;
}
