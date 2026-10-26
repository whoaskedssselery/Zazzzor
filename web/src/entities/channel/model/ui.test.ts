import { resetChannelUi, useChannelUi } from "./ui";

const KEY = "zazzzor.ui";

afterEach(() => {
  resetChannelUi();
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("channel ui store", () => {
  it("remembers the last channel of each server", () => {
    useChannelUi.getState().setLastChannel("10", "101");
    useChannelUi.getState().setLastChannel("20", "201");
    useChannelUi.getState().setLastChannel("10", "102");
    expect(useChannelUi.getState().lastChannel).toEqual({ "10": "102", "20": "201" });
  });

  it("toggles a collapsed category", () => {
    useChannelUi.getState().toggleCategory("5");
    expect(useChannelUi.getState().collapsed["5"]).toBe(true);
    useChannelUi.getState().toggleCategory("5");
    expect(useChannelUi.getState().collapsed["5"]).toBeUndefined();
  });

  it("keeps the last channels and collapsed categories across reloads", async () => {
    useChannelUi.getState().setLastChannel("10", "101");
    useChannelUi.getState().toggleCategory("5");
    expect(JSON.parse(localStorage.getItem(KEY) ?? "null")).toEqual({
      lastChannel: { "10": "101" },
      collapsed: { "5": true },
    });

    vi.resetModules();
    const reloaded = await import("./ui");
    expect(reloaded.useChannelUi.getState()).toMatchObject({
      lastChannel: { "10": "101" },
      collapsed: { "5": true },
    });
  });

  it("starts empty when what was saved is not its own", async () => {
    localStorage.setItem(KEY, JSON.stringify({ lastChannel: { "10": 101 }, collapsed: "yes" }));
    vi.resetModules();
    const reloaded = await import("./ui");
    expect(reloaded.useChannelUi.getState()).toMatchObject({ lastChannel: {}, collapsed: {} });

    localStorage.setItem(KEY, "{not json");
    vi.resetModules();
    const again = await import("./ui");
    expect(again.useChannelUi.getState()).toMatchObject({ lastChannel: {}, collapsed: {} });
  });

  it("works on when the browser blocks storage", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    useChannelUi.getState().setLastChannel("10", "101");
    expect(useChannelUi.getState().lastChannel).toEqual({ "10": "101" });
  });

  it("resetChannelUi forgets everything, also what was saved", () => {
    useChannelUi.getState().setLastChannel("10", "101");
    useChannelUi.getState().toggleCategory("5");
    resetChannelUi();
    const { lastChannel, collapsed } = useChannelUi.getState();
    expect({ lastChannel, collapsed }).toEqual({ lastChannel: {}, collapsed: {} });
    expect(localStorage.getItem(KEY)).toBeNull();
  });
});
