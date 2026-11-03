import type { AudioProcessorOptions } from "livekit-client";
import { Track } from "livekit-client";
import type { Mock } from "vitest";
import type { NoiseStatus } from "@/shared/lib";
import { browserNoiseSuppression, createNoiseProcessor, type NoiseEngines, type NoiseNode } from "./noise";

/** An audio node that remembers what it is connected to. */
class FakeNode {
  readonly targets = new Set<FakeNode>();
  connect(to: FakeNode) {
    this.targets.add(to);
    return to;
  }
  disconnect() {
    this.targets.clear();
  }
}

class FakeContext {
  static made: FakeContext[] = [];
  readonly out = Object.assign(new FakeNode(), {
    stream: { getAudioTracks: () => [processed], getTracks: () => [processed] },
  });
  readonly sources: (FakeNode & { stream: unknown })[] = [];
  closed = false;
  constructor() {
    FakeContext.made.push(this);
  }
  createMediaStreamSource(stream: unknown) {
    const s = Object.assign(new FakeNode(), { stream });
    this.sources.push(s);
    return s;
  }
  createMediaStreamDestination() {
    return this.out;
  }
  resume() {
    return Promise.resolve();
  }
  close() {
    this.closed = true;
    return Promise.resolve();
  }
  /** What the newest source reaches: the engine's node, or the output directly. */
  path(): FakeNode | undefined {
    return [...(this.sources.at(-1)?.targets ?? [])][0];
  }
}

const processed = { kind: "audio", stop: vi.fn() };

const microphone = (settings: MediaTrackSettings = {}) =>
  ({
    kind: "audio",
    getConstraints: () => ({ echoCancellation: true }),
    applyConstraints: vi.fn(() => Promise.resolve()),
    getSettings: () => settings,
  }) as unknown as MediaStreamTrack;

/** The spy behind a fake microphone's applyConstraints. */
const applied = (track: MediaStreamTrack) => (track as unknown as { applyConstraints: Mock }).applyConstraints;

const engine = () => {
  const node = new FakeNode();
  const handle: NoiseNode & { disposed: boolean } = {
    node: node as unknown as AudioNode,
    disposed: false,
    dispose() {
      this.disposed = true;
      node.disconnect();
    },
  };
  return handle;
};

const opts = (track: MediaStreamTrack): AudioProcessorOptions => ({
  kind: Track.Kind.Audio,
  track,
  audioContext: {} as AudioContext,
});

