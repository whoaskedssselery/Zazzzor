import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { runInNewContext } from "node:vm";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { THEME_COLOR, THEME_STORAGE_KEY, type ThemePreference, ThemeProvider, useTheme } from "./theme";

const wrapper = ({ children }: { children: ReactNode }) => <ThemeProvider>{children}</ThemeProvider>;
const root = document.documentElement;

/**
 * A controllable OS colour scheme: matchMedia answers from `os.light`, and
 * `os.set(light)` flips it and fires "change" on every list handed out, as a
 * browser does when the user switches the OS theme.
 */
function stubOsScheme(light: boolean) {
  const original = Object.getOwnPropertyDescriptor(window, "matchMedia");
  const lists: MediaQueryList[] = [];
  const os = {
    light,
    set(next: boolean) {
      os.light = next;
      for (const list of lists) list.dispatchEvent(new Event("change"));
    },
    restore() {
      if (original) Object.defineProperty(window, "matchMedia", original);
    },
  };
  const stub = (query: string): MediaQueryList => {
    const list = new EventTarget() as MediaQueryList;
    Object.defineProperty(list, "matches", { get: () => query.includes("light") === os.light });
    Object.assign(list, { media: query, onchange: null });
    lists.push(list);
    return list;
  };
  Object.defineProperty(window, "matchMedia", { configurable: true, writable: true, value: stub });
  return os;
}

function meta(name: string): HTMLMetaElement {
  const el = document.createElement("meta");
  el.name = name;
  document.head.append(el);
  return el;
}

describe("useTheme", () => {
  afterEach(() => {
    localStorage.clear();
    delete root.dataset.theme;
    vi.restoreAllMocks();
  });

  it("starts dark when nothing is stored", () => {
    const { result } = renderHook(() => useTheme(), { wrapper });
    expect(result.current.theme).toBe("dark");
    expect(root.dataset.theme).toBe("dark");
  });

  it("applies and remembers the chosen theme", () => {
    const { result } = renderHook(() => useTheme(), { wrapper });
    act(() => {
      result.current.setTheme("light");
    });
    expect(result.current.theme).toBe("light");
    expect(root.dataset.theme).toBe("light");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
  });

  it("follows the OS preference for system, including a later OS switch", () => {
    const os = stubOsScheme(true);
    try {
      localStorage.setItem(THEME_STORAGE_KEY, "system");
      const { result } = renderHook(() => useTheme(), { wrapper });
      expect(result.current.theme).toBe("system");
      expect(root.dataset.theme).toBe("light");

      act(() => {
        os.set(false);
      });
      expect(root.dataset.theme).toBe("dark");
      act(() => {
        os.set(true);
      });
      expect(root.dataset.theme).toBe("light");
    } finally {
      os.restore();
    }
  });

  it("stops following the OS once a fixed theme is chosen", () => {
    const os = stubOsScheme(false);
    try {
      localStorage.setItem(THEME_STORAGE_KEY, "system");
      const { result } = renderHook(() => useTheme(), { wrapper });
      act(() => {
        result.current.setTheme("dark");
      });
      act(() => {
        os.set(true);
      });
      expect(root.dataset.theme).toBe("dark");
    } finally {
      os.restore();
    }
  });

  it("ignores a garbage stored value", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "neon");
    const { result } = renderHook(() => useTheme(), { wrapper });
    expect(result.current.theme).toBe("dark");
  });

  it("falls back to dark and keeps working when storage is blocked", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    const { result } = renderHook(() => useTheme(), { wrapper });
    expect(result.current.theme).toBe("dark");
    act(() => {
      result.current.setTheme("light");
    });
    expect(result.current.theme).toBe("light");
    expect(root.dataset.theme).toBe("light");
  });
});

/**
 * public/theme-boot.js runs before the first paint (a plain <script> in
 * index.html head). It must agree with theme.tsx on the storage key, the
 * values and the browser-chrome colours; run it here against the real DOM.
 */
describe("theme-boot.js", () => {
  const source = readFileSync(
    join(dirname(fileURLToPath(import.meta.url)), "..", "..", "..", "public", "theme-boot.js"),
    "utf8",
  );
  let metas: HTMLMetaElement[] = [];

  beforeEach(() => {
    metas = [meta("color-scheme"), meta("theme-color")];
  });
  afterEach(() => {
    for (const m of metas) m.remove();
    localStorage.clear();
    delete root.dataset.theme;
    vi.restoreAllMocks();
  });

  function boot() {
    runInNewContext(source, { document, localStorage, matchMedia: window.matchMedia.bind(window) });
  }

  it.each<[ThemePreference | null, boolean, "dark" | "light"]>([
    [null, true, "dark"],
    ["dark", true, "dark"],
    ["light", false, "light"],
    ["system", true, "light"],
    ["system", false, "dark"],
  ])("stored %s with an OS preferring light=%s → %s", (stored, osLight, expected) => {
    const os = stubOsScheme(osLight);
    try {
      if (stored) localStorage.setItem(THEME_STORAGE_KEY, stored);
      boot();
      expect(root.dataset.theme).toBe(expected);
      expect(metas[0]?.content).toBe(expected);
      expect(metas[1]?.content).toBe(THEME_COLOR[expected]);
    } finally {
      os.restore();
    }
  });

  it("falls back to dark when storage is blocked", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    boot();
    expect(root.dataset.theme).toBe("dark");
  });

  it("is loaded from index.html head as a classic script, before the app", () => {
    const html = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "..", "..", "..", "index.html"), "utf8");
    const head = html.slice(0, html.indexOf("</head>"));
    expect(head).toContain('<script src="/theme-boot.js"></script>');
  });
});
