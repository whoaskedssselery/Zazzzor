import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig } from "@playwright/test";

/** The page is checked under the same CSP that Caddy sends in production. */
function cspFromCaddyfile(): string {
  const caddyfile = readFileSync(fileURLToPath(new URL("../../deploy/caddy/static.caddy", import.meta.url)), "utf8");
  const connectivity = /^\(connectivity\) \{([\s\S]*?)^\}/m.exec(caddyfile)?.[1];
  const csp = /Content-Security-Policy "([^"]+)"/.exec(connectivity ?? "")?.[1];
  if (!csp) throw new Error("Connectivity CSP not found in deploy/caddy/static.caddy");
  return csp;
}

const PORT = 5174;

export default defineConfig({
  testDir: "e2e",
  timeout: 120_000,
  workers: 1,
  reporter: "list",
  globalSetup: "./e2e/global-setup.ts",
  use: {
    baseURL: `http://localhost:${PORT}`,
    // Installed Google Chrome by default: it has the H.264 encoder friends will
    // use, and needs no download (Playwright's CDN is unreachable from some
    // networks). E2E_CHANNEL=chromium switches to Playwright's own build.
    channel: process.env.E2E_CHANNEL ?? "chrome",
    headless: true,
    launchOptions: {
      args: [
        // Auto-accept getUserMedia/getDisplayMedia prompts…
        "--use-fake-ui-for-media-stream",
        // …and feed fake mic/camera and a fake 1920×1080 "screen" at 60 fps.
        "--use-fake-device-for-media-stream=fps=60",
      ],
    },
  },
  webServer: {
    // Production bundle via `vite preview` (proxying /rtc like Caddy does).
    command: `pnpm exec vite build && pnpm exec vite preview --port ${PORT} --strictPort`,
    url: `http://localhost:${PORT}/connectivity-test/`,
    reuseExistingServer: false,
    timeout: 60_000,
    env: { ZAZZZOR_PREVIEW_CSP: cspFromCaddyfile() },
  },
});
