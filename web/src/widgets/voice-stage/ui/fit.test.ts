import { fitTileWidth } from "./VoiceStage";

/** Whether `count` tiles of `tile` px fit `width` × `height` without wrapping or scrolling, at the best column count. */
const fits = (count: number, tile: number, width: number, height: number): boolean => {
  for (let cols = 1; cols <= count; cols++) {
    const rows = Math.ceil(count / cols);
    if (width >= height && rows > cols) continue;
    if (cols * tile + 12 * (cols - 1) <= width && rows * ((tile * 9) / 16) + 12 * (rows - 1) <= height) return true;
  }
  return false;
};

describe("fitTileWidth", () => {
  it("gives one tile the whole height on a low, wide stage, less a little room for rounding", () => {
    // 322px high: a 16:9 tile 572px wide.
    expect(fitTileWidth(1, 900, 324)).toBe(572);
  });

  it("puts two tiles side by side on a wide stage", () => {
    // Side by side: (900 - 2 - 12) / 2 = 443 wide.
    expect(fitTileWidth(2, 900, 500)).toBe(443);
    // Stacked would be larger here, (560 - 2 - 12) / 2 * 16/9 = 485, but two people are a row, as in Discord.
    expect(fitTileWidth(2, 936, 560)).toBe(461);
  });

  it("lays out more tiles in a wider grid than tall", () => {
    // 3 tiles: 2 + 1 in two rows, not 3 stacked; 5 tiles: 3 + 2.
    expect(fitTileWidth(3, 936, 560)).toBe(461);
    expect(fitTileWidth(5, 936, 560)).toBe(303);
  });

  it("stacks two tiles on a tall, narrow stage", () => {
    expect(fitTileWidth(2, 360, 700)).toBe(358);
  });

  it("fits any number of tiles without a scroll until they would be unreadable", () => {
    for (const [w, h] of [
      [813, 517],
      [1100, 640],
      [640, 900],
      [1600, 820],
    ] as const) {
      for (let count = 1; count <= 16; count++) {
        const tile = fitTileWidth(count, w, h);
        if (tile > 176)
          expect(fits(count, tile, w, h), `${String(count)} tiles in ${String(w)}×${String(h)}`).toBe(true);
      }
    }
  });

  it("stops shrinking at a readable size, and the grid scrolls", () => {
    expect(fitTileWidth(12, 400, 200)).toBe(176);
  });
});
