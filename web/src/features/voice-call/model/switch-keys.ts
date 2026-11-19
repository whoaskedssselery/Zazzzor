import { desktopInvoke, inDesktopApp } from "@/shared/lib";
import { typing } from "./push-to-talk";

/** The switches a key combination flips. */
export type Switch = "mute" | "deafen";

export type SwitchKeys = Record<Switch, string | null>;

/** What the desktop app sends when a combination is pressed anywhere. */
const APP_EVENT = "zazzzor:hotkey";

const MODIFIER_CODES = new Set(["ControlLeft", "ControlRight", "ShiftLeft", "ShiftRight", "AltLeft", "AltRight"]);

/**
 * The combination a key press makes, as it is saved and as the desktop app
 * takes it: the modifiers held ("Control", "Shift", "Alt") and the key's
 * KeyboardEvent.code, joined with "+". A modifier pressed alone, or with
 * the Windows key, makes none.
 */
export const comboOf = (e: KeyboardEvent): string | null => {
  if (MODIFIER_CODES.has(e.code) || e.metaKey || e.code === "") return null;
  const parts = [e.ctrlKey && "Control", e.shiftKey && "Shift", e.altKey && "Alt"].filter((p) => p !== false);
  return [...parts, e.code].join("+");
};

const NAMES: Record<string, string> = { Control: "Ctrl", Shift: "Shift", Alt: "Alt" };

/** «Control+Shift+KeyM» → «Ctrl + Shift + M», for the settings. */
export const comboName = (combo: string): string =>
  combo
    .split("+")
    .map((part) => NAMES[part] ?? part.replace(/^Key/, "").replace(/^Digit/, ""))
    .join(" + ");

/**
 * Calls `flip.mute` or `flip.deafen` when their combination is pressed. A
 * combination without a modifier does not count in a text field, so typing
 * flips nothing. In the desktop app the combinations work in every window:
 * the app watches them and reports a press while Zazzzor is behind. Returns
 * the function that stops listening.
 */
export const startSwitchKeys = (keys: SwitchKeys, flip: Record<Switch, () => void>): (() => void) => {
  const onKey = (e: KeyboardEvent) => {
    if (e.repeat) return;
    const combo = comboOf(e);
    if (combo === null) return;
    const which = (["mute", "deafen"] as const).find((s) => keys[s] === combo);
    if (!which) return;
    if (!combo.includes("+") && typing(e.target)) return;
    e.preventDefault();
    flip[which]();
  };
  const fromApp = (e: Event) => {
    // In front, the page's own listener has seen the press.
    if (document.hasFocus()) return;
    const which = (e as CustomEvent<unknown>).detail;
    if (which === "mute" || which === "deafen") flip[which]();
  };
  window.addEventListener("keydown", onKey);
  if (inDesktopApp()) {
    window.addEventListener(APP_EVENT, fromApp);
    // An older app, or a combination it cannot watch: the keys work while Zazzzor is in front.
    desktopInvoke("hotkeys_toggles", { ...keys }).catch(() => undefined);
  }
  return () => {
    window.removeEventListener("keydown", onKey);
    if (inDesktopApp()) {
      window.removeEventListener(APP_EVENT, fromApp);
      desktopInvoke("hotkeys_toggles", { mute: null, deafen: null }).catch(() => undefined);
    }
  };
};
