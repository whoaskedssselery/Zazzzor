import { signalUrl } from "./signal-url";

test("https page uses secure websocket", () => {
  expect(signalUrl({ protocol: "https:", host: "z.duckdns.org" })).toBe("wss://z.duckdns.org");
});
test("http page uses plain websocket", () => {
  expect(signalUrl({ protocol: "http:", host: "localhost:5173" })).toBe("ws://localhost:5173");
});
test("keeps a custom port", () => {
  expect(signalUrl({ protocol: "https:", host: "z.duckdns.org:8443" })).toBe("wss://z.duckdns.org:8443");
});
