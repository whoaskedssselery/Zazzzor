import { playSound, resetSounds } from "./sounds";

/** A browser audio context, as far as a sound touches one, counting the notes started. */
const started: number[] = [];
const node = () => ({
  connect: vi.fn(function connect(this: unknown, next: unknown) {
    return next;
  }),
  frequency: { setValueAtTime: vi.fn(), exponentialRampToValueAtTime: vi.fn() },
  gain: { setValueAtTime: vi.fn(), linearRampToValueAtTime: vi.fn(), exponentialRampToValueAtTime: vi.fn() },
  start: vi.fn((at: number) => started.push(at)),
  stop: vi.fn(),
});

beforeEach(() => {
  started.length = 0;
  resetSounds();
  vi.stubGlobal(
    "AudioContext",
    vi.fn(function AudioContext() {
      return { state: "running", currentTime: 0, destination: {}, createOscillator: node, createGain: node };
    }),
  );
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("playSound", () => {
  it("plays a sound's notes one after another", () => {
    playSound("join");
    expect(started).toHaveLength(3);
    expect(started).toEqual([...started].sort((a, b) => a - b));
  });

  it("plays a burst of the same sound once, and different sounds each", () => {
    playSound("userJoin");
    playSound("userJoin");
    playSound("userLeave");
    expect(started).toHaveLength(2);
  });

  it("never throws where there is no audio", () => {
    vi.stubGlobal("AudioContext", undefined);
    expect(() => {
      playSound("mute");
    }).not.toThrow();
  });
});
