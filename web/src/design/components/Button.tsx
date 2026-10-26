import { Slot } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../cn";
import { Spinner } from "./Spinner";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "link";
export type ButtonSize = "sm" | "md" | "lg";

export const buttonVariants: Record<ButtonVariant, string> = {
  primary: "bg-accent text-on-accent hover:bg-accent-hover",
  secondary: "bg-control text-fg hover:bg-control-hover",
  ghost: "bg-transparent text-fg hover:bg-hover",
  danger: "bg-danger text-on-danger hover:bg-danger-hover",
  link: "h-auto! px-0! bg-transparent text-link underline-offset-2 hover:underline",
};

const sizes: Record<ButtonSize, string> = {
  sm: "h-8 px-3 text-sm gap-1.5",
  md: "h-9.5 px-4 text-sm gap-2",
  lg: "h-11 px-6 text-base gap-2",
};

export const buttonBase =
  "inline-flex shrink-0 cursor-pointer items-center justify-center rounded-md font-medium whitespace-nowrap select-none " +
  "transition-colors duration-(--duration-fast) disabled:cursor-not-allowed disabled:opacity-50 aria-busy:cursor-progress";

export interface ButtonProps extends ComponentProps<"button"> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Render the child element (a link, a Radix trigger) with the button's look. */
  asChild?: boolean;
  /** Busy: shows a spinner, blocks clicks, keeps the width. */
  loading?: boolean;
}

export function Button({
  variant = "primary",
  size = "md",
  asChild = false,
  loading = false,
  disabled,
  className,
  children,
  type,
  ...props
}: ButtonProps) {
  const classes = cn(buttonBase, sizes[size], buttonVariants[variant], className);
  if (asChild) {
    return (
      <Slot.Root className={classes} {...props}>
        {children}
      </Slot.Root>
    );
  }
  return (
    <button
      type={type ?? "button"}
      className={classes}
      disabled={disabled ?? loading}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading && <Spinner size={size === "lg" ? 18 : 16} />}
      {children}
    </button>
  );
}
