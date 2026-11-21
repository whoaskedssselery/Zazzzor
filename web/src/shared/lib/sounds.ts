/**
 * The app's short sounds, made on the spot with Web Audio (no files to load
 * or license): a few soft sine notes each. Browsers start sound only after
 * the user has interacted with the page; before that a sound is silently
 * skipped.
 */
export type SoundName =
  /** The user joined voice, or a call. */
  | "join"
  /** The user left voice. */
  | "leave"
  /** Someone else came into the user's channel or call. */
  | "userJoin"
  /** Someone else left it. */
  | "userLeave"
  | "mute"
  | "unmute"
  | "deafen"
  | "undeafen"
  /** A screen share started in the call (the user's or someone else's). */
  | "streamStart"
  | "streamStop"
  /** The user started watching someone's screen share. */
  | "watchStart"
  /** The user stopped watching it. */
  | "watchStop"
  /** A message that alerts the user. */
  | "mention";

/** One note: its pitch in Hz, when it starts after the sound does, and how long it rings, in seconds. */
interface Note {
  hz: number;
  at: number;
  len: number;
  /** A glide to this pitch over the note. */
  to?: number;
}

const notes = (...hz: number[]): Note[] => hz.map((f, i) => ({ hz: f, at: i * 0.075, len: 0.16 }));

/** Rising means something began or came on; falling, it ended or went off. */
const SOUNDS: Record<SoundName, { notes: Note[]; gain: number }> = {
  join: { notes: notes(523.25, 659.25, 783.99), gain: 0.1 },
  leave: { notes: notes(783.99, 659.25, 523.25), gain: 0.1 },
  userJoin: { notes: [{ hz: 587.33, to: 880, at: 0, len: 0.18 }], gain: 0.09 },
  userLeave: { notes: [{ hz: 880, to: 587.33, at: 0, len: 0.18 }], gain: 0.09 },
  mute: { notes: notes(493.88, 369.99), gain: 0.08 },
  unmute: { notes: notes(369.99, 493.88), gain: 0.08 },
  deafen: { notes: notes(392, 293.66), gain: 0.09 },
  undeafen: { notes: notes(293.66, 392), gain: 0.09 },
  streamStart: { notes: notes(659.25, 830.61, 987.77), gain: 0.08 },
  streamStop: { notes: notes(987.77, 830.61, 659.25), gain: 0.08 },
  watchStart: {
    notes: [
      { hz: 698.46, to: 1046.5, at: 0, len: 0.14 },
      { hz: 1046.5, at: 0.1, len: 0.12 },
    ],
    gain: 0.07,
  },
  watchStop: {
    notes: [
      { hz: 1046.5, to: 698.46, at: 0, len: 0.14 },
      { hz: 698.46, at: 0.1, len: 0.12 },
    ],
    gain: 0.07,
  },
  mention: {
    notes: [
      { hz: 880, at: 0, len: 0.22 },
      { hz: 1318.5, at: 0.09, len: 0.22 },
    ],
    gain: 0.12,
  },
};

/** The same sound twice within this is one: a burst of events plays once. */
const REPEAT_GAP_MS: Partial<Record<SoundName, number>> = { mention: 1_500 };
const DEFAULT_GAP_MS = 120;

let context: AudioContext | null = null;
const lastPlayed = new Map<SoundName, number>();

/** Plays sound `name`, unless it just played. */
export const playSound = (name: SoundName): void => {
  const now = performance.now();
  if (now - (lastPlayed.get(name) ?? Number.NEGATIVE_INFINITY) < (REPEAT_GAP_MS[name] ?? DEFAULT_GAP_MS)) return;
  lastPlayed.set(name, now);
  try {
    context ??= new AudioContext();
    const ctx = context;
    if (ctx.state === "suspended") void ctx.resume().catch(() => undefined);
    const start = ctx.currentTime + 0.01;
    const { notes: played, gain: level } = SOUNDS[name];
    for (const n of played) {
      const at = start + n.at;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = "sine";
      osc.frequency.setValueAtTime(n.hz, at);
      if (n.to !== undefined) osc.frequency.exponentialRampToValueAtTime(n.to, at + n.len * 0.8);
      gain.gain.setValueAtTime(0, at);
      gain.gain.linearRampToValueAtTime(level, at + 0.01);
      gain.gain.exponentialRampToValueAtTime(0.0001, at + n.len);
      osc.connect(gain).connect(ctx.destination);
      osc.start(at);
      osc.stop(at + n.len + 0.02);
    }
  } catch {
    // No audio here (a test, an old browser): a sound is not worth an error.
  }
};

/** Forgets which sounds just played (tests). */
export const resetSounds = (): void => {
  lastPlayed.clear();
};
