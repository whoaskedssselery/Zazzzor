import { DEFAULT_NOTIFY_PREFS, NOTIFY_PREFS_KEY, readNotifyPrefs, setNotifyPrefs } from "./prefs";

describe("notification settings", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("start with everything on and fall back to that from spoiled storage", () => {
    expect(readNotifyPrefs()).toEqual(DEFAULT_NOTIFY_PREFS);
    localStorage.setItem(NOTIFY_PREFS_KEY, "{");
    expect(readNotifyPrefs()).toEqual(DEFAULT_NOTIFY_PREFS);
    localStorage.setItem(NOTIFY_PREFS_KEY, JSON.stringify({ sound: "yes", desktop: false }));
    expect(readNotifyPrefs()).toEqual({ ...DEFAULT_NOTIFY_PREFS, desktop: false });
  });

  it("are kept in this browser", () => {
    setNotifyPrefs({ sound: false });
    expect(readNotifyPrefs().sound).toBe(false);
    setNotifyPrefs({ sound: true });
  });
});
