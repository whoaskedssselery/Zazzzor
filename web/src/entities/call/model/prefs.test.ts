import { DEFAULT_VOICE_PREFS, readVoicePrefs, VOICE_PREFS_KEY, writeVoicePrefs } from "./prefs";

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("voice prefs", () => {
  it("default to the system devices, voice activity on V, enhanced noise suppression, no switch keys", () => {
    expect(readVoicePrefs()).toEqual({
      inputId: null,
      outputId: null,
      mode: "voice",
      pttKey: "KeyV",
      muteKey: null,
      deafenKey: null,
      noise: "enhanced",
    });
    expect(DEFAULT_VOICE_PREFS.pttKey).toBe("KeyV");
  });

  it("are written and read back", () => {
    const prefs = {
      inputId: "mic-2",
      outputId: "out-1",
      mode: "ptt",
      pttKey: "KeyB",
      muteKey: "Control+Shift+KeyM",
      deafenKey: null,
      noise: "off",
    } as const;
    writeVoicePrefs(prefs);
    expect(readVoicePrefs()).toEqual(prefs);
    writeVoicePrefs({ ...prefs, noise: "standard" });
    expect(readVoicePrefs().noise).toBe("standard");
  });

  it("fall back to the defaults field by field for garbage", () => {
    localStorage.setItem(VOICE_PREFS_KEY, JSON.stringify({ inputId: 5, mode: "loud", pttKey: "KeyB", muteKey: 7 }));
    expect(readVoicePrefs()).toEqual({ ...DEFAULT_VOICE_PREFS, pttKey: "KeyB" });
    localStorage.setItem(VOICE_PREFS_KEY, "{not json");
    expect(readVoicePrefs()).toEqual(DEFAULT_VOICE_PREFS);
  });

  it("survive blocked storage", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    expect(readVoicePrefs()).toEqual(DEFAULT_VOICE_PREFS);
    expect(() => {
      writeVoicePrefs(DEFAULT_VOICE_PREFS);
    }).not.toThrow();
  });
});
