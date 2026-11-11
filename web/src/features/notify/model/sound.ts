/** Two sounds closer than this are one: a burst of mentions chimes once. */
export const SOUND_GAP_MS = 1_500;

let context: AudioContext | null = null;
let last = Number.NEGATIVE_INFINITY;

/**
 * A short two-note chime for a mention, made on the spot (no file to load).
 * Browsers start sound only after the user has interacted with the page;
 * before that the chime is silently skipped.
 */
export const playMentionSound = (): void => {
  const now = performance.now();
  if (now - last < SOUND_GAP_MS) return;
  last = now;
  try {
    context ??= new AudioContext();
    const ctx = context;
    if (ctx.state === "suspended") void ctx.resume().catch(() => undefined);
    const start = ctx.currentTime + 0.01;
    [880, 1318.5].forEach((frequency, i) => {
      const at = start + i * 0.09;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = "sine";
      osc.frequency.value = frequency;
      gain.gain.setValueAtTime(0, at);
      gain.gain.linearRampToValueAtTime(0.12, at + 0.01);
      gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.22);
      osc.connect(gain).connect(ctx.destination);
      osc.start(at);
      osc.stop(at + 0.24);
    });
  } catch {
    // No audio here (a test, an old browser): a sound is not worth an error.
  }
};
