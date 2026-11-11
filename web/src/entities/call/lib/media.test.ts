import { DisconnectReason } from "livekit-client";
import { endCause, microphoneOptions, roomOptions, screenShareOptions } from "./media";

describe("screenShareOptions", () => {
  it("sends one VP8 stream at 1080p, up to 60 fps, and never drops the resolution", () => {
    for (const browser of ["chromium", "firefox"] as const) {
      const { capture, publish } = screenShareOptions(browser, false);
      expect(capture.video).toMatchObject({ width: 1920, height: 1080, frameRate: 60 });
      expect(publish).toMatchObject({
        videoCodec: "vp8",
        simulcast: false,
        degradationPreference: "maintain-resolution",
        screenShareEncoding: { maxBitrate: 10_000_000, maxFramerate: 60 },
      });
      expect(publish.screenShareSimulcastLayers).toBeUndefined();
    }
  });

  it("gives viewers the stream as sent, not one sized to their tile", () => {
    expect(roomOptions()).toMatchObject({ adaptiveStream: false, dynacast: true });
  });

  it("asks Chromium for the system sound without its own tab, where it can", () => {
    expect(screenShareOptions("chromium", true).capture.audio).toMatchObject({ restrictOwnAudio: true });
    expect(screenShareOptions("chromium", false).capture.audio).not.toHaveProperty("restrictOwnAudio");
  });

  it("asks Firefox for no sound", () => {
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
