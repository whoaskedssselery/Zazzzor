import { create } from "zustand";

/** How mentions reach the user on this device. */
export interface NotifyPrefs {
  /** A system notification while the window is in the background (needs the browser's permission). */
  desktop: boolean;
  /** A short sound. */
  sound: boolean;
  /** The prompt to allow notifications was closed: it does not come back. */
  promptDismissed: boolean;
}

export const NOTIFY_PREFS_KEY = "zazzzor.notify";

export const DEFAULT_NOTIFY_PREFS: NotifyPrefs = { desktop: true, sound: true, promptDismissed: false };

/** The settings of this browser; each field missing or not valid takes its default. */
export const readNotifyPrefs = (): NotifyPrefs => {
  let raw: unknown;
  try {
    raw = JSON.parse(localStorage.getItem(NOTIFY_PREFS_KEY) ?? "null");
  } catch {
    return DEFAULT_NOTIFY_PREFS;
  }
  if (typeof raw !== "object" || raw === null) return DEFAULT_NOTIFY_PREFS;
  const p = raw as Record<string, unknown>;
  const bool = (v: unknown, d: boolean) => (typeof v === "boolean" ? v : d);
  return {
    desktop: bool(p.desktop, DEFAULT_NOTIFY_PREFS.desktop),
    sound: bool(p.sound, DEFAULT_NOTIFY_PREFS.sound),
    promptDismissed: bool(p.promptDismissed, DEFAULT_NOTIFY_PREFS.promptDismissed),
  };
};

const store = create<NotifyPrefs>()(() => readNotifyPrefs());

/** The notification settings; the component re-renders when they change. */
export const useNotifyPrefs = (): NotifyPrefs => store();

/** The settings now, outside React. */
export const getNotifyPrefs = (): NotifyPrefs => store.getState();

/** Changes some settings and keeps them in this browser. */
export const setNotifyPrefs = (change: Partial<NotifyPrefs>): void => {
  store.setState(change);
  try {
    localStorage.setItem(NOTIFY_PREFS_KEY, JSON.stringify(store.getState()));
  } catch {
    // Storage blocked (private mode): the settings last for this tab only.
  }
};
