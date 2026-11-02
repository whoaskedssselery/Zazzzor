import type { CSSProperties } from "react";
import { cn } from "../cn";

/** A role colour from the API (RGB integer, 0 = none) as CSS, or null for "no colour". */
export const roleColorCss = (color: number): string | null => {
  if (!Number.isInteger(color) || color <= 0 || color > 0xffffff) return null;
  return `#${color.toString(16).padStart(6, "0")}`;
};

interface RoleColorDotProps {
  color: number;
  size?: number;
  className?: string;
}

/** The role's colour as a dot (role lists, the member card). A role without a colour gets a hollow ring. */
export const RoleColorDot = ({ color, size = 12, className }: RoleColorDotProps) => {
  const css = roleColorCss(color);
  return (
    <span
      aria-hidden
      className={cn("inline-block shrink-0 rounded-full", !css && "ring-2 ring-offline ring-inset", className)}
      style={{ width: size, height: size, ...(css ? { backgroundColor: css } : {}) }}
    />
  );
};

/** Style for a name in its role colour: pair with className="role-text" (index.css clamps the lightness per theme). */
export const roleTextStyle = (color: number): CSSProperties => {
  const css = roleColorCss(color);
  return css ? ({ "--role": css } as CSSProperties) : {};
};
