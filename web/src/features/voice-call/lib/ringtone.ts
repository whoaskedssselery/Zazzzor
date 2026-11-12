/** How often the ring repeats. */
const RING_EVERY_MS = 2_000;

/**
 * Plays the ring of an incoming call, made on the spot as the mention chime
 * is: two soft rising notes, again every two seconds until the returned
 * function stops it. Before the user has interacted with the page the
 * browser keeps it silent; that is fine.
 */
export const startRingtone = (): (() => void) => {
  let ctx: AudioContext;
  try {
    ctx = new AudioContext();
  } catch {
    // No audio here (a test, an old browser).
    return () => {};
  }
  if (ctx.state === "suspended") void ctx.resume().catch(() => undefined);
  const ring = () => {
    const start = ctx.currentTime + 0.01;
    [659.25, 987.77, 659.25, 987.77].forEach((frequency, i) => {
      const at = start + i * 0.18;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = "sine";
      osc.frequency.value = frequency;
      gain.gain.setValueAtTime(0, at);
      gain.gain.linearRampToValueAtTime(0.1, at + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.16);
      osc.connect(gain).connect(ctx.destination);
      osc.start(at);
      osc.stop(at + 0.17);
    });
  };
  ring();
  const timer = setInterval(ring, RING_EVERY_MS);
  return () => {
    clearInterval(timer);
    void ctx.close().catch(() => undefined);
  };
};
