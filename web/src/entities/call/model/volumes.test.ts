import { getVolumes, reloadVolumes, setVolume, VOLUMES_KEY } from "./volumes";

afterEach(() => {
  localStorage.clear();
  reloadVolumes();
});

describe("volumes", () => {
  it("are 100 % for everyone until set, and keep what is set across a reload", () => {
    expect(getVolumes()).toEqual({ voice: {}, stream: {} });
    setVolume("voice", "7", 150);
    setVolume("stream", "7", 40);
    reloadVolumes();
    expect(getVolumes()).toEqual({ voice: { "7": 150 }, stream: { "7": 40 } });
  });

  it("stay within 0–200 %", () => {
    setVolume("voice", "7", 999);
    setVolume("voice", "8", -5);
    expect(getVolumes().voice).toEqual({ "7": 200, "8": 0 });
  });

  it("forget a volume set back to 100 %", () => {
    setVolume("voice", "7", 150);
    setVolume("voice", "7", 100);
    expect(getVolumes().voice).toEqual({});
    expect(JSON.parse(localStorage.getItem(VOLUMES_KEY) ?? "null")).toEqual({ voice: {}, stream: {} });
  });

  it("ignore garbage in storage", () => {
    localStorage.setItem(VOLUMES_KEY, JSON.stringify({ voice: { "7": "loud", "8": 300, "9": 100 }, stream: 5 }));
    reloadVolumes();
    expect(getVolumes()).toEqual({ voice: { "8": 200 }, stream: {} });
  });
});
