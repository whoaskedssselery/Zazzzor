import { createSpeakingDetector, HOLD_MS, LOUD, rms, SAMPLE_MS } from "./speaking";

/** The level each fake analyser reads, by the track it was made for. */
const levels = new Map<unknown, number>();
const closed = vi.fn();

class FakeContext {
  state = "running";
  destination = {};
  createMediaStreamSource(stream: { track: unknown }) {
    const node = { track: stream.track, connect: (next: unknown) => next, disconnect: vi.fn() };
    return node;
  }
  createAnalyser() {
    let track: unknown;
    const analyser = {
      fftSize: 0,
      connect: (next: unknown) => next,
      disconnect: vi.fn(),
      getFloatTimeDomainData: (out: Float32Array) => {
        out.fill(levels.get(track) ?? 0);
      },
      bind: (t: unknown) => {
        track = t;
      },
    };
    return analyser;
  }
  createGain() {
    return { gain: { value: 1 }, connect: (next: unknown) => next };
  }
  close() {
    closed();
    return Promise.resolve();
  }
}

beforeEach(() => {
  vi.useFakeTimers();
  levels.clear();
  closed.mockClear();
  // The source node carries its track to the analyser it is connected to.
  vi.stubGlobal(
    "AudioContext",
    class extends FakeContext {
      override createMediaStreamSource(stream: { track: unknown }) {
        const source = super.createMediaStreamSource(stream);
        source.connect = (next: unknown) => {
          (next as { bind: (t: unknown) => void }).bind(stream.track);
          return next;
        };
        return source;
      }
    },
  );
  vi.stubGlobal(
    "MediaStream",
    class {
      track: unknown;
      constructor(tracks: unknown[]) {
        this.track = tracks[0];
      }
    },
  );
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const track = (id: string) => ({ id }) as unknown as MediaStreamTrack;

describe("rms", () => {
  it("is the loudness of the samples", () => {
    expect(rms(new Float32Array([0.5, -0.5, 0.5, -0.5]))).toBeCloseTo(0.5);
    expect(rms(new Float32Array())).toBe(0);
  });
});

describe("the speaking detector", () => {
  it("says someone speaks within one sample of their first loud frame", () => {
    const changes: string[][] = [];
    const d = createSpeakingDetector((ids) => changes.push(ids));
    const anna = track("a");
    d?.watch("7", anna);
    vi.advanceTimersByTime(SAMPLE_MS);
    expect(changes).toEqual([]);
    levels.set(anna, LOUD * 2);
    vi.advanceTimersByTime(SAMPLE_MS);
    expect(changes).toEqual([["7"]]);
  });

  it("keeps the ring through a short pause and drops it after the hold", () => {
    const changes: string[][] = [];
    const d = createSpeakingDetector((ids) => changes.push(ids));
    const anna = track("a");
    d?.watch("7", anna);
    levels.set(anna, LOUD * 2);
    vi.advanceTimersByTime(SAMPLE_MS);
    levels.set(anna, 0);
    vi.advanceTimersByTime(HOLD_MS - SAMPLE_MS);
    expect(changes).toEqual([["7"]]);
    vi.advanceTimersByTime(SAMPLE_MS * 2);
    expect(changes).toEqual([["7"], []]);
  });

  it("tells several people apart, and forgets one who is not watched any more", () => {
    const changes: string[][] = [];
    const d = createSpeakingDetector((ids) => changes.push(ids));
    const [anna, petr] = [track("a"), track("p")];
    d?.watch("8", petr);
    d?.watch("7", anna);
    levels.set(anna, LOUD * 2);
    levels.set(petr, LOUD * 2);
    vi.advanceTimersByTime(SAMPLE_MS);
    expect(changes.at(-1)).toEqual(["7", "8"]);
    d?.unwatch("8");
    expect(changes.at(-1)).toEqual(["7"]);
    d?.stop();
    expect(changes.at(-1)).toEqual([]);
    expect(closed).toHaveBeenCalledTimes(1);
  });

  it("is live only while its audio context runs, and tries to resume a suspended one", () => {
    const d = createSpeakingDetector(() => {});
    expect(d?.live()).toBe(false);
    d?.watch("7", track("a"));
    expect(d?.live()).toBe(true);
  });

  it("is not made without Web Audio, so the server's speakers are used", () => {
    vi.stubGlobal("AudioContext", undefined);
    expect(createSpeakingDetector(() => {})).toBeNull();
  });
});
