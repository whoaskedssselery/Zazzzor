import { desktopInvoke, inDesktopApp } from "@/shared/lib";

/** Whether a key press went into something the user types in. */
const typing = (target: EventTarget | null): boolean =>
  target instanceof HTMLElement &&
  (target.isContentEditable || target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement);

/** What the desktop app sends when the push-to-talk key goes down (true) or up (false) anywhere. */
const APP_EVENT = "zazzzor:ptt";

/**
 * Calls `onChange(true)` when the key `code` (a KeyboardEvent.code) goes
 * down and `onChange(false)` when it comes up. Key presses in text fields do
 * not count. Returns the function that stops listening.
 *
 * A browser sees no keys of other windows, so push-to-talk works only while
 * Zazzzor is in front: losing focus, hiding the tab or leaving the page lets
 * go (the key then comes up elsewhere). The desktop app watches the key
 * everywhere and reports it while the window is behind; it is held across
 * the focus moving then, and comes up when the app says so.
 */
export const startPushToTalk = (code: string, onChange: (held: boolean) => void): (() => void) => {
  let held = false;
  /** The desktop app watches the key outside the window. */
  let global = false;
  let stopped = false;
  const set = (next: boolean) => {
    if (next === held) return;
    held = next;
    onChange(next);
  };
  const down = (e: KeyboardEvent) => {
    if (e.code === code && !e.repeat && !typing(e.target)) set(true);
  };
  const up = (e: KeyboardEvent) => {
    if (e.code === code) set(false);
  };
  const release = () => {
    set(false);
  };
  const blurred = () => {
    if (!global) set(false);
  };
  const hidden = () => {
    if (document.hidden && !global) set(false);
  };
  const fromApp = (e: Event) => {
    const pressed = (e as CustomEvent<unknown>).detail === true;
    // In front, the page's own listeners count, and they leave typing alone.
    if (pressed && document.hasFocus()) return;
    set(pressed);
  };
  window.addEventListener("keydown", down);
  window.addEventListener("keyup", up);
  window.addEventListener("blur", blurred);
  window.addEventListener("pagehide", release);
  document.addEventListener("visibilitychange", hidden);
  if (inDesktopApp()) {
    window.addEventListener(APP_EVENT, fromApp);
    // An older app, or a key it cannot watch: push-to-talk stays as in a browser.
    desktopInvoke("hotkeys_ptt", { code }).then(
      () => {
        if (!stopped) global = true;
      },
      () => undefined,
    );
  }
  return () => {
    stopped = true;
    window.removeEventListener("keydown", down);
    window.removeEventListener("keyup", up);
    window.removeEventListener("blur", blurred);
    window.removeEventListener("pagehide", release);
    document.removeEventListener("visibilitychange", hidden);
    if (inDesktopApp()) {
      window.removeEventListener(APP_EVENT, fromApp);
      desktopInvoke("hotkeys_ptt", { code: null }).catch(() => undefined);
    }
    set(false);
  };
};
