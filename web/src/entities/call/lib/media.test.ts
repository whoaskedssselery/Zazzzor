import { DisconnectReason } from "livekit-client";
import { endCause, microphoneOptions, roomOptions, screenShareOptions } from "./media";

describe("screenShareOptions", () => {
  it("sends sharp 1080p at 30 fps and keeps the frame rate when the link is short", () => {
    const { capture, publish } = screenShareOptions("chromium", true);
    expect(capture.video).toMatchObject({ width: 1920, height: 1080, frameRate: 30 });
    expect(publish).toMatchObject({
      degradationPreference: "maintain-framerate",
      simulcast: true,
      screenShareEncoding: { maxBitrate: 6_000_000, maxFramerate: 30 },
      videoCodec: "h264",
    });
  });

  it("gives slow viewers a 720p layer, still at 30 fps", () => {
    const layers = screenShareOptions("chromium", true).publish.screenShareSimulcastLayers ?? [];
    expect(layers.map((l) => [l.width, l.height, l.encoding.maxFramerate])).toEqual([[1280, 720, 30]]);
  });

  it("asks viewers for the best layer, not the one that fits the tile", () => {
    expect(roomOptions()).toMatchObject({ adaptiveStream: false, dynacast: true });
  });

  it("asks Chromium for the system sound without its own tab, where it can", () => {
    expect(screenShareOptions("chromium", true).capture.audio).toMatchObject({ restrictOwnAudio: true });
    expect(screenShareOptions("chromium", false).capture.audio).not.toHaveProperty("restrictOwnAudio");
  });

  it("asks Firefox for no sound and publishes VP8", () => {
    const { capture, publish } = screenShareOptions("firefox", false);
    expect(capture.audio).toBe(false);
    expect(publish.videoCodec).toBe("vp8");
  });
});

describe("microphoneOptions", () => {
  it("keeps echo cancellation; the browser's noise suppression and gain control by the setting", () => {
    expect(microphoneOptions({ inputId: null, noise: "standard" })).toEqual({
      echoCancellation: true,
      autoGainControl: true,
      noiseSuppression: true,
    });
    expect(microphoneOptions({ inputId: "mic-2", noise: "off" })).toEqual({
      echoCancellation: true,
      autoGainControl: true,
      noiseSuppression: false,
      deviceId: "mic-2",
    });
    // DeepFilterNet3 replaces both, and levels the voice itself.
    expect(microphoneOptions({ inputId: null, noise: "enhanced" })).toEqual({
      echoCancellation: true,
      autoGainControl: false,
      noiseSuppression: false,
    });
  });
});

describe("endCause", () => {
  it("tells a takeover, a removal and our own leave from a failure", () => {
    expect(endCause(DisconnectReason.DUPLICATE_IDENTITY)).toBe("replaced");
    expect(endCause(DisconnectReason.PARTICIPANT_REMOVED)).toBe("removed");
    expect(endCause(DisconnectReason.ROOM_DELETED)).toBe("removed");
    expect(endCause(DisconnectReason.CLIENT_INITIATED)).toBe("left");
    expect(endCause(DisconnectReason.SIGNAL_CLOSE)).toBe("failed");
    expect(endCause(undefined)).toBe("failed");
  });
});
