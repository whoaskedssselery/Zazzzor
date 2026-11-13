import type { LucideIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "../cn";
import { Tooltip } from "./Tooltip";

export interface IconButtonProps extends Omit<ComponentProps<"button">, "children" | "aria-label"> {
  /** Accessible name, also shown as the tooltip. Required: an icon alone says nothing to a screen reader. */
  label: string;
  icon: LucideIcon;
  size?: "sm" | "md";
  variant?: "ghost" | "danger";
  /** For toggles (mute, show members): rendered with aria-pressed and the selected wash. */
  pressed?: boolean;
  /** Set false when an enclosing component already shows the label. */
  tooltip?: boolean;
  tooltipSide?: "top" | "right" | "bottom" | "left";
}

// On a touch screen the target grows to 44px (40px for the small one), the
// glyph a little with it.
const sizes = {
  sm: "size-7 [&>svg]:size-4 pointer-coarse:size-10 pointer-coarse:[&>svg]:size-5",
  md: "size-8 [&>svg]:size-5 pointer-coarse:size-11 pointer-coarse:[&>svg]:size-6",
};

export const IconButton = ({
  label,
  icon: Icon,
  size = "md",
  variant = "ghost",
  pressed,
  tooltip = true,
  tooltipSide,
  className,
  type,
  ...props
}: IconButtonProps) => {
  const button = (
    <button
      type={type ?? "button"}
      aria-label={label}
      aria-pressed={pressed}
      className={cn(
        "inline-flex shrink-0 cursor-pointer items-center justify-center rounded-md transition-[color,background-color,scale] duration-(--duration-fast) active:scale-[0.92] disabled:active:scale-100",
        "disabled:cursor-not-allowed disabled:opacity-50",
        variant === "danger" ? "text-fg-danger hover:bg-hover" : "text-fg-muted hover:bg-hover hover:text-fg",
        pressed && "bg-selected text-fg",
        sizes[size],
        className,
      )}
      {...props}
    >
      <Icon aria-hidden strokeWidth={1.75} />
    </button>
  );
  return tooltip ? (
    <Tooltip content={label} {...(tooltipSide ? { side: tooltipSide } : {})}>
      {button}
    </Tooltip>
  ) : (
    button
  );
};
