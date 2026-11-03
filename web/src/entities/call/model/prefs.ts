/** The user's voice settings on this device (Task 7 reads and writes them). */
export interface VoicePrefs {
  /** The microphone; null is the system default. */
  inputId: string | null;
  /** The speakers or headphones; null is the system default. */
  outputId: string | null;
  /** Talk whenever the microphone is on, or only while `pttKey` is held. */
  mode: "voice" | "ptt";
  /** A `KeyboardEvent.code`. */
  pttKey: string;
  /** DeepFilterNet3 («Улучшенное», with fallbacks), the browser's own («Стандартное»), or none. */
  noise: "off" | "standard" | "enhanced";
}

export const VOICE_PREFS_KEY = "zazzzor.voice";

export const DEFAULT_VOICE_PREFS: VoicePrefs = {
  inputId: null,
  outputId: null,
  mode: "voice",
  pttKey: "KeyV",
  noise: "enhanced",
};

const idOrNull = (v: unknown): string | null => (typeof v === "string" && v !== "" ? v : null);

/** The voice settings of this browser; each field that is missing or not valid takes its default. */
export const readVoicePrefs = (): VoicePrefs => {
  let raw: unknown;
  try {
    raw = JSON.parse(localStorage.getItem(VOICE_PREFS_KEY) ?? "null");
  } catch {
    return DEFAULT_VOICE_PREFS;
  }
  if (typeof raw !== "object" || raw === null) return DEFAULT_VOICE_PREFS;
  const p = raw as Record<string, unknown>;
  return {
    inputId: idOrNull(p.inputId),
    outputId: idOrNull(p.outputId),
    mode: p.mode === "ptt" ? "ptt" : "voice",
    pttKey: typeof p.pttKey === "string" && p.pttKey !== "" ? p.pttKey : DEFAULT_VOICE_PREFS.pttKey,
    noise: p.noise === "off" || p.noise === "standard" ? p.noise : "enhanced",
  };
};

export const writeVoicePrefs = (prefs: VoicePrefs): void => {
  try {
    localStorage.setItem(VOICE_PREFS_KEY, JSON.stringify(prefs));
  } catch {
    // Storage blocked (private mode): the settings last for this tab only.
  }
};
