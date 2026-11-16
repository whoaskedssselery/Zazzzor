/**
 * Who is speaking, measured here rather than told by the server. LiveKit's
 * server measures levels every 400 ms and smooths over several of them, and
 * its answer then travels back: the speaking ring lit up long after the
 * voice was heard. Here each microphone (the user's own, and each one
 * received) feeds an analyser, read every 50 ms: the ring lights with the
 * first loud frame and goes out after a short hold, so it follows what the
 * user hears.
 */

/** Read this often. */
export const SAMPLE_MS = 50;
/** A frame this loud (RMS of the samples, 0..1; about -38 dBFS) is speech. */
export const LOUD = 0.0125;
/** Speaking ends this long after the last loud frame: pauses between words keep the ring on. */
export const HOLD_MS = 350;

export interface SpeakingDetector {
  /** Measures `track` as `userId`'s voice (one track per user: a new one replaces the old). */
  watch(userId: string, track: MediaStreamTrack): void;
  /** Stops measuring `userId`. */
  unwatch(userId: string): void;
  /** Stops everything and lets the audio context go. */
  stop(): void;
}

interface Watched {
  track: MediaStreamTrack;
  source: MediaStreamAudioSourceNode;
  analyser: AnalyserNode;
  samples: Float32Array<ArrayBuffer>;
  lastLoud: number;
}

/** The loudness of `samples`: their root mean square. */
export const rms = (samples: Float32Array): number => {
  let sum = 0;
  for (const s of samples) sum += s * s;
  return samples.length === 0 ? 0 : Math.sqrt(sum / samples.length);
};

/**
 * A detector that calls `onChange` with who speaks (sorted) whenever that
 * changes. Null where the browser has no Web Audio: then the server's
 * active speakers are used instead.
 */
export const createSpeakingDetector = (
  onChange: (userIds: string[]) => void,
  now: () => number = () => performance.now(),
): SpeakingDetector | null => {
  if (typeof AudioContext === "undefined") return null;
  let ctx: AudioContext | null = null;
  let timer: ReturnType<typeof setInterval> | null = null;
  const watched = new Map<string, Watched>();
  let speaking: string[] = [];

  const context = (): AudioContext => {
    if (ctx === null) {
      ctx = new AudioContext();
    }
    // Made outside a click it starts suspended; a click (joining) has come before.
    if (ctx.state === "suspended") void ctx.resume().catch(() => undefined);
    return ctx;
  };

  const tick = () => {
    const t = now();
    const next: string[] = [];
    for (const [id, w] of watched) {
      w.analyser.getFloatTimeDomainData(w.samples);
      if (rms(w.samples) >= LOUD) w.lastLoud = t;
      if (t - w.lastLoud < HOLD_MS) next.push(id);
    }
    next.sort();
    if (next.length !== speaking.length || next.some((id, i) => id !== speaking[i])) {
      speaking = next;
      onChange(next);
    }
  };

  const release = (w: Watched) => {
    w.source.disconnect();
    w.analyser.disconnect();
  };

  return {
    watch(userId, track) {
      const held = watched.get(userId);
      if (held?.track === track) return;
      if (held) release(held);
      watched.delete(userId);
      try {
        const c = context();
        const source = c.createMediaStreamSource(new MediaStream([track]));
        const analyser = c.createAnalyser();
        analyser.fftSize = 1024;
        // Silent, but connected: a node the destination does not pull from may not be processed.
        const sink = c.createGain();
        sink.gain.value = 0;
        source.connect(analyser).connect(sink).connect(c.destination);
        watched.set(userId, {
          track,
          source,
          analyser,
          samples: new Float32Array(analyser.fftSize),
          lastLoud: Number.NEGATIVE_INFINITY,
        });
      } catch {
        // A track Web Audio cannot read: that one person shows no ring, nothing else breaks.
        return;
      }
      timer ??= setInterval(tick, SAMPLE_MS);
    },

    unwatch(userId) {
      const held = watched.get(userId);
      if (!held) return;
      release(held);
      watched.delete(userId);
      tick();
    },

    stop() {
      if (timer !== null) clearInterval(timer);
      timer = null;
      for (const w of watched.values()) release(w);
      watched.clear();
      void ctx?.close().catch(() => undefined);
      ctx = null;
      if (speaking.length > 0) {
        speaking = [];
        onChange([]);
      }
    },
  };
};
