import {
  ConnectionState,
  createLocalVideoTrack,
  LocalAudioTrack,
  type LocalTrackPublication,
  LocalVideoTrack,
  type Participant,
  RemoteAudioTrack,
  type RemoteParticipant,
  type RemoteTrack,
  type RemoteTrackPublication,
  Room,
  RoomEvent,
  Track,
} from "livekit-client";
import type { CallLink, CallTransport, CallTransportEvents, CameraResult, ScreenShareResult } from "@/shared/lib";
import type { VoicePrefs } from "../model/prefs";
import { canRestrictOwnAudio, currentBrowser } from "./browser";
import {
  cameraOptions,
  connectOptions,
  endCause,
  microphoneOptions,
  roomOptions,
  SCREEN_CONTENT_HINT,
  screenShareOptions,
} from "./media";
import { browserNoiseSuppression, createNoiseProcessor } from "./noise";
import { noiseEngines } from "./noise-engines";
import { steerScreenQuality } from "./screen-quality";
import { createSpeakingDetector, type SpeakingDetector } from "./speaking";

type Listeners = { [K in keyof CallTransportEvents]: Set<CallTransportEvents[K]> };

const LINK: Partial<Record<ConnectionState, CallLink>> = {
  [ConnectionState.Connecting]: "connecting",
  [ConnectionState.Connected]: "connected",
  [ConnectionState.Reconnecting]: "reconnecting",
  [ConnectionState.SignalReconnecting]: "reconnecting",
};

const SCREEN = new Set<Track.Source>([Track.Source.ScreenShare, Track.Source.ScreenShareAudio]);

/**
 * The desktop client captures the screen natively and shares it as a
 * second participant beside the person's own, `<user id>.screen`, which
 * publishes only the screen share (the server allows it while the person is
 * in the call). Here it is that person: their screen share, their tile,
 * their screen share volume.
 */
const SCREEN_IDENTITY = ".screen";

/** The user a participant is: its identity, or the owner of a `.screen` one. */
const userOf = (identity: string): string =>
  identity.endsWith(SCREEN_IDENTITY) ? identity.slice(0, -SCREEN_IDENTITY.length) : identity;

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

/** Why getUserMedia gave no camera, as the user is told. */
const cameraFailure = (e: unknown): CameraResult => {
  const name = e instanceof DOMException ? e.name : "";
  if (name === "NotAllowedError" || name === "SecurityError") return { ok: false, reason: "denied" };
  if (name === "NotFoundError" || name === "OverconstrainedError") return { ok: false, reason: "missing" };
  return { ok: false, reason: "failed" };
};

/**
 * The call over LiveKit. Microphones are received as soon as they are
 * published; screen shares only while watched. Remote sound plays through
 * hidden audio elements, muted all at once when deafened. A screen share is
 * captured here (getDisplayMedia, for `restrictOwnAudio`) and published as
 * the ScreenShare sources. Cameras are received only while a tile shows
 * them, each in the layer that fits its tile.
 */
