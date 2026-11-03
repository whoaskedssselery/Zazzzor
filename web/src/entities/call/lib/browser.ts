export type Browser = "chromium" | "firefox";

/** Firefox, or anything else (Chrome, Edge and the other Chromium browsers). */
export const currentBrowser = (): Browser => (navigator.userAgent.includes("Firefox") ? "firefox" : "chromium");

/** Whether screen capture can leave out the sound of this tab (Chromium). */
export const canRestrictOwnAudio = (): boolean =>
  // mediaDevices is missing outside a secure context (and in jsdom).
  "mediaDevices" in navigator && "restrictOwnAudio" in navigator.mediaDevices.getSupportedConstraints();
