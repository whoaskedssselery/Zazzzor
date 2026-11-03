export type { CallLink, CallTransport, CallTransportEvents, EndCause, ScreenShareResult } from "@/shared/lib";
export { type Browser, canRestrictOwnAudio, currentBrowser } from "./lib/browser";
export type { VoicePrefs } from "./model/prefs";

/** The LiveKit transport, in a chunk of its own: it loads with the first voice call. */
export const loadLiveKitTransport = async () => (await import("./lib/livekit")).createLiveKitTransport;
export { type CallState, clearCall, getCall, resetCall, setCall, useCall } from "./model/store";
