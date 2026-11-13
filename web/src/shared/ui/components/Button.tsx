import { Slot } from "radix-ui";
import type { ComponentProps, MouseEvent } from "react";
import { cn } from "../cn";
import { Spinner } from "./Spinner";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "link";
export type ButtonSize = "sm" | "md" | "lg";

/** Applied after the size, so `link` drops the size's height and padding (cn resolves the conflict). */
export const buttonVariants: Record<ButtonVariant, string> = {
  primary: "bg-accent text-on-accent hover:bg-accent-hover",
  secondary: "bg-control text-fg hover:bg-control-hover",
  ghost: "bg-transparent text-fg hover:bg-hover",
  danger: "bg-danger text-on-danger hover:bg-danger-hover",
  link: "h-auto px-0 bg-transparent text-link underline-offset-2 hover:underline",
};

const sizes: Record<ButtonSize, string> = {
  sm: "h-8 px-3 text-sm gap-1.5",
  md: "h-9.5 px-4 text-sm gap-2",
  lg: "h-11 px-6 text-base gap-2",
};

export const buttonBase =
  "inline-flex shrink-0 cursor-pointer items-center justify-center rounded-md font-medium whitespace-nowrap select-none " +
  "transition-[color,background-color,border-color,box-shadow,scale] duration-(--duration-fast) active:scale-[0.97] " +
  "disabled:active:scale-100 aria-disabled:active:scale-100 disabled:cursor-not-allowed disabled:opacity-50 aria-busy:cursor-progress " +
  "aria-disabled:cursor-not-allowed aria-disabled:opacity-50";

export interface ButtonProps extends ComponentProps<"button"> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /**
   * Render the child element (a link, a Radix trigger) with the button's look.
   * `disabled` then sets aria-disabled and data-disabled and swallows clicks,
   * since a link has no disabled attribute; `loading` is not shown.
   */
  asChild?: boolean;
  /** Busy: shows a spinner, blocks clicks, keeps the width. */
  loading?: boolean;
}

export const Button = ({
  variant = "primary",
  size = "md",
  asChild = false,
  loading = false,
  disabled = false,
  className,
  children,
  type,
  onClick,
  ...props
}: ButtonProps) => {
  const classes = cn(buttonBase, sizes[size], buttonVariants[variant], className);
  if (asChild) {
    const blocked = disabled || loading;
    return (
      <Slot.Root
        className={classes}
        aria-disabled={blocked || undefined}
        data-disabled={blocked ? "" : undefined}
        onClick={(e: MouseEvent<HTMLButtonElement>) => {
          if (blocked) {
            e.preventDefault();
            e.stopPropagation();
            return;
          }
          onClick?.(e);
        }}
        {...props}
      >
        {children}
      </Slot.Root>
    );
  }
  return (
    <button
      type={type ?? "button"}
      className={classes}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      onClick={onClick}
      {...props}
    >
      {loading && <Spinner size={size === "lg" ? 18 : 16} />}
      {children}
    </button>
  );
};
