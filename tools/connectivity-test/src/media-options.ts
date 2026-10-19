import type { RoomOptions, ScreenShareCaptureOptions, TrackPublishOptions } from "livekit-client";

/**
 * adaptiveStream is off on purpose: it would pick a simulcast layer by the size
 * of the <video> element, and the test must measure the real 1080p load.
 * Side effect: pauseVideoInBackground is an AdaptiveStreamSettings option and
 * does nothing without adaptiveStream, so a background tab keeps receiving
 * video and does not show fake stalls. dynacast still stops unused layers.
 */
export function roomOptions(): Partial<RoomOptions> {
  return { adaptiveStream: false, dynacast: true };
}

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
