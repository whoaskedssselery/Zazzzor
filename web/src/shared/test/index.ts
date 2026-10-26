// Test-only helpers; lint keeps them out of app code (see eslint.config.js).
// setup.ts is not exported: Vitest loads it by path (vite.config.ts).
export * from "./msw";
export * from "./render";
