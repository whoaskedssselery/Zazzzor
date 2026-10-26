import type { ComponentProps } from "react";
import { cn } from "../cn";

export type BadgeVariant = "count" | "neutral" | "accent" | "live";

const variants: Record<BadgeVariant, string> = {
  /** Unread mentions: white on red, on rail icons and channel rows. */
  count: "min-w-4 h-4 px-1 rounded-full bg-badge-count text-on-danger text-xs font-bold tabular-nums leading-4",
  /** A tag beside a name or a setting («Владелец», «Скоро»). */
  neutral: "h-5 px-1.5 rounded-sm bg-control text-fg text-xs font-semibold",
  accent: "h-5 px-1.5 rounded-sm bg-accent text-on-accent text-xs font-semibold",
  /** Someone is streaming («В эфире»). */
  live: "h-4 px-1 rounded-sm bg-danger text-on-danger text-[11px] font-bold leading-4",
};

interface BadgeProps extends ComponentProps<"span"> {
  variant?: BadgeVariant;
}

/** A small label. For counts, render the number and give the parent an accessible name with the noun. */
export const Badge = ({ variant = "neutral", className, ...props }: BadgeProps) => (
  <span
    className={cn("inline-flex shrink-0 items-center justify-center whitespace-nowrap", variants[variant], className)}
    {...props}
  />
);

/** «99+» above 99, as Discord caps its counters. */
export const formatCount = (n: number): string => (n > 99 ? "99+" : String(n));
