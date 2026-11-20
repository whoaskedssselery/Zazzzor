import { readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig, devices } from "@playwright/test";
import { API_PORT, PORT } from "./e2e/env";

/**
 * The fake microphone of Chromium: a steady 440 Hz tone (2 s, 48 kHz mono,
 * looped by Chromium). Its own fake device plays a short beep once a second,
 * which LiveKit does not always count as speech, so «who speaks» flickered.
 * Written to a temporary file when the config loads, so no binary is kept.
 */
const steadyTone = (): string => {
  const rate = 48_000;
  const samples = rate * 2;
  const wav = Buffer.alloc(44 + samples * 2);
  wav.write("RIFF", 0);
  wav.writeUInt32LE(36 + samples * 2, 4);
  wav.write("WAVEfmt ", 8);
  wav.writeUInt32LE(16, 16); // fmt chunk size
  wav.writeUInt16LE(1, 20); // PCM
  wav.writeUInt16LE(1, 22); // mono
  wav.writeUInt32LE(rate, 24);
  wav.writeUInt32LE(rate * 2, 28); // bytes per second
  wav.writeUInt16LE(2, 32); // bytes per frame
  wav.writeUInt16LE(16, 34); // bits per sample
  wav.write("data", 36);
  wav.writeUInt32LE(samples * 2, 40);
  for (let i = 0; i < samples; i++) {
    wav.writeInt16LE(Math.round(Math.sin((2 * Math.PI * 440 * i) / rate) * 0.5 * 32_767), 44 + i * 2);
  }
  const file = join(tmpdir(), "zazzzor-e2e-tone.wav");
  writeFileSync(file, wav);
  return file;
};

/** The CSP Caddy sends with the web client in production (the block with frame-ancestors). */
const productionCsp = (): string => {
  const caddy = readFileSync(fileURLToPath(new URL("../deploy/caddy/static.caddy", import.meta.url)), "utf8");
  const csp = /Content-Security-Policy "([^"]*frame-ancestors[^"]*)"/.exec(caddy)?.[1];
  if (!csp) throw new Error("the web client's Content-Security-Policy is not in deploy/caddy/static.caddy");
  return csp;
};

/** Chromium with a fake microphone (a steady tone) and screen, and no permission prompts. */
const chromium = {
  ...devices["Desktop Chrome"],
  launchOptions: {
    args: [
      "--use-fake-ui-for-media-stream",
      "--use-fake-device-for-media-stream",
      `--use-file-for-fake-audio-capture=${steadyTone()}`,
      "--auto-select-desktop-capture-source=Entire screen",
    ],
  },
};

/** Firefox with fake media. */
const firefox = {
  ...devices["Desktop Firefox"],
  launchOptions: {
    firefoxUserPrefs: {
      "media.navigator.streams.fake": true,
      "media.navigator.permission.disabled": true,
      // The microphone is allowed, as Chromium's fake UI does. Without the permission Firefox hides its
      // addresses behind mDNS names, and ICE with LiveKit on 127.0.0.1 (e2e/compose.yml) never connects.
      "permissions.default.microphone": 1,
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
};

/** The scenarios that restart the API under everyone: they run alone, after the rest. */
const RESTARTS = /(reconnect|voice)\.spec\.ts$/;

/**
 * The browser tests: every scenario in Chromium and in Firefox, against the
 * production bundle (`vite preview` with Caddy's CSP) and a real API
 * (e2e/compose.yml, started beforehand: `pnpm e2e:up`). Files run side by
 * side (each signs up its own people); the scenarios that restart the API
 * run one at a time once everything else is done.
 */
export default defineConfig({
  testDir: "e2e",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  workers: 3,
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
    { name: "chromium", use: chromium, testIgnore: RESTARTS },
    { name: "firefox", use: firefox, testIgnore: RESTARTS },
    {
      name: "chromium-restarts",
      use: chromium,
      testMatch: RESTARTS,
      workers: 1,
      dependencies: ["chromium", "firefox"],
    },
    { name: "firefox-restarts", use: firefox, testMatch: RESTARTS, workers: 1, dependencies: ["chromium-restarts"] },
  ],
  webServer: {
    command: `pnpm exec vite build && pnpm exec vite preview --port ${PORT} --strictPort`,
    url: `http://localhost:${PORT}/login`,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: { ZAZZZOR_API_TARGET: `http://127.0.0.1:${API_PORT}`, ZAZZZOR_PREVIEW_CSP: productionCsp() },
  },
});
