import {
  ConnectionState,
  LocalAudioTrack,
  type LocalTrackPublication,
  type Participant,
  RemoteAudioTrack,
  type RemoteParticipant,
  type RemoteTrack,
  type RemoteTrackPublication,
  Room,
  RoomEvent,
  Track,
} from "livekit-client";
import type { CallLink, CallTransport, CallTransportEvents, ScreenShareResult } from "@/shared/lib";
import type { VoicePrefs } from "../model/prefs";
import { canRestrictOwnAudio, currentBrowser } from "./browser";
import {
  connectOptions,
  endCause,
  microphoneOptions,
  roomOptions,
  SCREEN_CONTENT_HINT,
  screenShareOptions,
} from "./media";
import { browserNoiseSuppression, createNoiseProcessor } from "./noise";
import { noiseEngines } from "./noise-engines";

type Listeners = { [K in keyof CallTransportEvents]: Set<CallTransportEvents[K]> };

const LINK: Partial<Record<ConnectionState, CallLink>> = {
  [ConnectionState.Connecting]: "connecting",
  [ConnectionState.Connected]: "connected",
  [ConnectionState.Reconnecting]: "reconnecting",
  [ConnectionState.SignalReconnecting]: "reconnecting",
};

const SCREEN = new Set<Track.Source>([Track.Source.ScreenShare, Track.Source.ScreenShareAudio]);

/** How long a screen may stay unpublished before it counts as ended: a full reconnect publishes it again. */
const REPUBLISH_MS = 1000;

/** What getUserMedia throws when there is no usable microphone: refused, missing, busy, gone or blocked. */
const MIC_ERRORS = new Set([
  "NotAllowedError",
  "NotFoundError",
  "NotReadableError",
  "OverconstrainedError",
  "AbortError",
  "SecurityError",
]);

const isMicError = (e: unknown): boolean => e instanceof DOMException && MIC_ERRORS.has(e.name);

/**
 * The call over LiveKit. Microphones are received as soon as they are
 * published; screen shares only while watched. Remote sound plays through
 * hidden audio elements, muted all at once when deafened. A screen share is
 * captured here (getDisplayMedia, for `restrictOwnAudio`) and published as
 * the ScreenShare sources.
 */
