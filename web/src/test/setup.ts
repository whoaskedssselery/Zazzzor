import { cleanup } from "@testing-library/react";
import { server } from "./msw";

// jsdom keeps Node's fetch, whose Request rejects relative URLs; a browser
// resolves them against the page. Do the same, so `api` keeps its real
// same-origin baseUrl "/api/v1" in tests (it reads Request when created).
const NodeRequest = globalThis.Request;
globalThis.Request = class extends NodeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === "string" ? new URL(input, window.location.href) : input, init);
  }
};

// jsdom has no layout; the router resets the scroll position on navigation.
window.scrollTo = () => {};

// Any request without a handler fails the test instead of reaching the network.
beforeAll(() => {
  server.listen({ onUnhandledFrame: "error" });
});
afterEach(() => {
  cleanup();
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});
