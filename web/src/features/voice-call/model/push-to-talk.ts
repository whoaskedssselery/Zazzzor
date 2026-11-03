/** Whether a key press went into something the user types in. */
const typing = (target: EventTarget | null): boolean =>
  target instanceof HTMLElement &&
  (target.isContentEditable || target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement);

/**
 * Calls `onChange(true)` when the key `code` (a KeyboardEvent.code) goes
 * down and `onChange(false)` when it comes up or the window loses focus with
 * it held: the browser sees no keys of other windows, so push-to-talk works
 * only while Zazzzor is in front. Key presses in text fields do not count.
 * Returns the function that stops listening.
 */
export const startPushToTalk = (code: string, onChange: (held: boolean) => void): (() => void) => {
  let held = false;
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
  const blur = () => {
    set(false);
  };
  window.addEventListener("keydown", down);
  window.addEventListener("keyup", up);
  window.addEventListener("blur", blur);
  return () => {
    window.removeEventListener("keydown", down);
    window.removeEventListener("keyup", up);
    window.removeEventListener("blur", blur);
    set(false);
  };
};