export const createLiveKitTransport = (prefs: () => Pick<VoicePrefs, "inputId" | "noise">): CallTransport => {
  const listeners: Listeners = {
    link: new Set(),
    speaking: new Set(),
    streams: new Set(),
    micDenied: new Set(),
    screenEnded: new Set(),
    cameras: new Set(),
    cameraEnded: new Set(),
    noise: new Set(),
  };
  const emit = <K extends keyof CallTransportEvents>(event: K, ...args: Parameters<CallTransportEvents[K]>) => {
    for (const fn of listeners[event]) (fn as (...a: Parameters<CallTransportEvents[K]>) => void)(...args);
  };

  let room: Room | null = null;
  let leaving = false;
  let listening = true;
  let screen: MediaStream | null = null;
  let camera: LocalVideoTrack | null = null;
  /** Stops the 1080p/720p steering of the published screen share. */
  let stopSteering = () => {};
  /** After one overload DeepFilterNet3 is not tried again in this tab. */
  const noiseMemory = { overloaded: false };
  const watching = new Set<string>();
  const screens = new Map<string, HTMLVideoElement>();
  /** The tiles showing others' cameras, and what stops following each tile's size. */
  const cameraViews = new Map<string, { el: HTMLVideoElement; stop: () => void }>();
  const sounds = document.createElement("div");
  sounds.hidden = true;

  /** How loud each person's voice and screen share is, 0–2; one never set is 1. */
  const volumes = { voice: new Map<string, number>(), stream: new Map<string, number>() };
  /** The speakers chosen in the settings, for the sound that goes through `boost`. */
  let outputId: string | null = null;
  /**
   * Carries the tracks turned up past 1: an audio element cannot play louder
   * than it was sent, a gain node can. Made with the first such track, so a
   * call where nobody is turned up plays as before, through elements alone.
   */
  let boost: AudioContext | null = null;

  /** Sends the boosted sound to the chosen speakers, where the browser can. */
  const routeBoost = () => {
    // Not every browser can choose the speakers of an audio context (Firefox): there it plays on the default ones.
    const ctx: (AudioContext & { setSinkId?: (id: string) => Promise<void> }) | null = boost;
    if (ctx?.setSinkId === undefined || outputId === null) return;
    void ctx.setSinkId(outputId === "default" ? "" : outputId).catch(() => undefined);
  };

  const boostContext = (): AudioContext | null => {
    if (boost === null && typeof AudioContext !== "undefined") {
      boost = new AudioContext();
      routeBoost();
    }
    // A context made outside a click starts suspended; the next click resumes it.
    if (boost?.state === "suspended") void boost.resume().catch(() => undefined);
    return boost;
  };

  const dropBoost = () => {
    void boost?.close().catch(() => undefined);
    boost = null;
  };

  /** Plays `track` of `userId` at its volume, or silent while deafened. */
  const applyVolume = (track: RemoteAudioTrack, userId: string, source: Track.Source) => {
    const kind = source === Track.Source.ScreenShareAudio ? "stream" : "voice";
    const wanted = listening ? (volumes[kind].get(userId) ?? 1) : 0;
    const ctx = wanted > 1 ? boostContext() : null;
    if (ctx !== null) {
      track.setAudioContext(ctx);
      track.setVolume(wanted);
      return;
    }
    // The volume first: the elements take it over when the track leaves the gain node.
    track.setVolume(Math.min(wanted, 1));
    track.setAudioContext(undefined);
  };

  const playAudio = (track: RemoteTrack, pub: RemoteTrackPublication, participant: RemoteParticipant) => {
    if (track.kind !== Track.Kind.Audio) return;
    sounds.append(track.attach());
    if (track instanceof RemoteAudioTrack) applyVolume(track, userOf(participant.identity), pub.source);
  };

  /** Applies the volumes again to everything `participant` plays. */
  const applyVolumes = (participant: RemoteParticipant) => {
    for (const pub of participant.audioTrackPublications.values()) {
      if (pub.audioTrack instanceof RemoteAudioTrack) {
        applyVolume(pub.audioTrack, userOf(participant.identity), pub.source);
      }
    }
  };

  /** `userId`'s participants in the room: their own and the one their desktop client shares through. */
  const participantsOf = (userId: string): RemoteParticipant[] =>
    [room?.remoteParticipants.get(userId), room?.remoteParticipants.get(userId + SCREEN_IDENTITY)].filter(
      (p): p is RemoteParticipant => p !== undefined,
    );

  const sharing = (r: Room) => [
    ...new Set(
      [...r.remoteParticipants.values()]
        .filter((p) => p.getTrackPublication(Track.Source.ScreenShare) !== undefined)
        .map((p) => userOf(p.identity)),
    ),
  ];

  const filming = (r: Room) =>
    [...r.remoteParticipants.values()]
      .filter((p) => p.getTrackPublication(Track.Source.Camera) !== undefined)
      .map((p) => p.identity);

  /** Tells the app who shares a screen and who has a camera on. */
  const announce = (r: Room) => {
    emit("streams", sharing(r));
    emit("cameras", filming(r));
  };

  /** Subscribes what this participant's publication should be received: microphones, watched screens, shown cameras. */
  const follow = (pub: RemoteTrackPublication, participant: RemoteParticipant) => {
    if (pub.source === Track.Source.Microphone) pub.setSubscribed(true);
    else if (SCREEN.has(pub.source)) pub.setSubscribed(watching.has(userOf(participant.identity)));
    else if (pub.source === Track.Source.Camera) pub.setSubscribed(cameraViews.has(participant.identity));
  };

  /** The noise suppression each microphone track runs now, so a switch mid-call changes only what differs. */
  const noiseOf = new WeakMap<LocalAudioTrack, VoicePrefs["noise"]>();

  /** Puts the chosen noise suppression on a new microphone track, and says what runs. */
  const cleanMicrophone = async (track: LocalAudioTrack) => {
    const { noise } = prefs();
    noiseOf.set(track, noise);
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

  /**
   * Brings the published microphone to the noise suppression chosen now:
   * the setting changes during a call without joining again. The browser's
   * own processing follows microphoneOptions; «Улучшенное» adds the processor.
   */
  const syncNoise = async (r: Room) => {
    const track = r.localParticipant.getTrackPublication(Track.Source.Microphone)?.track;
    if (!(track instanceof LocalAudioTrack)) return;
    const { noise } = prefs();
    if (noiseOf.get(track) === noise) return;
    noiseOf.set(track, noise);
    if (noise !== "enhanced" && track.getProcessor()) await track.stopProcessor().catch(() => undefined);
    const input = track.mediaStreamTrack;
    try {
      await input.applyConstraints({
        ...input.getConstraints(),
        autoGainControl: noise !== "enhanced",
        noiseSuppression: noise === "standard",
      });
    } catch {
      // The browser keeps the track as it was.
    }
    if (noise === "enhanced") {
      noiseOf.delete(track);
      await cleanMicrophone(track);
    } else {
      emit("noise", { running: input.getSettings().noiseSuppression === true ? "standard" : "off" });
    }
  };

  const showScreen = (participant: RemoteParticipant) => {
    const el = screens.get(userOf(participant.identity));
    const track = participant.getTrackPublication(Track.Source.ScreenShare)?.videoTrack;
    if (el && track) track.attach(el);
  };

  const showCamera = (participant: RemoteParticipant) => {
    const view = cameraViews.get(participant.identity);
    const track = participant.getTrackPublication(Track.Source.Camera)?.videoTrack;
    if (view && track) track.attach(view.el);
  };

  /**
   * Receives `participant`'s camera for a tile, in the layer that fits the
   * tile as it is now and each time it changes size. Returns what stops
   * following the size.
   */
  const subscribeCamera = (participant: RemoteParticipant, el: HTMLVideoElement): (() => void) => {
    const pub = participant.getTrackPublication(Track.Source.Camera);
    if (!pub) return () => {};
    pub.setSubscribed(true);
    const fitToTile = () => {
      if (el.clientWidth > 0 && el.clientHeight > 0) {
        pub.setVideoDimensions({ width: el.clientWidth, height: el.clientHeight });
      }
    };
    fitToTile();
    if (typeof ResizeObserver === "undefined") return () => {};
    const observer = new ResizeObserver(fitToTile);
    observer.observe(el);
    return () => {
      observer.disconnect();
    };
  };

  /** Who speaks, measured here (lib/speaking); null without Web Audio, when the server tells instead. */
  let detector: SpeakingDetector | null = null;
  const stopDetector = () => {
    detector?.stop();
    detector = null;
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
          stopCameraCapture();
          dropCameraViews();
          sounds.replaceChildren();
          sounds.remove();
          watching.clear();
          dropBoost();
          stopDetector();
        }
        emit("link", "disconnected", leaving ? "left" : endCause(reason));
      })
      .on(RoomEvent.TrackPublished, (pub, participant) => {
        const view = cameraViews.get(participant.identity);
        // A tile that waited for this camera (it was turned on, or came back after a reconnect).
        if (pub.source === Track.Source.Camera && view) {
          view.stop();
          view.stop = subscribeCamera(participant, view.el);
        } else follow(pub, participant);
        announce(r);
      })
      .on(RoomEvent.TrackUnpublished, () => {
        announce(r);
      })
      .on(RoomEvent.ParticipantDisconnected, (participant) => {
        detector?.unwatch(participant.identity);
        announce(r);
      })
      .on(RoomEvent.TrackSubscribed, (track, pub, participant) => {
        playAudio(track, pub, participant);
        if (pub.source === Track.Source.Microphone) detector?.watch(participant.identity, track.mediaStreamTrack);
        if (pub.source === Track.Source.ScreenShare) showScreen(participant);
        if (pub.source === Track.Source.Camera) showCamera(participant);
      })
      .on(RoomEvent.TrackUnsubscribed, (track, pub, participant) => {
        if (pub.source === Track.Source.Microphone) detector?.unwatch(participant.identity);
        for (const el of track.detach()) if (sounds.contains(el)) el.remove();
      })
      .on(RoomEvent.ActiveSpeakersChanged, (speakers: Participant[]) => {
        // The server's answer comes late (see lib/speaking): only while our own measure does not run.
        if (detector?.live()) return;
        emit(
          "speaking",
          speakers.map((p) => p.identity),
        );
      })
      .on(RoomEvent.LocalTrackPublished, (pub: LocalTrackPublication) => {
        if (pub.source === Track.Source.Microphone && pub.track instanceof LocalAudioTrack) {
          void cleanMicrophone(pub.track);
          detector?.watch(r.localParticipant.identity, pub.track.mediaStreamTrack);
        }
        // Also after a reconnect, which publishes the share again on a new sender.
        const sender = pub.track instanceof LocalVideoTrack ? pub.track.sender : undefined;
        if (pub.source === Track.Source.ScreenShare && sender) {
          stopSteering();
          stopSteering = steerScreenQuality(sender);
        }
      })
      .on(RoomEvent.LocalTrackUnpublished, (pub: LocalTrackPublication) => {
        if (pub.source === Track.Source.Microphone) detector?.unwatch(r.localParticipant.identity);
        if (pub.source === Track.Source.Camera && camera !== null) {
          const track = camera;
          // As for a screen: a reconnect publishes it again, the server taking it down does not.
          setTimeout(() => {
            if (camera !== track || r.localParticipant.getTrackPublication(Track.Source.Camera)) return;
            stopCameraCapture();
            emit("cameraEnded");
          }, REPUBLISH_MS);
          return;
        }
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
    stopSteering();
    const stream = screen;
    screen = null;
    for (const t of stream?.getTracks() ?? []) t.stop();
  };

  /** Turns the camera off without touching the room. */
  const stopCameraCapture = () => {
    const track = camera;
    camera = null;
    track?.stop();
  };

  const unpublishCamera = async (r: Room) => {
    const track = camera;
    camera = null;
    try {
      if (track) await r.localParticipant.unpublishTrack(track);
    } catch (e) {
      console.warn("voice: the camera could not be unpublished", e);
    } finally {
      track?.stop();
    }
  };

  /** Lets go of every camera tile: nothing is received any more. */
  const dropCameraViews = () => {
    for (const view of cameraViews.values()) view.stop();
    cameraViews.clear();
  };

  const unpublishScreen = async (r: Room) => {
    stopSteering();
    const stream = screen;
    screen = null;
    // The capture ends at once; taking the tracks off the room may wait on the network.
    for (const t of stream?.getTracks() ?? []) t.stop();
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
      stopDetector();
      detector = createSpeakingDetector((ids) => {
        if (room === r) emit("speaking", ids);
      });
      wire(r);
      await r.connect(url, token, connectOptions());
      for (const participant of r.remoteParticipants.values()) {
        for (const pub of participant.trackPublications.values()) follow(pub, participant);
      }
      announce(r);
      // Joining is a click, so playback is allowed; this resumes it if a browser held it back.
      await r.startAudio().catch(() => undefined);
    },

    async disconnect() {
      const r = room;
      if (!r) return;
      room = null;
      leaving = true;
      await unpublishScreen(r).catch(() => undefined);
      await unpublishCamera(r);
      await r.disconnect();
      sounds.replaceChildren();
      sounds.remove();
      watching.clear();
      dropCameraViews();
      dropBoost();
      stopDetector();
    },

    async setMicrophone(on) {
      const r = room;
      if (!r) return;
      try {
        await r.localParticipant.setMicrophoneEnabled(on, microphoneOptions(prefs()));
        if (room === r) await syncNoise(r);
      } catch (e) {
        // Callers do not wait for the microphone: nothing may escape as an unhandled rejection.
        if (isMicError(e)) emit("micDenied");
        else console.warn("voice: the microphone could not be switched", e);
      }
    },

    setListening(on) {
      listening = on;
      for (const participant of room?.remoteParticipants.values() ?? []) applyVolumes(participant);
    },

    setVolume(userId, source, volume) {
      volumes[source].set(userId, Math.min(2, Math.max(0, volume)));
      for (const participant of participantsOf(userId)) applyVolumes(participant);
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
      // A stop (the browser's button, the user, the call ending) may come while a track is being
      // published: then this share is over, and whatever got published comes down again.
      const stopped = async (): Promise<boolean> => {
        if (screen === stream) return false;
        if (room === r) await unpublishScreen(r).catch(() => undefined);
        for (const t of stream.getTracks()) t.stop();
        return true;
      };
      try {
        await r.localParticipant.publishTrack(video, { ...publish, source: Track.Source.ScreenShare });
        if (await stopped()) return { ok: false, reason: "cancelled" };
        if (audio) await r.localParticipant.publishTrack(audio, { source: Track.Source.ScreenShareAudio });
        if (await stopped()) return { ok: false, reason: "cancelled" };
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
      for (const participant of participantsOf(userId)) {
        for (const pub of participant.trackPublications.values()) if (SCREEN.has(pub.source)) pub.setSubscribed(on);
      }
    },

    attachScreen(userId, el) {
      screens.set(userId, el);
      for (const participant of participantsOf(userId)) showScreen(participant);
      return () => {
        if (screens.get(userId) === el) screens.delete(userId);
        for (const participant of participantsOf(userId)) {
          participant.getTrackPublication(Track.Source.ScreenShare)?.videoTrack?.detach(el);
        }
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

    async startCamera(): Promise<CameraResult> {
      const r = room;
      if (!r) return { ok: false, reason: "failed" };
      if (camera) return { ok: true };
      const { capture, publish } = cameraOptions();
      let track: LocalVideoTrack;
      try {
        track = await createLocalVideoTrack(capture);
      } catch (e) {
        return cameraFailure(e);
      }
      // The call ended while the browser asked. (One request at a time is the session's: startCamera.)
      if (room !== r) {
        track.stop();
        return { ok: false, reason: "failed" };
      }
      camera = track;
      // Unplugged, or taken away by the system.
      track.mediaStreamTrack.addEventListener("ended", () => {
        if (camera !== track) return;
        void unpublishCamera(r).then(() => {
          emit("cameraEnded");
        });
      });
      try {
        await r.localParticipant.publishTrack(track, { ...publish, source: Track.Source.Camera });
      } catch {
        // Also what a voice token that does not allow the camera gives.
        await unpublishCamera(r);
        return { ok: false, reason: "failed" };
      }
      return { ok: true };
    },

    async stopCamera() {
      if (room) await unpublishCamera(room);
      else stopCameraCapture();
    },

    attachCamera(userId, el) {
      const participant = room?.remoteParticipants.get(userId);
      const view = { el, stop: () => {} };
      cameraViews.set(userId, view);
      if (participant) {
        view.stop = subscribeCamera(participant, el);
        showCamera(participant);
      }
      return () => {
        view.stop();
        if (cameraViews.get(userId) !== view) return;
        cameraViews.delete(userId);
        const pub = room?.remoteParticipants.get(userId)?.getTrackPublication(Track.Source.Camera);
        pub?.videoTrack?.detach(el);
        pub?.setSubscribed(false);
      };
    },

    attachOwnCamera(el) {
      if (camera) el.srcObject = new MediaStream([camera.mediaStreamTrack]);
      return () => {
        el.srcObject = null;
      };
    },

    async useDevice(kind, deviceId) {
      // "default" follows the system's choice; an exact match would pin today's default device.
      const switched = await room?.switchActiveDevice(kind, deviceId, deviceId !== "default");
      if (switched === false) throw new Error(`voice: could not switch ${kind} to ${deviceId}`);
      if (kind === "audiooutput") {
        outputId = deviceId;
        routeBoost();
      }
    },

    on(event, fn) {
      listeners[event].add(fn);
      return () => {
        listeners[event].delete(fn);
      };
    },
  };
};
