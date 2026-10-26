import { CircleAlert } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../cn";

/** A form-level error (role="alert"): what went wrong and, when there is one, the way out. */
export const FormAlert = ({ children, className }: { children: ReactNode; className?: string }) => (
  <div role="alert" className={cn("mb-5 flex gap-2.5 rounded-md bg-danger/12 px-3 py-2.5 text-sm text-fg", className)}>
    <CircleAlert aria-hidden className="mt-px size-4.5 shrink-0 text-fg-danger" strokeWidth={2} />
    <p className="min-w-0">{children}</p>
  </div>
);
