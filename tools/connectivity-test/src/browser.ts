export type BrowserName = "chrome" | "edge" | "firefox" | "other";

export function detectBrowser(userAgent: string): { name: BrowserName; reliable60fps: boolean } {
  if (/\bEdg\//.test(userAgent)) return { name: "edge", reliable60fps: true };
  if (/\bFirefox\//.test(userAgent)) return { name: "firefox", reliable60fps: false };
  if (/\bChrome\//.test(userAgent)) return { name: "chrome", reliable60fps: true };
  return { name: "other", reliable60fps: false };
}
