import type { ComponentProps } from "react";
import { cn } from "../cn";

/**
 * A flat placeholder block at the real size of what is loading (no shimmer).
 * Give it the final element's width, height and radius (a rounded-* class; it
 * has none of its own) so nothing shifts when
 * the data lands. Hidden from assistive tech: announce loading on the region.
 */
export const Skeleton = ({ className, ...props }: ComponentProps<"div">) => (
  <div aria-hidden className={cn("animate-pulse-soft bg-skeleton", className)} {...props} />
);
