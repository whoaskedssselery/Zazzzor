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
