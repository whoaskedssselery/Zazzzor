import { defineConfig } from "vitest/config";

export default defineConfig({
  base: "/connectivity-test/",
  // livekit-client alone is ~540 kB; a single-page tool, no splitting needed.
  build: { chunkSizeWarningLimit: 700 },
  test: { globals: true },
});
