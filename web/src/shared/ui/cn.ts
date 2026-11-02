import { extendTailwindMerge } from "tailwind-merge";

/**
 * The token names of the Tailwind theme in index.css. tailwind-merge has to
 * know them to tell, say, `text-fg-muted` (a colour) from `text-sm` (a size).
 * cn.test.ts checks this list against index.css.
 */
export const THEME_COLORS = [
  "rail",
  "panel",
  "sidebar",
  "chat",
  "input",
  "overlay",
  "tooltip",
  "guild",
  "control",
  "control-hover",
  "hover",
  "selected",
  "message-hover",
  "mention",
  "mention-chip",
  "scrim",
  "skeleton",
  "code",
  "spoiler",
  "spoiler-shown",
  "fg",
  "fg-muted",
  "fg-faint",
  "link",
  "fg-danger",
  "fg-warning",
  "fg-success",
  "fg-mention",
  "accent",
  "accent-hover",
  "on-accent",
  "danger",
  "danger-hover",
  "on-danger",
  "warning",
  "badge-count",
  "online",
  "offline",
  "border",
  "border-strong",
  "border-control",
  "focus",
  "on-avatar",
  "on-guild",
  "switch-thumb",
] as const;
export const THEME_TEXT_SIZES = ["xs", "sm", "base", "lg", "xl"] as const;
export const THEME_SHADOWS = ["float", "dialog"] as const;
export const THEME_RADII = ["xs", "sm", "md", "lg", "xl"] as const;

const merge = extendTailwindMerge({
  extend: {
    theme: {
      color: [...THEME_COLORS],
      text: [...THEME_TEXT_SIZES],
      shadow: [...THEME_SHADOWS],
      radius: [...THEME_RADII],
    },
  },
});

/**
 * Joins class names, skipping falsy parts, and resolves Tailwind conflicts in
 * favour of the later class: a caller's `className` overrides the component's
 * own (`cn("h-8", "h-7")` → `"h-7"`).
 */
export function cn(...parts: (string | false | null | undefined)[]): string {
  return merge(parts.filter(Boolean));
}
