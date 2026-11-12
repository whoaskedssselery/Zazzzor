import "./environment";
import { cleanup, configure } from "@testing-library/react";
import { server } from "./msw";

// The flow tests render the whole app; with every file running in parallel a
// render can take longer than Testing Library's default second to show.
configure({ asyncUtilTimeout: 4000 });

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
