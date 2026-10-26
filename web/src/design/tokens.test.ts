import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Contrast check of the colour tokens, read from tokens.css itself: every
 * text token reaches 4.5:1 (WCAG AA, normal text) on each surface it is used
 * on, in both themes. Translucent tokens (hover and selection washes) are
 * composited over the surface beneath them first.
 */

// Read from disk: Vitest does not process CSS, so a ?raw import comes back empty.
const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "tokens.css"), "utf8");

type Theme = "dark" | "light";
type Rgba = [number, number, number, number];

/** The custom properties declared in the first rule whose selector matches. */
function block(selector: RegExp): Map<string, string> {
  const rule = new RegExp(`${selector.source}\\s*\\{([^}]*)\\}`).exec(css);
  if (!rule?.[1]) throw new Error(`tokens.css has no rule for ${selector.source}`);
  const vars = new Map<string, string>();
  for (const m of rule[1].matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) {
    if (m[1] && m[2]) vars.set(m[1], m[2].trim());
  }
  return vars;
}

const themes: Record<Theme, Map<string, string>> = {
  dark: block(/:root,\s*\[data-theme="dark"\]/),
  light: block(/\[data-theme="light"\]/),
};

function parseColor(value: string): Rgba {
  const hex = /^#([0-9a-f]{6})$/i.exec(value);
  if (hex?.[1]) {
    const n = Number.parseInt(hex[1], 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255, 1];
  }
  const rgb = /^rgb\(\s*(\d+)\s+(\d+)\s+(\d+)\s*(?:\/\s*([\d.]+)\s*)?\)$/.exec(value);
  if (rgb?.[1] && rgb[2] && rgb[3]) {
    return [Number(rgb[1]), Number(rgb[2]), Number(rgb[3]), rgb[4] === undefined ? 1 : Number(rgb[4])];
  }
  throw new Error(`unsupported colour ${value}`);
}

function token(theme: Theme, name: string): string {
  const value = themes[theme].get(name);
  if (value === undefined) throw new Error(`${theme} theme has no ${name}`);
  const ref = /^var\((--[\w-]+)\)$/.exec(value);
  return ref?.[1] ? token(theme, ref[1]) : value;
}

/** A surface is a token, or "wash over base" for a translucent layer on a base. */
function surface(theme: Theme, spec: string): Rgba {
  const cut = spec.indexOf(" over ");
  const top = cut < 0 ? spec : spec.slice(0, cut);
  const base = cut < 0 ? undefined : spec.slice(cut + " over ".length);
  const color = parseColor(token(theme, top));
  if (!base) {
    if (color[3] !== 1) throw new Error(`${spec} is translucent; name the surface under it`);
    return color;
  }
  const under = surface(theme, base);
  const a = color[3];
  return [0, 1, 2].map((i) => Math.round((color[i] ?? 0) * a + (under[i] ?? 0) * (1 - a))).concat(1) as Rgba;
}

function luminance([r, g, b]: Rgba): number {
  const lin = (v: number) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function contrast(a: Rgba, b: Rgba): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

const reading = [
  "--bg-sidebar",
  "--bg-chat",
  "--bg-panel",
  "--bg-input",
  "--bg-overlay",
  "--bg-hover over --bg-sidebar",
  "--bg-hover over --bg-chat",
  "--bg-selected over --bg-sidebar",
  "--bg-message-hover over --bg-chat",
  "--bg-mention over --bg-chat",
];

/** Text token → the surfaces it is set on. */
const usage: Record<string, string[]> = {
  "--text": [...reading, "--bg-control", "--bg-control-hover", "--bg-tooltip", "--bg-rail"],
  "--text-muted": [...reading, "--bg-control"],
  "--text-faint": reading,
  "--text-link": ["--bg-chat", "--bg-overlay", "--bg-message-hover over --bg-chat", "--bg-mention over --bg-chat"],
  "--text-danger": ["--bg-chat", "--bg-sidebar", "--bg-overlay", "--bg-input", "--bg-hover over --bg-overlay"],
  "--text-warning": ["--bg-chat", "--bg-overlay"],
  "--text-success": ["--bg-chat", "--bg-overlay"],
  "--text-mention": ["--bg-mention-chip over --bg-chat", "--bg-mention-chip over --bg-mention over --bg-chat"],
  "--on-accent": ["--accent", "--accent-hover"],
  "--on-danger": ["--danger", "--danger-hover", "--badge-count"],
  "--on-avatar": ["--avatar-1", "--avatar-2", "--avatar-3", "--avatar-4", "--avatar-5", "--avatar-6"],
  "--on-guild": ["--bg-guild"],
};

describe.each(["dark", "light"] as const)("%s theme tokens", (theme) => {
  for (const [text, surfaces] of Object.entries(usage)) {
    it.each(surfaces)(`${text} reaches 4.5:1 on %s`, (spec) => {
      const ratio = contrast(parseColor(token(theme, text)), surface(theme, spec));
      expect(ratio, `${text} on ${spec} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(4.5);
    });
  }

  it("declares the same set of tokens as the other theme", () => {
    const other = theme === "dark" ? themes.light : themes.dark;
    expect([...themes[theme].keys()].sort()).toEqual([...other.keys()].sort());
  });

  it("keeps presence dots and focus ring at 3:1 against the sidebar (non-text contrast)", () => {
    for (const name of ["--status-online", "--status-offline", "--focus-ring"]) {
      const ratio = contrast(parseColor(token(theme, name)), surface(theme, "--bg-sidebar"));
      expect(ratio, `${name} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(3);
    }
  });
});

describe("contrast()", () => {
  it("matches the WCAG reference values", () => {
    expect(contrast([0, 0, 0, 1], [255, 255, 255, 1])).toBeCloseTo(21, 5);
    expect(contrast([118, 118, 118, 1], [255, 255, 255, 1])).toBeCloseTo(4.54, 2);
  });
});
