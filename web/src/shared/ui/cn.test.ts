import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { cn, THEME_COLORS, THEME_RADII, THEME_SHADOWS, THEME_TEXT_SIZES } from "./cn";

const indexCss = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), "..", "..", "app", "styles", "index.css"),
  "utf8",
);

/** Names declared in index.css's Tailwind theme for one namespace (`--color-chat` → `chat`). */
function themeNames(namespace: string): string[] {
  const re = new RegExp(`^\\s*--${namespace}-([a-z0-9-]+?)(?:--[a-z-]+)?:`, "gm");
  return [...new Set([...indexCss.matchAll(re)].map((m) => m[1] ?? ""))].filter((n) => n !== "" && n !== "*");
}

describe("cn", () => {
  it("lets the later class win a conflict", () => {
    expect(cn("h-8", "h-7")).toBe("h-7");
    expect(cn("px-4", false, null, undefined, "px-0")).toBe("px-0");
  });

  it("keeps a token colour and a token size, which do not conflict", () => {
    expect(cn("text-fg-muted", "text-sm")).toBe("text-fg-muted text-sm");
    expect(cn("text-sm", "text-fg-muted")).toBe("text-sm text-fg-muted");
  });

  it("resolves token colours, sizes, shadows and radii by group", () => {
    expect(cn("bg-chat", "bg-sidebar")).toBe("bg-sidebar");
    expect(cn("text-fg", "text-link")).toBe("text-link");
    expect(cn("text-sm", "text-lg")).toBe("text-lg");
    expect(cn("shadow-float", "shadow-dialog")).toBe("shadow-dialog");
    expect(cn("shadow-float", "bg-overlay")).toBe("shadow-float bg-overlay");
    expect(cn("rounded-md", "rounded-xl")).toBe("rounded-xl");
    expect(cn("ring-border", "ring-2")).toBe("ring-border ring-2");
  });

  it("knows every token of the Tailwind theme in index.css", () => {
    expect([...THEME_COLORS].sort()).toEqual(themeNames("color").sort());
    expect([...THEME_TEXT_SIZES].sort()).toEqual(themeNames("text").sort());
    expect([...THEME_SHADOWS].sort()).toEqual(themeNames("shadow").sort());
    expect([...THEME_RADII].sort()).toEqual(themeNames("radius").sort());
  });

  it("has no shadow token named like a colour token (tailwind-merge could not tell them apart)", () => {
    const colors = new Set<string>(THEME_COLORS);
    expect(THEME_SHADOWS.filter((s) => colors.has(s))).toEqual([]);
  });
});
