import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { noiseSuppressionAudioWorkletVitePlugin } from "@workadventure/noise-suppression/vite";
import { defineConfig } from "vitest/config";

// Dev only: the browser talks to the Vite origin and Vite forwards the REST
// API and the gateway WebSocket to a local `go run ./cmd/zazzzor` (in
// production Caddy serves both on one origin). The API must allow the origin
// http://localhost:5173, hence the fixed port. The browser tests point the
// proxy at their own API (e2e/compose.yml) and serve the production CSP from
// deploy/caddy/static.caddy, so the bundle is checked under it.
const proxy = {
  "/api": { target: process.env.ZAZZZOR_API_TARGET ?? "http://127.0.0.1:8080", ws: true },
  // LiveKit signaling: the API hands out this origin's /rtc, as Caddy serves it in production.
  "/rtc": { target: process.env.ZAZZZOR_LIVEKIT_TARGET ?? "http://127.0.0.1:7880", ws: true },
};
const csp = process.env.ZAZZZOR_PREVIEW_CSP;

export default defineConfig({
  // The noise suppression plugin serves its worklet, Wasm and model in dev; a build emits them to /assets.
  plugins: [react(), tailwindcss(), noiseSuppressionAudioWorkletVitePlugin()],
  // `@/` is web/src, as in tsconfig.json "paths".
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: { port: 5173, strictPort: true, proxy },
  preview: { proxy, headers: csp ? { "Content-Security-Policy": csp } : {} },
  // livekit-client alone is ~540 kB: a chunk of its own that loads with the first voice call.
  build: { chunkSizeWarningLimit: 600 },
  test: {
    globals: true,
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["src/shared/test/setup.ts"],
  },
});
