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
 * The room: viewers get the simulcast layer that fits the video's size, and
 * layers nobody watches are not sent.
 */
export const roomOptions = (): Partial<RoomOptions> => ({ adaptiveStream: true, dynacast: true });

/** Microphones are subscribed by hand, screen shares only when watched. */
export const connectOptions = (): RoomConnectOptions => ({ autoSubscribe: false });

/** The microphone: echo cancellation and gain control always, noise suppression by the setting. */
export const microphoneOptions = ({ inputId, noise }: Pick<VoicePrefs, "inputId" | "noise">): AudioCaptureOptions => ({
  echoCancellation: true,
  autoGainControl: true,
  noiseSuppression: noise === "standard",
  ...(inputId === null ? {} : { deviceId: inputId }),
});

/**
 * A screen share: 1080p at 60 fps for motion, and when the link is short the
 * resolution drops, never the frame rate. H.264 in Chromium; VP8 in Firefox,
 * which may lack H.264 and gets no simulcast for screens from livekit-client.
 * Firefox captures no sound; Chromium leaves this tab's sound (the voice chat)
 * out of the system sound where it can.
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
    videoCodec: browser === "firefox" ? "vp8" : "h264",
    degradationPreference: "maintain-framerate",
    simulcast: true,
    screenShareEncoding: { maxBitrate: 8_000_000, maxFramerate: 60 },
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
