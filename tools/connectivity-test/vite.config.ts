import { defineConfig } from "vitest/config";

// Dev-only: `vite dev` / `vite preview` dial LiveKit at their own host, so
// forward signaling to a local `livekit-server --dev` (in production Caddy does this).
const proxy = { "/rtc": { target: "http://127.0.0.1:7880", ws: true } };
// The e2e smoke passes the CSP from deploy/caddy/Caddyfile to check the bundle under it.
const csp = process.env.ZAZZZOR_PREVIEW_CSP;

export default defineConfig({
  base: "/connectivity-test/",
  // livekit-client alone is ~540 kB; a single-page tool, no splitting needed.
  build: { chunkSizeWarningLimit: 700 },
  server: { proxy },
  preview: { proxy, headers: csp ? { "Content-Security-Policy": csp } : {} },
  test: { globals: true, include: ["src/**/*.test.ts"] },
});
