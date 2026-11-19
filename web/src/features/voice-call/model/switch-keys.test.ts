import { comboName, comboOf, startSwitchKeys } from "./switch-keys";

const press = (code: string, init: KeyboardEventInit = {}, target: EventTarget = window) => {
  const e = new KeyboardEvent("keydown", { code, bubbles: true, cancelable: true, ...init });
  target.dispatchEvent(e);
  return e;
};

const KEYS = { mute: "Control+Shift+KeyM", deafen: "F9" };

describe("switch keys", () => {
  it("names a combination as it is saved and as it is shown", () => {
    expect(comboOf(new KeyboardEvent("keydown", { code: "KeyM", ctrlKey: true, shiftKey: true }))).toBe(
      "Control+Shift+KeyM",
    );
    expect(comboOf(new KeyboardEvent("keydown", { code: "F9" }))).toBe("F9");
    expect(comboOf(new KeyboardEvent("keydown", { code: "ShiftLeft", shiftKey: true }))).toBeNull();
    expect(comboOf(new KeyboardEvent("keydown", { code: "KeyM", metaKey: true }))).toBeNull();
    expect(comboName("Control+Shift+KeyM")).toBe("Ctrl + Shift + M");
    expect(comboName("Alt+Digit1")).toBe("Alt + 1");
  });

  it("flips the switch of the combination pressed, once per press", () => {
    const flipped: string[] = [];
    const stop = startSwitchKeys(KEYS, { mute: () => flipped.push("mute"), deafen: () => flipped.push("deafen") });
    const e = press("KeyM", { ctrlKey: true, shiftKey: true });
    press("KeyM", { ctrlKey: true, shiftKey: true, repeat: true });
    press("KeyM", { ctrlKey: true });
    press("F9");
    stop();
    press("F9");
    expect(flipped).toEqual(["mute", "deafen"]);
    expect(e.defaultPrevented).toBe(true);
  });

  it("leaves a key without modifiers alone while the user types", () => {
    const flipped: string[] = [];
    const stop = startSwitchKeys(KEYS, { mute: () => flipped.push("mute"), deafen: () => flipped.push("deafen") });
    const field = document.createElement("input");
    document.body.append(field);
    press("F9", {}, field);
    press("KeyM", { ctrlKey: true, shiftKey: true }, field);
    stop();
    field.remove();
    expect(flipped).toEqual(["mute"]);
  });
});

describe("switch keys in the desktop app", () => {
  let asked: unknown[];

  beforeEach(() => {
    asked = [];
    Object.assign(globalThis, {
      __TAURI_INTERNALS__: {
        invoke: (cmd: string, args?: unknown) => {
          if (cmd === "hotkeys_toggles") asked.push(args);
          return Promise.resolve(null);
        },
      },
    });
  });

  afterEach(() => {
    Reflect.deleteProperty(globalThis, "__TAURI_INTERNALS__");
    vi.restoreAllMocks();
  });

  const fromApp = (which: string) => {
    window.dispatchEvent(new CustomEvent("zazzzor:hotkey", { detail: which }));
  };

  it("asks the app to watch the combinations, and to stop", () => {
    const stop = startSwitchKeys(KEYS, { mute: () => undefined, deafen: () => undefined });
    stop();
    expect(asked).toEqual([KEYS, { mute: null, deafen: null }]);
  });

  it("flips on the app's report while the window is behind, not while it is in front", () => {
    const focus = vi.spyOn(document, "hasFocus").mockReturnValue(false);
    const flipped: string[] = [];
    const stop = startSwitchKeys(KEYS, { mute: () => flipped.push("mute"), deafen: () => flipped.push("deafen") });
    fromApp("deafen");
    focus.mockReturnValue(true);
    fromApp("mute");
    fromApp("anything");
    stop();
    expect(flipped).toEqual(["deafen"]);
  });
});
