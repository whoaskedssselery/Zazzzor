import { defineConfig } from "vitest/config";

export default defineConfig({
  base: "/connectivity-test/",
  // livekit-client alone is ~540 kB; a single-page tool, no splitting needed.
  build: { chunkSizeWarningLimit: 700 },
  // `vite dev` only: the page dials LiveKit at its own host, so forward
  // signaling to a local `livekit-server --dev` (in production Caddy does this).
  server: {
    proxy: {
      "/rtc": { target: "http://127.0.0.1:7880", ws: true },
      "/twirp": { target: "http://127.0.0.1:7880" },
    },
  },
  test: { globals: true },
});
