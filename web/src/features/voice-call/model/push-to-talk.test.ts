import { startPushToTalk } from "./push-to-talk";

const press = (type: "keydown" | "keyup", code: string, init: KeyboardEventInit = {}, target: EventTarget = window) => {
  target.dispatchEvent(new KeyboardEvent(type, { code, bubbles: true, ...init }));
};

describe("push-to-talk", () => {
  it("holds while the key is down", () => {
    const seen: boolean[] = [];
    const stop = startPushToTalk("KeyV", (held) => seen.push(held));
    press("keydown", "KeyV");
    press("keyup", "KeyV");
    press("keydown", "KeyB");
    stop();
    press("keydown", "KeyV");
    expect(seen).toEqual([true, false]);
  });

  it("lets go when the window loses focus with the key down", () => {
    const seen: boolean[] = [];
    const stop = startPushToTalk("KeyV", (held) => seen.push(held));
    press("keydown", "KeyV");
    window.dispatchEvent(new Event("blur"));
    stop();
    expect(seen).toEqual([true, false]);
  });

  it("lets go when the tab is hidden with the key down (the key comes up in another tab)", () => {
    const seen: boolean[] = [];
    const stop = startPushToTalk("KeyV", (held) => seen.push(held));
    press("keydown", "KeyV");
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    document.dispatchEvent(new Event("visibilitychange"));
    Reflect.deleteProperty(document, "hidden");
    press("keydown", "KeyV");
    window.dispatchEvent(new Event("pagehide"));
    stop();
    expect(seen).toEqual([true, false, true, false]);
  });

  it("does not repeat while the key is held", () => {
    const seen: boolean[] = [];
    const stop = startPushToTalk("KeyV", (held) => seen.push(held));
    press("keydown", "KeyV");
    press("keydown", "KeyV", { repeat: true });
    press("keydown", "KeyV");
    stop();
    // One press, then stopping lets go of the microphone.
    expect(seen).toEqual([true, false]);
  });

  it("does not talk while the user types", () => {
    const seen: boolean[] = [];
    const input = document.createElement("textarea");
    document.body.append(input);
    const stop = startPushToTalk("KeyV", (held) => seen.push(held));
    press("keydown", "KeyV", {}, input);
    stop();
    input.remove();
    expect(seen).toEqual([]);
  });
});

describe("push-to-talk in the desktop app", () => {
  let asked: unknown[];
  let refuse: boolean;

  beforeEach(() => {
    asked = [];
    refuse = false;
    Object.assign(globalThis, {
      __TAURI_INTERNALS__: {
        invoke: (cmd: string, args?: { code: string | null }) => {
          if (cmd === "hotkeys_ptt") asked.push(args?.code);
          return refuse ? Promise.reject(new Error("no global key")) : Promise.resolve(null);
        },
      },
    });
  });

  afterEach(() => {
    Reflect.deleteProperty(globalThis, "__TAURI_INTERNALS__");
    vi.restoreAllMocks();
  });

  /** The app reports the key going down or up anywhere in Windows. */
  const fromApp = (down: boolean) => {
    window.dispatchEvent(new CustomEvent("zazzzor:ptt", { detail: down }));
  };
  const behind = () => vi.spyOn(document, "hasFocus").mockReturnValue(false);
  /** Lets the answer to the app request arrive. */
  const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

  it("asks the app to watch the key, and to stop", async () => {
    const stop = startPushToTalk("KeyV", () => undefined);
    await settle();
    stop();
    expect(asked).toEqual(["KeyV", null]);
  });

  it("talks while the key is held in another program", async () => {
    behind();
    const seen: boolean[] = [];
    const stop = startPushToTalk("KeyV", (held) => seen.push(held));
    await settle();
    fromApp(true);
    fromApp(false);
    stop();
    expect(seen).toEqual([true, false]);
  });

  it("keeps talking when the window loses focus with the key down", async () => {
    const seen: boolean[] = [];
    const stop = startPushToTalk("KeyV", (held) => seen.push(held));
    await settle();
    press("keydown", "KeyV");
    behind();
    window.dispatchEvent(new Event("blur"));
    expect(seen).toEqual([true]);
    fromApp(false);
    expect(seen).toEqual([true, false]);
    stop();
  });

  it("leaves the window's own keys to the page while it is in front", async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    const seen: boolean[] = [];
    const stop = startPushToTalk("KeyV", (held) => seen.push(held));
    await settle();
    const field = document.createElement("textarea");
    document.body.append(field);
    press("keydown", "KeyV", {}, field);
    fromApp(true);
    stop();
    field.remove();
    expect(seen).toEqual([]);
  });

  it("lets go on blur as in a browser when the app cannot watch the key", async () => {
    refuse = true;
    const seen: boolean[] = [];
    const stop = startPushToTalk("KeyV", (held) => seen.push(held));
    await settle();
    press("keydown", "KeyV");
    window.dispatchEvent(new Event("blur"));
    stop();
    expect(seen).toEqual([true, false]);
  });
});
