// Sets the theme before the first paint, so a light or "system" user never
// sees a flash of the dark default. A classic script from 'self' (the CSP
// bans inline scripts). Mirrors applyTheme() in src/shared/ui/theme.tsx: same
// storage key, values and colours; theme.test.tsx runs this file to check.
(() => {
  var pref = "dark";
  var stored;
  try {
    stored = localStorage.getItem("zazzzor.theme");
    if (stored === "light" || stored === "system") pref = stored;
  } catch {
    // Storage blocked: keep the dark default.
  }
  var theme = pref === "system" ? (matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark") : pref;
  document.documentElement.setAttribute("data-theme", theme);
  var scheme = document.querySelector('meta[name="color-scheme"]');
  if (scheme) scheme.setAttribute("content", theme);
  var color = document.querySelector('meta[name="theme-color"]');
  if (color) color.setAttribute("content", theme === "light" ? "#dcd9d4" : "#0e0d0c");
})();
