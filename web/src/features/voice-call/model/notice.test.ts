import { ECHO_RISK, FIREFOX_NO_AUDIO, shareNotice, WINDOW_NO_AUDIO } from "./notice";

describe("shareNotice", () => {
  it("tells Firefox users their share has no sound, whatever they picked", () => {
    expect(shareNotice({ ok: true, surface: "browser", audio: false }, "firefox", false)).toBe(FIREFOX_NO_AUDIO);
    expect(shareNotice({ ok: true, surface: "monitor", audio: false }, "firefox", false)).toBe(FIREFOX_NO_AUDIO);
  });

  it("points a window without sound to the app for Windows or the whole screen", () => {
    expect(shareNotice({ ok: true, surface: "window", audio: false }, "chromium", true)).toBe(WINDOW_NO_AUDIO);
  });

  it("warns of echo when the whole screen's sound cannot leave the voice chat out", () => {
    expect(shareNotice({ ok: true, surface: "monitor", audio: true }, "chromium", false)).toBe(ECHO_RISK);
    expect(shareNotice({ ok: true, surface: "monitor", audio: true }, "chromium", true)).toBeNull();
  });

  it("says nothing for a tab with its sound, or a cancelled picker", () => {
    expect(shareNotice({ ok: true, surface: "browser", audio: true }, "chromium", false)).toBeNull();
    expect(shareNotice({ ok: false, reason: "cancelled" }, "chromium", true)).toBeNull();
    expect(shareNotice({ ok: false, reason: "cancelled" }, "firefox", false)).toBeNull();
  });
});
