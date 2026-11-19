import { desktopInvoke, inDesktopApp } from "@/shared/lib";

/**
 * The desktop app's own screen share (desktop/share): a screen or a window
 * on the GPU's encoder, with one application's sound, joined to the call
 * as the user's screen companion with a token from
 * `POST /channels/{id}/voice/screen`. The others see it as the user's share.
 */

/** A screen or a window the app can share. */
export interface ShareSource {
  id: string;
  kind: "screen" | "window";
  /** A window's title; a screen's device name. */
  name: string;
  /** The window's executable, e.g. "chrome.exe". */
  app: string | null;
  width: number;
  height: number;
}

/** The share's figures over the last two seconds. */
export interface NativeShareStats {
  captureFps: number;
  sentFps: number;
  width: number;
  height: number;
  mbitS: number;
  estimateMbitS: number | null;
  encoder: string;
  sound: boolean;
  level: "p1080" | "p720";
}

/** Why a share ended: stopped here, its connection removed (the call left or rejoined), its window closed. */
export type NativeShareEnd = "stopped" | "disconnected" | "sourceClosed";

export type NativeShareStatus =
  | { state: "idle" }
  | { state: "sharing"; stats: NativeShareStats | null }
  | { state: "ended"; end: NativeShareEnd };

/** How to share a source: with its sound or not, at up to 30 or 60 frames per second. */
export interface ShareChoice {
  sound: boolean;
  fps: 30 | 60;
}

export interface Thumbnail {
  width: number;
  height: number;
  rgba: Uint8ClampedArray<ArrayBuffer>;
}

/** Reads the app's thumbnail: width and height as little-endian u32, then RGBA rows. */
export const decodeThumbnail = (buffer: ArrayBuffer): Thumbnail => {
  const view = new DataView(buffer);
  const width = view.getUint32(0, true);
  const height = view.getUint32(4, true);
  const rgba = new Uint8ClampedArray(buffer, 8, width * height * 4);
  return { width, height, rgba };
};

let available: Promise<boolean> | null = null;

export const nativeShare = {
  /** Whether this page may use the app's share (an older app has no such commands). Asked once. */
  available: (): Promise<boolean> => {
    available ??= inDesktopApp()
      ? desktopInvoke("share_status").then(
          () => true,
          () => false,
        )
      : Promise.resolve(false);
    return available;
  },
  sources: (): Promise<ShareSource[]> => desktopInvoke("share_sources"),
  thumbnail: async (id: string): Promise<Thumbnail> =>
    decodeThumbnail(await desktopInvoke<ArrayBuffer>("share_thumbnail", { id })),
  /** Starts sharing source `id` (replacing a share in progress); `sound` brings its sound, `fps` caps the frame rate. */
  start: (args: { url: string; token: string; id: string } & ShareChoice): Promise<void> =>
    desktopInvoke("share_start", { ...args }),
  stop: (): Promise<void> => desktopInvoke("share_stop"),
  status: (): Promise<NativeShareStatus> => desktopInvoke("share_status"),
};

/** Forgets whether the app's share is available (tests). */
export const resetNativeShare = (): void => {
  available = null;
};
