/** The link to the voice server. */
export type CallLink = "connecting" | "connected" | "reconnecting" | "disconnected";

/**
 * Why a call ended: the user left; the same account joined voice elsewhere;
 * a moderator or the server removed them; or the link failed.
 */
export type EndCause = "left" | "replaced" | "removed" | "failed";

/** What the picker gave, or why there is no share. */
export type ScreenShareResult =
  | { ok: true; surface: "monitor" | "window" | "browser" | "unknown"; audio: boolean }
  | { ok: false; reason: "cancelled" | "failed" };

/** The camera is on, or why not: refused by the user or the browser, no camera, or anything else. */
export type CameraResult = { ok: true } | { ok: false; reason: "denied" | "missing" | "failed" };

/**
 * What cleans the microphone now: DeepFilterNet3 ("enhanced", "loading" while
 * its model downloads), RNNoise as the fallback, the browser's own ("standard"),
 * or nothing. `reason` says why it is not what the user chose.
 */
export interface NoiseStatus {
  running: "loading" | "enhanced" | "rnnoise" | "standard" | "off";
  reason?: "overload" | "failed";
}

export interface CallTransportEvents {
  link: (state: CallLink, cause?: EndCause) => void;
  /** Who is speaking now, the user included. */
  speaking: (userIds: string[]) => void;
  /** Who shares a screen now (others only). */
  streams: (userIds: string[]) => void;
  /** The microphone was refused or there is none; the call goes on without it. */
  micDenied: () => void;
  /** The browser stopped the user's screen share ("Stop sharing"). */
  screenEnded: () => void;
  /** Who has a camera on now (others only). */
  cameras: (userIds: string[]) => void;
  /** The user's camera went off by itself: unplugged, taken by another app, or no longer allowed. */
  cameraEnded: () => void;
  /** The microphone's noise suppression, each time it changes. */
  noise: (status: NoiseStatus) => void;
}

/**
 * A voice call as the app uses it. The LiveKit implementation is in entities/call
 * (lib/livekit.ts); tests use FakeTransport from shared/test.
 */
export interface CallTransport {
  connect(url: string, token: string): Promise<void>;
  disconnect(): Promise<void>;
  setMicrophone(on: boolean): Promise<void>;
  /** Hear the others (false = deafened, screen share sound included). */
  setListening(on: boolean): void;
  /**
   * How loud `userId` is heard: their voice or the sound of their screen
   * share, from 0 (silent) through 1 (as sent) to 2. Kept across joins.
   */
  setVolume(userId: string, source: "voice" | "stream", volume: number): void;
  startScreenShare(): Promise<ScreenShareResult>;
  stopScreenShare(): Promise<void>;
  /** Receive `userId`'s screen share (video and sound) or stop receiving it. */
  watch(userId: string, on: boolean): void;
  /** Shows `userId`'s screen share in `el` until the returned function runs. */
  attachScreen(userId: string, el: HTMLVideoElement): () => void;
  /** Shows the user's own screen share in `el` (a local preview, nothing is received) until the returned function runs. */
  attachOwnScreen(el: HTMLVideoElement): () => void;
  startCamera(): Promise<CameraResult>;
  stopCamera(): Promise<void>;
  /**
   * Receives `userId`'s camera and shows it in `el` until the returned
   * function runs: a camera costs bandwidth only while its tile is shown,
   * and at the size of that tile.
   */
  attachCamera(userId: string, el: HTMLVideoElement): () => void;
  /** Shows the user's own camera in `el` (a local preview) until the returned function runs. */
  attachOwnCamera(el: HTMLVideoElement): () => void;
  useDevice(kind: "audioinput" | "audiooutput", deviceId: string): Promise<void>;
  on<K extends keyof CallTransportEvents>(event: K, fn: CallTransportEvents[K]): () => void;
}
