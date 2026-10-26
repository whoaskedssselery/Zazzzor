import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { THEME_STORAGE_KEY, ThemeProvider, useTheme } from "./theme";

const wrapper = ({ children }: { children: ReactNode }) => <ThemeProvider>{children}</ThemeProvider>;
const root = document.documentElement;

/** Stubs matchMedia so the OS asks for the light theme; returns the undo. */
function preferLight(): () => void {
  const original = Object.getOwnPropertyDescriptor(window, "matchMedia");
  const stub = (query: string): MediaQueryList => {
    const list = new EventTarget() as MediaQueryList;
    Object.assign(list, { matches: query.includes("light"), media: query, onchange: null });
    return list;
  };
  Object.defineProperty(window, "matchMedia", { configurable: true, writable: true, value: stub });
  return () => {
    if (original) Object.defineProperty(window, "matchMedia", original);
  };
}

describe("useTheme", () => {
  afterEach(() => {
    localStorage.clear();
    delete root.dataset.theme;
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

  it("follows the OS preference for system", () => {
    const restore = preferLight();
    try {
      localStorage.setItem(THEME_STORAGE_KEY, "system");
      const { result } = renderHook(() => useTheme(), { wrapper });
      expect(result.current.theme).toBe("system");
      expect(root.dataset.theme).toBe("light");
    } finally {
      restore();
    }
  });

  it("ignores a garbage stored value", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "neon");
    const { result } = renderHook(() => useTheme(), { wrapper });
    expect(result.current.theme).toBe("dark");
  });
});
