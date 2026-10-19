import { screenShareOptions, isPickerCancelled } from "./media-options";

test("screen share keeps 60 fps at 1080p over H.264", () => {
  const { capture, publish } = screenShareOptions();
  expect(capture.resolution).toEqual({ width: 1920, height: 1080, frameRate: 60 });
  expect(capture.contentHint).toBe("motion");
  expect(capture.audio).toBe(true);
  expect(publish.videoCodec).toBe("h264");
  expect(publish.degradationPreference).toBe("maintain-framerate");
  expect(publish.simulcast).toBe(true);
  expect(publish.screenShareEncoding).toEqual({ maxBitrate: 8_000_000, maxFramerate: 60 });
});
test("NotAllowedError means the picker was cancelled", () => {
  expect(isPickerCancelled(new DOMException("x", "NotAllowedError"))).toBe(true);
  expect(isPickerCancelled(new Error("boom"))).toBe(false);
});
