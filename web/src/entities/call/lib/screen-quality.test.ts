import { INITIAL_QUALITY, nextQuality, SCALE_720P, type ScreenQuality, type ScreenSample } from "./screen-quality";

const run = (q: ScreenQuality, samples: ScreenSample[]) => samples.reduce(nextQuality, q);
const starved: ScreenSample = { sourceFps: 50, sentFps: 4, bandwidth: 2_000_000 };
const smooth: ScreenSample = { sourceFps: 50, sentFps: 48, bandwidth: 2_000_000 };
const roomy: ScreenSample = { sourceFps: 50, sentFps: 30, bandwidth: 9_000_000 };
const still: ScreenSample = { sourceFps: 2, sentFps: 2, bandwidth: 1_000_000 };

describe("nextQuality", () => {
  it("steps down to 720p after two starved samples of a moving screen in a row", () => {
    expect(run(INITIAL_QUALITY, [starved]).scale).toBe(1);
    expect(run(INITIAL_QUALITY, [starved, smooth, starved]).scale).toBe(1);
    expect(run(INITIAL_QUALITY, [starved, starved]).scale).toBe(SCALE_720P);
  });

  it("holds 1080p at 30 fps and more, steps down below 30", () => {
    const at = (sentFps: number): ScreenSample => ({ sourceFps: 60, sentFps, bandwidth: 5_000_000 });
    expect(run(INITIAL_QUALITY, [at(30), at(30), at(30)]).scale).toBe(1);
    expect(run(INITIAL_QUALITY, [at(29), at(29)]).scale).toBe(SCALE_720P);
  });

  it("stays at 1080p while the screen is still: few frames there are not starvation", () => {
    expect(run(INITIAL_QUALITY, [still, still, still]).scale).toBe(1);
  });

  it("steps back up once the link has room for 1080p for ten seconds", () => {
    const down = run(INITIAL_QUALITY, [starved, starved]);
    expect(run(down, [roomy, roomy, roomy, roomy]).scale).toBe(SCALE_720P);
    expect(run(down, [roomy, roomy, smooth, roomy, roomy, roomy, roomy]).scale).toBe(SCALE_720P);
    expect(run(down, [roomy, roomy, roomy, roomy, roomy]).scale).toBe(1);
  });

  it("waits longer to step up after each fall, so a flaky link does not flap", () => {
    const up = run(INITIAL_QUALITY, [starved, starved, ...Array<ScreenSample>(5).fill(roomy)]);
    const second = run(up, [starved, starved]);
    expect(second.scale).toBe(SCALE_720P);
    expect(run(second, Array<ScreenSample>(9).fill(roomy)).scale).toBe(SCALE_720P);
    expect(run(second, Array<ScreenSample>(10).fill(roomy)).scale).toBe(1);
  });

  it("does not step up without a bandwidth estimate", () => {
    const down = run(INITIAL_QUALITY, [starved, starved]);
    expect(run(down, Array<ScreenSample>(40).fill({ ...roomy, bandwidth: null })).scale).toBe(SCALE_720P);
  });
});
