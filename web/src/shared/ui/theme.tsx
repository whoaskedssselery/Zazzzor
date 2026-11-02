import { createContext, type ReactNode, use, useCallback, useEffect, useMemo, useState } from "react";

export type ThemePreference = "dark" | "light" | "system";
type ResolvedTheme = "dark" | "light";

export const THEME_STORAGE_KEY = "zazzzor.theme";
const LIGHT_QUERY = "(prefers-color-scheme: light)";
/**
 * The browser chrome colour: the rail's --bg-rail in each theme. public/theme-boot.js
 * repeats these and the storage key (it runs before any module); theme.test.tsx
 * runs it to keep the two in step.
 */
export const THEME_COLOR: Record<ResolvedTheme, string> = { dark: "#0e0d0c", light: "#dcd9d4" };

const isPreference = (v: unknown): v is ThemePreference => v === "dark" || v === "light" || v === "system";

/** The stored preference; dark when nothing (or garbage) is stored, or storage is blocked. */
export const readThemePreference = (): ThemePreference => {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY);
    return isPreference(stored) ? stored : "dark";
  } catch {
    return "dark";
  }
};

const resolve = (pref: ThemePreference): ResolvedTheme => {
  if (pref !== "system") return pref;
  return window.matchMedia(LIGHT_QUERY).matches ? "light" : "dark";
};

/**
 * Writes the theme onto <html> (tokens.css keys off data-theme) and the
 * color-scheme meta, so form controls and scrollbars follow. main.tsx calls it
 * before the first render to avoid a flash of the wrong theme.
 */
export const applyTheme = (pref: ThemePreference): void => {
  const theme = resolve(pref);
  document.documentElement.dataset.theme = theme;
  document.querySelector('meta[name="color-scheme"]')?.setAttribute("content", theme);
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", THEME_COLOR[theme]);
};

interface ThemeContextValue {
  theme: ThemePreference;
  setTheme: (t: ThemePreference) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export const ThemeProvider = ({ children }: { children: ReactNode }) => {
  const [theme, setThemeState] = useState<ThemePreference>(readThemePreference);

  useEffect(() => {
    applyTheme(theme);
    if (theme !== "system") return;
    const media = window.matchMedia(LIGHT_QUERY);
    const onChange = () => {
      applyTheme("system");
    };
    media.addEventListener("change", onChange);
    return () => {
      media.removeEventListener("change", onChange);
    };
  }, [theme]);

  const setTheme = useCallback((t: ThemePreference) => {
    try {
      localStorage.setItem(THEME_STORAGE_KEY, t);
    } catch {
      // Storage blocked (private mode): the choice lasts for this tab only.
    }
    setThemeState(t);
  }, []);

  const value = useMemo(() => ({ theme, setTheme }), [theme, setTheme]);
  return <ThemeContext value={value}>{children}</ThemeContext>;
};

/** The user's theme preference and its setter. */
export const useTheme = (): ThemeContextValue => {
  const ctx = use(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used inside <ThemeProvider>");
  return ctx;
};
