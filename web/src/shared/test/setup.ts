import "./environment";
import { cleanup, configure } from "@testing-library/react";
import { i18n } from "@/shared/i18n";
import { server } from "./msw";

// The tests read the interface in Russian, its source language, whatever the
// machine's own (jsdom says en-US); a test that switches it is undone after.
void i18n.changeLanguage("ru");

// The flow tests render the whole app; with every file running in parallel a
// render can take longer than Testing Library's default second to show.
configure({ asyncUtilTimeout: 4000 });

// Any request without a handler fails the test instead of reaching the network.
beforeAll(() => {
  server.listen({ onUnhandledFrame: "error" });
});
afterEach(() => {
  cleanup();
  if (i18n.language !== "ru") void i18n.changeLanguage("ru");
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});
