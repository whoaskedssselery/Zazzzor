import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig, devices } from "@playwright/test";
import { API_PORT, PORT } from "./e2e/env";

/** The CSP Caddy sends with the web client in production (the block with frame-ancestors). */
const productionCsp = (): string => {
  const caddy = readFileSync(fileURLToPath(new URL("../deploy/caddy/static.caddy", import.meta.url)), "utf8");
  const csp = /Content-Security-Policy "([^"]*frame-ancestors[^"]*)"/.exec(caddy)?.[1];
  if (!csp) throw new Error("the web client's Content-Security-Policy is not in deploy/caddy/static.caddy");
  return csp;
};

/**
 * The browser tests: every scenario in Chromium and in Firefox, against the
 * production bundle (`vite preview` with Caddy's CSP) and a real API
 * (e2e/compose.yml, started beforehand: `pnpm e2e:up`). One worker: the
 * reconnect scenario restarts the API under everyone.
 */
export default defineConfig({
  testDir: "e2e",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  workers: 1,
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  globalSetup: "./e2e/global-setup.ts",
  use: {
    baseURL: `http://localhost:${PORT}`,
    locale: "ru-RU",
    timezoneId: "Europe/Warsaw",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        // Fake microphone (a tone) and screen, and no permission prompts.
        launchOptions: {
          args: [
            "--use-fake-ui-for-media-stream",
            "--use-fake-device-for-media-stream",
            "--auto-select-desktop-capture-source=Entire screen",
          ],
        },
      },
    },
    {
      name: "firefox",
      use: {
        ...devices["Desktop Firefox"],
        launchOptions: {
          firefoxUserPrefs: {
            "media.navigator.streams.fake": true,
            "media.navigator.permission.disabled": true,
            // LiveKit offers 127.0.0.1 (e2e/compose.yml), and Firefox skips loopback ICE candidates by default
            // (and gathers them only while the page captures: see enter() in e2e/voice.spec.ts).
            "media.peerconnection.ice.loopback": true,
          },
        },
        // Where Playwright's Firefox does not start (some Windows builds), it
        // runs in Playwright's Linux image instead and still reaches this
        // machine's localhost:
        //   docker run --rm --init -p 127.0.0.1:3000:3000 mcr.microsoft.com/playwright:v1.64.1-noble \
        //     npx -y playwright@1.64.1 run-server --port 3000 --host 0.0.0.0
        //   E2E_FIREFOX_SERVER=ws://127.0.0.1:3000/ pnpm e2e
        ...(process.env.E2E_FIREFOX_SERVER
          ? { connectOptions: { wsEndpoint: process.env.E2E_FIREFOX_SERVER, exposeNetwork: "<loopback>" } }
          : {}),
      },
    },
  ],
  webServer: {
    command: `pnpm exec vite build && pnpm exec vite preview --port ${PORT} --strictPort`,
    url: `http://localhost:${PORT}/login`,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: { ZAZZZOR_API_TARGET: `http://127.0.0.1:${API_PORT}`, ZAZZZOR_PREVIEW_CSP: productionCsp() },
  },
});
