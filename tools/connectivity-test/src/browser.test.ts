import { detectBrowser } from "./browser";

const CHROME = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36";
const EDGE = `${CHROME} Edg/141.0.0.0`;
const FIREFOX = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:143.0) Gecko/20100101 Firefox/143.0";

test("chrome is reliable", () => expect(detectBrowser(CHROME)).toEqual({ name: "chrome", reliable60fps: true }));
test("edge is detected before chrome", () => expect(detectBrowser(EDGE)).toEqual({ name: "edge", reliable60fps: true }));
test("firefox is not reliable for 60 fps", () => expect(detectBrowser(FIREFOX)).toEqual({ name: "firefox", reliable60fps: false }));
test("unknown is other", () => expect(detectBrowser("curl/8")).toEqual({ name: "other", reliable60fps: false }));
