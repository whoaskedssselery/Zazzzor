import type { ScreenShareCaptureOptions, TrackPublishOptions } from "livekit-client";

export function screenShareOptions(): { capture: ScreenShareCaptureOptions; publish: TrackPublishOptions } {
  return {
    capture: {
      resolution: { width: 1920, height: 1080, frameRate: 60 },
      contentHint: "motion",
      audio: true,
    },
    publish: {
      videoCodec: "h264",
      degradationPreference: "maintain-framerate",
      simulcast: true,
      screenShareEncoding: { maxBitrate: 8_000_000, maxFramerate: 60 },
    },
  };
}

export function isPickerCancelled(err: unknown): boolean {
  return err instanceof DOMException && err.name === "NotAllowedError";
}
