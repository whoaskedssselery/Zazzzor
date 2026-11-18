export type {
  CallLink,
  CallTransport,
  CallTransportEvents,
  CameraResult,
  EndCause,
  NoiseStatus,
  ScreenShareResult,
} from "@/shared/lib";
export { type Browser, canRestrictOwnAudio, canShareScreen, canUseCamera, currentBrowser } from "./lib/browser";
export {
  decodeThumbnail,
  type NativeShareEnd,
  type NativeShareStats,
  type NativeShareStatus,
  nativeShare,
  resetNativeShare,
  type ShareSource,
  type Thumbnail,
} from "./lib/native-share";
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
export {
  getVolumes,
  onVolumes,
  reloadVolumes,
  setVolume,
  useVolume,
  VOLUME_DEFAULT,
  VOLUME_MAX,
  VOLUMES_KEY,
  type VolumeKind,
} from "./model/volumes";
export { snapVolume, VolumeSlider } from "./ui/VolumeSlider";
