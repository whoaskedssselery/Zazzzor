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
  startScreenShare(): Promise<ScreenShareResult>;
  stopScreenShare(): Promise<void>;
  /** Receive `userId`'s screen share (video and sound) or stop receiving it. */
  watch(userId: string, on: boolean): void;
  /** Shows `userId`'s screen share in `el` until the returned function runs. */
  attachScreen(userId: string, el: HTMLVideoElement): () => void;
  useDevice(kind: "audioinput" | "audiooutput", deviceId: string): Promise<void>;
  on<K extends keyof CallTransportEvents>(event: K, fn: CallTransportEvents[K]): () => void;
}
