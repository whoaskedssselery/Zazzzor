export type Browser = "chromium" | "firefox";

/** Firefox, or anything else (Chrome, Edge and the other Chromium browsers). */
export const currentBrowser = (): Browser => (navigator.userAgent.includes("Firefox") ? "firefox" : "chromium");

/** Whether screen capture can leave out the sound of this tab (Chromium). */
export const canRestrictOwnAudio = (): boolean =>
  // mediaDevices is missing outside a secure context (and in jsdom).
  "mediaDevices" in navigator && "restrictOwnAudio" in navigator.mediaDevices.getSupportedConstraints();

/**
 * Whether this browser can share a screen at all. Phones' browsers cannot
 * (no getDisplayMedia on Android and iOS): there the share button is not shown.
 */
export const canShareScreen = (): boolean =>
  "mediaDevices" in navigator && typeof navigator.mediaDevices.getDisplayMedia === "function";

/** The browser can open a camera (a secure page with getUserMedia). */
export const canUseCamera = (): boolean =>
  "mediaDevices" in navigator && typeof navigator.mediaDevices.getUserMedia === "function";
