import { cn } from "../cn";

interface SpinnerProps {
  /** Spoken label; omit when the spinner sits inside something already labelled (a busy button). */
  label?: string;
  size?: number;
  className?: string;
}

/** A small arc that turns. Inherits the text colour. */
export function Spinner({ label, size = 16, className }: SpinnerProps) {
  return (
    // biome-ignore lint/a11y/noSvgWithoutTitle: the spread below gives it role="img" with aria-label, or aria-hidden.
    <svg
      viewBox="0 0 16 16"
      width={size}
      height={size}
      fill="none"
      className={cn("shrink-0 animate-spin", className)}
      {...(label ? { role: "img", "aria-label": label } : { "aria-hidden": true })}
    >
      <circle cx="8" cy="8" r="6.5" stroke="currentColor" strokeOpacity="0.25" strokeWidth="2" />
      <path d="M14.5 8A6.5 6.5 0 0 0 8 1.5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  );
}