beforeEach(() => {
  FakeContext.made = [];
  vi.stubGlobal(
    "MediaStream",
    class {
      constructor(readonly tracks: MediaStreamTrack[]) {}
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const setup = (engines: Partial<NoiseEngines>) => {
  const statuses: NoiseStatus[] = [];
  const memory = { overloaded: false };
  const processor = createNoiseProcessor({
    engines: {
      deepFilterNet: () => Promise.reject(new Error("no DeepFilterNet")),
      rnnoise: () => Promise.reject(new Error("no RNNoise")),
      ...engines,
    },
    report: (s) => statuses.push(s),
    memory,
    makeContext: () => new FakeContext() as unknown as AudioContext,
  });
  return { processor, statuses, memory, ctx: () => FakeContext.made.at(-1) as FakeContext };
};

describe("noise processor", () => {
  it("runs DeepFilterNet3, and passes the microphone through while it loads", async () => {
    const dfn = engine();
    let loaded: (n: NoiseNode) => void = () => {};
    const { processor, statuses, ctx } = setup({
      deepFilterNet: () =>
        new Promise((resolve) => {
          loaded = resolve;
        }),
    });
    await processor.init(opts(microphone()));
    expect(processor.processedTrack).toBe(processed);
    expect(ctx().path()).toBe(ctx().out);
    expect(statuses).toEqual([{ running: "loading" }]);

    loaded(dfn);
    await vi.waitFor(() => {
      expect(ctx().path()).toBe(dfn.node);
    });
    expect(dfn.node).toBeDefined();
    expect([...(dfn.node as unknown as FakeNode).targets]).toEqual([ctx().out]);
    expect(statuses.at(-1)).toEqual({ running: "enhanced" });
  });

  it("falls back to RNNoise when DeepFilterNet3 does not load", async () => {
    const rn = engine();
    const { processor, statuses, ctx } = setup({ rnnoise: () => Promise.resolve(rn) });
    await processor.init(opts(microphone()));
    await vi.waitFor(() => {
      expect(statuses.at(-1)).toEqual({ running: "rnnoise", reason: "failed" });
    });
    expect(ctx().path()).toBe(rn.node);
  });

  it("falls back to the browser's noise suppression when neither loads", async () => {
    const mic = microphone({ noiseSuppression: true });
    const { processor, statuses, ctx } = setup({});
    await processor.init(opts(mic));
    await vi.waitFor(() => {
      expect(statuses.at(-1)).toEqual({ running: "standard", reason: "failed" });
    });
    expect(applied(mic)).toHaveBeenCalledWith({
      echoCancellation: true,
      noiseSuppression: true,
      autoGainControl: true,
    });
    expect(ctx().path()).toBe(ctx().out);
  });

  it("says the microphone is not cleaned when the browser cannot do it either", async () => {
    const { processor, statuses } = setup({});
    await processor.init(opts(microphone({ noiseSuppression: false })));
    await vi.waitFor(() => {
      expect(statuses.at(-1)).toEqual({ running: "off", reason: "failed" });
    });
  });

  it("moves to RNNoise once when the machine cannot keep up, and remembers it", async () => {
    const dfn = engine();
    const rn = engine();
    let overload = () => {};
    const rnnoise = vi.fn(() => Promise.resolve(rn));
    const { processor, statuses, memory, ctx } = setup({
      deepFilterNet: (_ctx, onOverload) => {
        overload = onOverload;
        return Promise.resolve(dfn);
      },
      rnnoise,
    });
    await processor.init(opts(microphone()));
    await vi.waitFor(() => {
      expect(statuses.at(-1)).toEqual({ running: "enhanced" });
    });
    overload();
    overload();
    await vi.waitFor(() => {
      expect(statuses.at(-1)).toEqual({ running: "rnnoise", reason: "overload" });
    });
    expect(rnnoise).toHaveBeenCalledTimes(1);
    expect(dfn.disposed).toBe(true);
    expect(ctx().path()).toBe(rn.node);
    expect(memory.overloaded).toBe(true);
  });

  it("goes straight to RNNoise after an overload earlier in the session", async () => {
    const deepFilterNet = vi.fn(() => Promise.resolve(engine()));
    const { processor, statuses, memory } = setup({ deepFilterNet, rnnoise: () => Promise.resolve(engine()) });
    memory.overloaded = true;
    await processor.init(opts(microphone()));
    await vi.waitFor(() => {
      expect(statuses.at(-1)).toEqual({ running: "rnnoise", reason: "overload" });
    });
    expect(deepFilterNet).not.toHaveBeenCalled();
  });

  it("keeps one context and one engine across a new microphone track", async () => {
    const dfn = engine();
    const deepFilterNet = vi.fn(() => Promise.resolve(dfn));
    const { processor, ctx } = setup({ deepFilterNet });
    await processor.init(opts(microphone()));
    await vi.waitFor(() => {
      expect(ctx().path()).toBe(dfn.node);
    });
    const next = microphone();
    await processor.restart(opts(next));
    expect(FakeContext.made).toHaveLength(1);
    expect(deepFilterNet).toHaveBeenCalledTimes(1);
    expect(ctx().sources.at(-1)?.stream).toEqual({ tracks: [next] });
    expect(ctx().path()).toBe(dfn.node);
    expect(ctx().sources[0]?.targets.size).toBe(0);
  });

  it("closes everything on destroy, and drops an engine that loads after", async () => {
    const dfn = engine();
    let loaded: (n: NoiseNode) => void = () => {};
    const { processor, statuses, ctx } = setup({
      deepFilterNet: () =>
        new Promise((resolve) => {
          loaded = resolve;
        }),
    });
    await processor.init(opts(microphone()));
    await processor.destroy();
    expect(ctx().closed).toBe(true);
    loaded(dfn);
    await vi.waitFor(() => {
      expect(dfn.disposed).toBe(true);
    });
    expect(statuses).toEqual([{ running: "loading" }]);
  });

  it("disposes the engine and closes the context when destroyed while running", async () => {
    const dfn = engine();
    const { processor, ctx } = setup({ deepFilterNet: () => Promise.resolve(dfn) });
    await processor.init(opts(microphone()));
    await vi.waitFor(() => {
      expect(ctx().path()).toBe(dfn.node);
    });
    await processor.destroy();
    expect(dfn.disposed).toBe(true);
    expect(ctx().closed).toBe(true);
  });
});

describe("browser noise suppression", () => {
  it("turns it on and says whether the browser did", async () => {
    expect(await browserNoiseSuppression(microphone({ noiseSuppression: true }))).toBe("standard");
    expect(await browserNoiseSuppression(microphone({}))).toBe("off");
    const refusing = microphone({});
    applied(refusing).mockRejectedValueOnce(new Error("OverconstrainedError"));
    expect(await browserNoiseSuppression(refusing)).toBe("off");
  });
});
