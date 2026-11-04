import { fitTileWidth } from "./VoiceStage";

describe("fitTileWidth", () => {
  it("gives one tile the whole height on a low, wide stage", () => {
    // 324px high: a 16:9 tile 576px wide.
    expect(fitTileWidth(1, 900, 324)).toBe(576);
  });

  it("puts two tiles side by side when that makes them larger", () => {
    // Side by side: (900 - 12) / 2 = 444 wide; stacked: (500 - 12) / 2 * 16/9 ≈ 433.
    expect(fitTileWidth(2, 900, 500)).toBe(444);
  });

  it("stacks two tiles on a tall, narrow stage", () => {
    expect(fitTileWidth(2, 360, 700)).toBe(360);
  });

  it("stops shrinking at a readable size, and the grid scrolls", () => {
    expect(fitTileWidth(12, 400, 200)).toBe(176);
  });
});
