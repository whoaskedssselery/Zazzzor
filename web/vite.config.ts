import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// Dev only: the browser talks to the Vite origin and Vite forwards the REST
// API and the gateway WebSocket to a local `go run ./cmd/zazzzor` (in
// production Caddy serves both on one origin). The API must allow the origin
// http://localhost:5173, hence the fixed port.
const proxy = { "/api": { target: "http://127.0.0.1:8080", ws: true } };

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { port: 5173, strictPort: true, proxy },
  preview: { proxy },
  test: {
    globals: true,
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["src/test/setup.ts"],
  },
});
