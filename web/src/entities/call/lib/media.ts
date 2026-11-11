import {
  type AudioCaptureOptions,
  DisconnectReason,
  type RoomConnectOptions,
  type RoomOptions,
  type TrackPublishOptions,
} from "livekit-client";
import type { EndCause } from "@/shared/lib";
import type { VoicePrefs } from "../model/prefs";
import type { Browser } from "./browser";

/**
 * The room: a stream nobody watches is paused (dynacast), and viewers get the
 * stream as it is sent rather than one sized to their tile. The only video
 * here is a screen share, watched on demand, sent as a single 1080p layer.
 */
export const roomOptions = (): Partial<RoomOptions> => ({ adaptiveStream: false, dynacast: true });

/** How the encoder treats a screen: as motion (games, video, scrolling); "detail" dropped a moving screen to a few fps. */
export const SCREEN_CONTENT_HINT = "motion";

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
 * A screen share: one VP8 stream at 1080p, up to 60 fps and 10 Mbit/s, and
 * the resolution never drops: when the sender's CPU or link is short, frames
 * go, not sharpness.
 *
 * Measured in Chrome with a 1080p page flipping twice a second (a tab
 * switch) and a viewer: H.264 with simulcast fell to 480p (the encoder's
 * quality scaler) or, with the resolution held, to 10 fps; any simulcast
 * cost the top layer its frames (two encodes); VP9 and AV1 gave 3–7 fps.
 * VP8 alone held 1080p at ~30 fps with the viewer on the same machine.
 * The cost: no lower layer for a viewer on a slow link.
 *
 * Holding the resolution has its own failure: when the sender's link is
 * short, 1080p fell to 3–5 fps. Then the share steps down to 720p and back
 * up when the link recovers (screen-quality.ts).
 *
 * Firefox captures no sound; Chromium leaves this tab's sound (the voice
 * chat) out of the system sound where it can.
 */
export const screenShareOptions = (
  browser: Browser,
  restrictOwnAudio: boolean,
): { capture: DisplayMediaStreamOptions; publish: TrackPublishOptions } => ({
  capture: {
    video: { width: 1920, height: 1080, frameRate: 60 },
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
    videoCodec: "vp8",
    degradationPreference: "maintain-resolution",
    simulcast: false,
    screenShareEncoding: { maxBitrate: 10_000_000, maxFramerate: 60 },
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
