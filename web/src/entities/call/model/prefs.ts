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
  /** The browser's own noise suppression ("Стандартное"), or none. */
  noise: "off" | "standard";
}
