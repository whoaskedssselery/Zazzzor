export type {
  CallLink,
  CallTransport,
  CallTransportEvents,
  EndCause,
  NoiseStatus,
  ScreenShareResult,
} from "@/shared/lib";
export { type Browser, canRestrictOwnAudio, canShareScreen, currentBrowser } from "./lib/browser";
export {
  DEFAULT_VOICE_PREFS,
  readVoicePrefs,
  VOICE_PREFS_KEY,
  type VoicePrefs,
  writeVoicePrefs,
} from "./model/prefs";

/** The LiveKit transport, in a chunk of its own: it loads with the first voice call. */
export const loadLiveKitTransport = async () => (await import("./lib/livekit")).createLiveKitTransport;
export { type CallState, clearCall, getCall, resetCall, setCall, useCall } from "./model/store";
