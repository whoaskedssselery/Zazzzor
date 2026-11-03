import type { AudioProcessorOptions, Track, TrackProcessor } from "livekit-client";
import type { NoiseStatus } from "@/shared/lib";

/** A noise suppression node, running; `dispose` stops it for good. */
export interface NoiseNode {
  node: AudioNode;
  dispose(): void;
}

/** How each engine starts in a 48 kHz context (lib/noise-engines.ts; fakes in tests). */
export interface NoiseEngines {
  /** Resolves once DeepFilterNet3 runs; `onOverload` fires when the machine cannot keep up. */
  deepFilterNet(ctx: AudioContext, onOverload: () => void): Promise<NoiseNode>;
  rnnoise(ctx: AudioContext): Promise<NoiseNode>;
}

export interface NoiseDeps {
  engines: NoiseEngines;
  report: (status: NoiseStatus) => void;
  /** Kept by the transport for the whole session: after one overload, DeepFilterNet3 is not tried again. */
  memory: { overloaded: boolean };
  makeContext?: () => AudioContext;
}

/** Both engines run at 48 kHz, whatever the room's own context does. */
const SAMPLE_RATE = 48_000;

/** Turns the browser's noise suppression (and gain control) on, and says whether it took. */
export const browserNoiseSuppression = async (track: MediaStreamTrack): Promise<"standard" | "off"> => {
  try {
    await track.applyConstraints({ ...track.getConstraints(), noiseSuppression: true, autoGainControl: true });
  } catch {
    // The browser keeps the track as it was.
  }
  return track.getSettings().noiseSuppression === true ? "standard" : "off";
};

/**
 * The microphone's «Улучшенное» noise suppression, as a LiveKit processor:
 * the track goes through DeepFilterNet3 in a context of its own. Until the
 * model has loaded the microphone passes through as it is; if it does not
 * load, or the machine cannot keep up, RNNoise takes over; if that fails too,
 * the browser's own noise suppression. A new microphone track (another
 * device) keeps the context and the engine.
 */
export const createNoiseProcessor = (deps: NoiseDeps): TrackProcessor<Track.Kind.Audio, AudioProcessorOptions> => {
  const makeContext =
    deps.makeContext ?? (() => new AudioContext({ sampleRate: SAMPLE_RATE, latencyHint: "interactive" }));
  let ctx: AudioContext | null = null;
  let source: MediaStreamAudioSourceNode | null = null;
  let out: MediaStreamAudioDestinationNode | null = null;
  let engine: NoiseNode | null = null;
  let input: MediaStreamTrack | null = null;
  /** The browser cleans the microphone: a new track needs it switched on again. */
  let browserFallback = false;
  /** Bumped by destroy: whatever loads after belongs to no one. */
  let life = 0;

  const wire = () => {
    if (!source || !out) return;
    source.disconnect();
    if (engine) {
      source.connect(engine.node);
      engine.node.connect(out);
    } else source.connect(out);
  };

  const swapTo = (next: NoiseNode | null) => {
    engine?.dispose();
    engine = next;
    wire();
  };

  const toBrowser = async (mine: number) => {
    swapTo(null);
    browserFallback = true;
    if (!input) return;
    const running = await browserNoiseSuppression(input);
    if (mine === life) deps.report({ running, reason: "failed" });
  };

  const toRnnoise = async (mine: number, reason: "overload" | "failed") => {
    if (!ctx) return;
    try {
      const next = await deps.engines.rnnoise(ctx);
      if (mine !== life) {
        next.dispose();
        return;
      }
      swapTo(next);
      deps.report({ running: "rnnoise", reason });
    } catch {
      if (mine === life) await toBrowser(mine);
    }
  };

  const start = async (mine: number) => {
    if (!ctx) return;
    if (deps.memory.overloaded) {
      await toRnnoise(mine, "overload");
      return;
    }
    deps.report({ running: "loading" });
    let dfn: NoiseNode | null = null;
    const onOverload = () => {
      if (mine !== life || engine === null || engine !== dfn || deps.memory.overloaded) return;
      deps.memory.overloaded = true;
      void toRnnoise(mine, "overload");
    };
    try {
      dfn = await deps.engines.deepFilterNet(ctx, onOverload);
    } catch {
      if (mine === life) await toRnnoise(mine, "failed");
      return;
    }
    if (mine !== life) {
      dfn.dispose();
      return;
    }
    swapTo(dfn);
    deps.report({ running: "enhanced" });
  };

  const listen = (track: MediaStreamTrack) => {
    if (!ctx) return;
    source?.disconnect();
    input = track;
    source = ctx.createMediaStreamSource(new MediaStream([track]));
    wire();
    if (browserFallback) void browserNoiseSuppression(track);
  };

  const processor: TrackProcessor<Track.Kind.Audio, AudioProcessorOptions> = {
    name: "zazzzor-noise-suppression",

    init(opts) {
      ctx = makeContext();
      // Joining is a click, so the context may run; this resumes it if the browser held it back.
      void ctx.resume().catch(() => undefined);
      out = ctx.createMediaStreamDestination();
      processor.processedTrack = out.stream.getAudioTracks()[0];
      listen(opts.track);
      void start(life);
      return Promise.resolve();
    },

    async restart(opts) {
      if (!ctx) {
        await processor.init(opts);
        return;
      }
      listen(opts.track);
    },

    async destroy() {
      life += 1;
      engine?.dispose();
      engine = null;
      source?.disconnect();
      source = null;
      for (const t of out?.stream.getTracks() ?? []) t.stop();
      out = null;
      input = null;
      const closing = ctx;
      ctx = null;
      await closing?.close().catch(() => undefined);
    },
  };
  return processor;
};
