import {
  type AudioCaptureOptions,
  DisconnectReason,
  type RoomConnectOptions,
  type RoomOptions,
  type TrackPublishOptions,
  VideoPreset,
} from "livekit-client";
import type { EndCause } from "@/shared/lib";
import type { VoicePrefs } from "../model/prefs";
import type { Browser } from "./browser";

/**
 * The room: layers nobody watches are not sent (dynacast). Viewers ask for
 * the best layer, not the one that fits the tile: the only video here is a
 * screen share, watched on demand, and a small tile on a low layer was blurry.
 * The server still moves a viewer to a lower layer when their link is short.
 */
export const roomOptions = (): Partial<RoomOptions> => ({ adaptiveStream: false, dynacast: true });

/** How the encoder treats a screen: sharpness first (text, details); the frame rate is held by screenShareOptions. */
export const SCREEN_CONTENT_HINT = "detail";

/** Microphones are subscribed by hand, screen shares only when watched. */
export const connectOptions = (): RoomConnectOptions => ({ autoSubscribe: false });

/**
 * The microphone: echo cancellation always; the browser's noise suppression
 * and gain control in «Стандартное», gain control alone when off, neither in
 * «Улучшенное», where DeepFilterNet3 cleans and levels the voice (lib/noise.ts).
 */
export const microphoneOptions = ({ inputId, noise }: Pick<VoicePrefs, "inputId" | "noise">): AudioCaptureOptions => ({
  echoCancellation: true,
  autoGainControl: noise !== "enhanced",
  noiseSuppression: noise === "standard",
  ...(inputId === null ? {} : { deviceId: inputId }),
});

/**
 * A screen share: sharp 1080p at 30 fps rather than a blurry 60. When the
 * link is short the resolution drops, never the frame rate, and viewers with
 * a slow link get the 720p30 layer: no layer goes under 30 fps. H.264 in
 * Chromium; VP8 in Firefox,
 * which may lack H.264 and gets no simulcast for screens from livekit-client.
 * Firefox captures no sound; Chromium leaves this tab's sound (the voice chat)
 * out of the system sound where it can.
 */
export const screenShareOptions = (
  browser: Browser,
  restrictOwnAudio: boolean,
): { capture: DisplayMediaStreamOptions; publish: TrackPublishOptions } => ({
  capture: {
    video: { width: 1920, height: 1080, frameRate: 30 },
    audio:
      browser === "firefox"
        ? false
        : {
            echoCancellation: false,
            noiseSuppression: false,
            autoGainControl: false,
            ...(restrictOwnAudio ? { restrictOwnAudio: true } : {}),
          },
    // Chromium hints: offer the system sound, hide this tab from the picker.
    ...({ systemAudio: "include", selfBrowserSurface: "exclude", surfaceSwitching: "include" } as object),
  },
  publish: {
    videoCodec: browser === "firefox" ? "vp8" : "h264",
    degradationPreference: "maintain-framerate",
    simulcast: true,
    // Almost twice the bits per frame of the old 8 Mbit/s at 60 fps.
    screenShareEncoding: { maxBitrate: 6_000_000, maxFramerate: 30 },
    screenShareSimulcastLayers: [new VideoPreset(1280, 720, 2_000_000, 30)],
  },
});

/** What a LiveKit disconnect means for the call. */
export const endCause = (reason: DisconnectReason | undefined): EndCause => {
  switch (reason) {
    case DisconnectReason.CLIENT_INITIATED:
      return "left";
    case DisconnectReason.DUPLICATE_IDENTITY:
      return "replaced";
    case DisconnectReason.PARTICIPANT_REMOVED:
    case DisconnectReason.ROOM_DELETED:
      return "removed";
    default:
      return "failed";
  }
};