export const createLiveKitTransport = (prefs: () => Pick<VoicePrefs, "inputId" | "noise">): CallTransport => {
  const listeners: Listeners = {
    link: new Set(),
    speaking: new Set(),
    streams: new Set(),
    micDenied: new Set(),
    screenEnded: new Set(),
    noise: new Set(),
  };
  const emit = <K extends keyof CallTransportEvents>(event: K, ...args: Parameters<CallTransportEvents[K]>) => {
    for (const fn of listeners[event]) (fn as (...a: Parameters<CallTransportEvents[K]>) => void)(...args);
  };

  let room: Room | null = null;
  let leaving = false;
  let listening = true;
  let screen: MediaStream | null = null;
  /** After one overload DeepFilterNet3 is not tried again in this tab. */
  const noiseMemory = { overloaded: false };
  const watching = new Set<string>();
  const screens = new Map<string, HTMLVideoElement>();
  const sounds = document.createElement("div");
  sounds.hidden = true;

  const playAudio = (track: RemoteTrack) => {
    if (track.kind !== Track.Kind.Audio) return;
    sounds.append(track.attach());
    if (track instanceof RemoteAudioTrack) track.setVolume(listening ? 1 : 0);
  };

  const sharing = (r: Room) =>
    [...r.remoteParticipants.values()]
      .filter((p) => p.getTrackPublication(Track.Source.ScreenShare) !== undefined)
      .map((p) => p.identity);

  /** Subscribes what this participant's publication should be received: microphones, and watched screens. */
  const follow = (pub: RemoteTrackPublication, participant: RemoteParticipant) => {
    if (pub.source === Track.Source.Microphone) pub.setSubscribed(true);
    else if (SCREEN.has(pub.source)) pub.setSubscribed(watching.has(participant.identity));
  };

  /** Puts the chosen noise suppression on a new microphone track, and says what runs. */
  const cleanMicrophone = async (track: LocalAudioTrack) => {
    const { noise } = prefs();
    if (noise !== "enhanced") {
      emit("noise", { running: noise });
      return;
    }
    if (track.getProcessor()) return;
    try {
      await track.setProcessor(
        createNoiseProcessor({
          engines: noiseEngines,
          report: (status) => {
            emit("noise", status);
          },
          memory: noiseMemory,
        }),
      );
    } catch {
      emit("noise", { running: await browserNoiseSuppression(track.mediaStreamTrack), reason: "failed" });
    }
  };

  const showScreen = (participant: RemoteParticipant) => {
    const el = screens.get(participant.identity);
    const track = participant.getTrackPublication(Track.Source.ScreenShare)?.videoTrack;
    if (el && track) track.attach(el);
  };

  const wire = (r: Room) => {
    r.on(RoomEvent.ConnectionStateChanged, (state) => {
      const link = LINK[state];
      if (link) emit("link", link);
    })
      .on(RoomEvent.Disconnected, (reason) => {
        // The server closed the room: forget it, so nothing touches the microphone or the screen after.
        if (!leaving && room === r) {
          room = null;
          stopCapture();
          sounds.replaceChildren();
          sounds.remove();
          watching.clear();
        }
        emit("link", "disconnected", leaving ? "left" : endCause(reason));
      })
      .on(RoomEvent.TrackPublished, (pub, participant) => {
        follow(pub, participant);
        emit("streams", sharing(r));
      })
      .on(RoomEvent.TrackUnpublished, () => {
        emit("streams", sharing(r));
      })
      .on(RoomEvent.ParticipantDisconnected, () => {
        emit("streams", sharing(r));
      })
      .on(RoomEvent.TrackSubscribed, (track, pub, participant) => {
        playAudio(track);
        if (pub.source === Track.Source.ScreenShare) showScreen(participant);
      })
      .on(RoomEvent.TrackUnsubscribed, (track) => {
        for (const el of track.detach()) if (sounds.contains(el)) el.remove();
      })
      .on(RoomEvent.ActiveSpeakersChanged, (speakers: Participant[]) => {
        emit(
          "speaking",
          speakers.map((p) => p.identity),
        );
      })
      .on(RoomEvent.LocalTrackPublished, (pub: LocalTrackPublication) => {
        if (pub.source === Track.Source.Microphone && pub.track instanceof LocalAudioTrack) {
          void cleanMicrophone(pub.track);
        }
      })
      .on(RoomEvent.LocalTrackUnpublished, (pub: LocalTrackPublication) => {
        if (pub.source !== Track.Source.ScreenShare || screen === null) return;
        const stream = screen;
        // A full reconnect unpublishes every track and publishes it again; the server taking it down does not.
        setTimeout(() => {
          if (screen !== stream || r.localParticipant.getTrackPublication(Track.Source.ScreenShare)) return;
          stopCapture();
          emit("screenEnded");
        }, REPUBLISH_MS);
      })
      .on(RoomEvent.MediaDevicesError, (e) => {
        if (isMicError(e)) emit("micDenied");
      });
  };

  /** Stops the captured screen without touching the room. */
  const stopCapture = () => {
    const stream = screen;
    screen = null;
    for (const t of stream?.getTracks() ?? []) t.stop();
  };

  const unpublishScreen = async (r: Room) => {
    const stream = screen;
    screen = null;
    try {
      for (const pub of [...r.localParticipant.trackPublications.values()]) {
        if (pub.track && SCREEN.has(pub.source)) await r.localParticipant.unpublishTrack(pub.track);
      }
    } catch (e) {
      // The room is going away; the capture must stop all the same.
      console.warn("voice: the screen share could not be unpublished", e);
    } finally {
      for (const t of stream?.getTracks() ?? []) t.stop();
    }
  };

  return {
    async connect(url, token) {
      await this.disconnect();
      leaving = false;
      const r = new Room(roomOptions());
      room = r;
      document.body.append(sounds);
      wire(r);
      await r.connect(url, token, connectOptions());
      for (const participant of r.remoteParticipants.values()) {
        for (const pub of participant.trackPublications.values()) follow(pub, participant);
      }
      emit("streams", sharing(r));
      // Joining is a click, so playback is allowed; this resumes it if a browser held it back.
      await r.startAudio().catch(() => undefined);
    },

    async disconnect() {
      const r = room;
      if (!r) return;
      room = null;
      leaving = true;
      await unpublishScreen(r).catch(() => undefined);
      await r.disconnect();
      sounds.replaceChildren();
      sounds.remove();
      watching.clear();
    },

    async setMicrophone(on) {
      if (!room) return;
      try {
        await room.localParticipant.setMicrophoneEnabled(on, microphoneOptions(prefs()));
      } catch (e) {
        // Callers do not wait for the microphone: nothing may escape as an unhandled rejection.
        if (isMicError(e)) emit("micDenied");
        else console.warn("voice: the microphone could not be switched", e);
      }
    },

    setListening(on) {
      listening = on;
      for (const participant of room?.remoteParticipants.values() ?? []) {
        for (const pub of participant.audioTrackPublications.values()) {
          if (pub.audioTrack instanceof RemoteAudioTrack) pub.audioTrack.setVolume(on ? 1 : 0);
        }
      }
    },

    async startScreenShare(): Promise<ScreenShareResult> {
      const r = room;
      if (!r) return { ok: false, reason: "failed" };
      const { capture, publish } = screenShareOptions(currentBrowser(), canRestrictOwnAudio());
      let stream: MediaStream;
      try {
        stream = await navigator.mediaDevices.getDisplayMedia(capture);
      } catch (e) {
        const cancelled = e instanceof DOMException && e.name === "NotAllowedError";
        return { ok: false, reason: cancelled ? "cancelled" : "failed" };
      }
      const [video] = stream.getVideoTracks();
      const [audio] = stream.getAudioTracks();
      // The call ended while the picker was open.
      if (!video || room !== r) {
        for (const t of stream.getTracks()) t.stop();
        return { ok: false, reason: "failed" };
      }
      video.contentHint = SCREEN_CONTENT_HINT;
      screen = stream;
      // The browser's own "Stop sharing" ends the track.
      video.addEventListener("ended", () => {
        if (screen !== stream) return;
        void unpublishScreen(r).then(() => {
          emit("screenEnded");
        });
      });
      try {
        await r.localParticipant.publishTrack(video, { ...publish, source: Track.Source.ScreenShare });
        if (audio) await r.localParticipant.publishTrack(audio, { source: Track.Source.ScreenShareAudio });
      } catch {
        await unpublishScreen(r).catch(() => undefined);
        for (const t of stream.getTracks()) t.stop();
        return { ok: false, reason: "failed" };
      }
      const surface = video.getSettings().displaySurface;
      return {
        ok: true,
        surface: surface === "monitor" || surface === "window" || surface === "browser" ? surface : "unknown",
        audio: audio !== undefined,
      };
    },

    async stopScreenShare() {
      if (room) await unpublishScreen(room);
    },

    watch(userId, on) {
      if (on) watching.add(userId);
      else watching.delete(userId);
      const participant = room?.remoteParticipants.get(userId);
      if (!participant) return;
      for (const pub of participant.trackPublications.values()) if (SCREEN.has(pub.source)) pub.setSubscribed(on);
    },

    attachScreen(userId, el) {
      screens.set(userId, el);
      const participant = room?.remoteParticipants.get(userId);
      if (participant) showScreen(participant);
      return () => {
        if (screens.get(userId) === el) screens.delete(userId);
        room?.remoteParticipants.get(userId)?.getTrackPublication(Track.Source.ScreenShare)?.videoTrack?.detach(el);
      };
    },

    attachOwnScreen(el) {
      // The captured stream itself, video only: the preview must not echo the shared sound.
      const video = screen?.getVideoTracks()[0];
      if (video) el.srcObject = new MediaStream([video]);
      return () => {
        el.srcObject = null;
      };
    },

    async useDevice(kind, deviceId) {
      // "default" follows the system's choice; an exact match would pin today's default device.
      const switched = await room?.switchActiveDevice(kind, deviceId, deviceId !== "default");
      if (switched === false) throw new Error(`voice: could not switch ${kind} to ${deviceId}`);
    },

    on(event, fn) {
      listeners[event].add(fn);
      return () => {
        listeners[event].delete(fn);
      };
    },
  };
};
