/**
 * Joins class names, skipping falsy parts. There is no Tailwind class merging:
 * a component's own classes and a caller's `className` must not set the same
 * property (callers add layout such as margins and widths, not colours).
 */
export function cn(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(" ");
}
