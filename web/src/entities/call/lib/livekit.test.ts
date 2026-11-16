import { DisconnectReason, LocalAudioTrack, RemoteAudioTrack, RoomEvent, Track } from "livekit-client";
import type { Mock } from "vitest";
import { createLiveKitTransport } from "./livekit";

interface FakeRoom {
  publications: Map<string, { source: Track.Source; track: unknown }>;
  localParticipant: {
    publishTrack: ReturnType<typeof vi.fn>;
    setMicrophoneEnabled: ReturnType<typeof vi.fn>;
  };
  switchActiveDevice: ReturnType<typeof vi.fn>;
  emit: (event: string, ...args: unknown[]) => void;
}

/** The rooms the transport made, newest last. */
const made = vi.hoisted(() => ({ rooms: [] as unknown[], camera: vi.fn() }));

// A stand-in for livekit-client's Room: what livekit.ts calls, and events fired by hand.
vi.mock("livekit-client", async (original) => {
  const real = await original<typeof import("livekit-client")>();
  class Room {
    readonly handlers = new Map<string, ((...args: unknown[]) => void)[]>();
    readonly remoteParticipants = new Map();
    readonly publications = new Map<string, { source: string; track: unknown }>();
    readonly localParticipant = {
      trackPublications: this.publications,
      publishTrack: vi.fn((track: unknown, opts: { source: string }) => {
        this.publications.set(opts.source, { source: opts.source, track });
        return Promise.resolve();
      }),
      unpublishTrack: vi.fn(() => Promise.resolve()),
      setMicrophoneEnabled: vi.fn(() => Promise.resolve()),
      getTrackPublication: (source: string) => this.publications.get(source),
    };
    switchActiveDevice = vi.fn(() => Promise.resolve(true));
    startAudio = vi.fn(() => Promise.resolve());
    connect = vi.fn(() => Promise.resolve());
    disconnect = vi.fn(() => {
      this.emit(real.RoomEvent.Disconnected, real.DisconnectReason.CLIENT_INITIATED);
      return Promise.resolve();
    });
    constructor() {
      made.rooms.push(this);
    }
    on(event: string, fn: (...args: unknown[]) => void) {
      this.handlers.set(event, [...(this.handlers.get(event) ?? []), fn]);
      return this;
    }
    emit(event: string, ...args: unknown[]) {
      for (const fn of this.handlers.get(event) ?? []) fn(...args);
    }
  }
  return { ...real, Room, createLocalVideoTrack: made.camera };
});

const room = (): FakeRoom => {
  const last = made.rooms.at(-1);
  if (!last) throw new Error("no room");
  return last as FakeRoom;
};

/** A captured screen: a video track (and no sound) whose stop is watched. */
const capture = () => {
  const video = {
    kind: "video",
    contentHint: "",
    readyState: "live",
    stop: vi.fn(),
    addEventListener: vi.fn(),
    getSettings: () => ({ displaySurface: "browser" }),
  };
  const stream = { getVideoTracks: () => [video], getAudioTracks: () => [], getTracks: () => [video] };
  return { video, stream };
};

const pickScreen = (result: Promise<unknown>) => {
  Object.defineProperty(navigator, "mediaDevices", {
    configurable: true,
    value: { getDisplayMedia: () => result, getSupportedConstraints: () => ({}) },
  });
};

afterEach(() => {
  made.rooms.length = 0;
  Reflect.deleteProperty(navigator, "mediaDevices");
  vi.useRealTimers();
});

const connected = async () => {
  const t = createLiveKitTransport(() => ({ inputId: null, noise: "standard" }));
  await t.connect("ws://localhost", "token");
  return t;
};

describe("LiveKit transport", () => {
  it("I9: forgets a room the server closed, so the microphone is not touched after", async () => {
    const t = await connected();
    room().emit(RoomEvent.Disconnected, DisconnectReason.PARTICIPANT_REMOVED);
    await t.setMicrophone(true);
    expect(room().localParticipant.setMicrophoneEnabled).not.toHaveBeenCalled();
  });

  it("I7: switches to the system default without an exact device match", async () => {
    const t = await connected();
    await t.useDevice("audioinput", "default");
    await t.useDevice("audioinput", "mic-1");
    expect(room().switchActiveDevice.mock.calls).toEqual([
      ["audioinput", "default", false],
      ["audioinput", "mic-1", true],
    ]);
  });

  it("I6: stops the capture when publishing it fails", async () => {
    const t = await connected();
    const { video, stream } = capture();
    pickScreen(Promise.resolve(stream));
    room().localParticipant.publishTrack.mockRejectedValueOnce(new Error("403"));
    expect(await t.startScreenShare()).toEqual({ ok: false, reason: "failed" });
    expect(video.stop).toHaveBeenCalled();
  });

  it("I6: stops a capture picked after the call ended", async () => {
    const t = await connected();
    const { video, stream } = capture();
    let pick: (s: unknown) => void = () => {};
    pickScreen(
      new Promise((resolve) => {
        pick = resolve;
      }),
    );
    const sharing = t.startScreenShare();
    await t.disconnect();
    pick(stream);
    expect(await sharing).toEqual({ ok: false, reason: "failed" });
    expect(video.stop).toHaveBeenCalled();
  });

  it("I6: a stop while the screen is being published ends the share instead of reporting it started", async () => {
    const t = await connected();
    const ended = vi.fn();
    t.on("screenEnded", ended);
    const { video, stream } = capture();
    pickScreen(Promise.resolve(stream));
    const lp = room().localParticipant;
    let published: () => void = () => {};
    const inFlight = new Promise<void>((resolve) => {
      published = resolve;
    });
    // The track is on the room already; LiveKit has not answered yet.
    room().publications.set(Track.Source.ScreenShare, { source: Track.Source.ScreenShare, track: video });
    lp.publishTrack.mockReturnValueOnce(inFlight);
    const sharing = t.startScreenShare();
    await vi.waitFor(() => {
      expect(video.addEventListener).toHaveBeenCalled();
    });
    // The browser's own «Stop sharing», while the publish is still in flight.
    const onEnded = video.addEventListener.mock.calls.find(([type]) => type === "ended")?.[1] as () => void;
    onEnded();
    expect(video.stop).toHaveBeenCalled();
    published();
    expect(await sharing).toEqual({ ok: false, reason: "cancelled" });
    const unpublish = (lp as unknown as { unpublishTrack: Mock }).unpublishTrack;
    expect(unpublish).toHaveBeenCalledWith(video);
    await vi.waitFor(() => {
      expect(ended).toHaveBeenCalledTimes(1);
    });
  });

  it("I4: keeps sharing through a reconnect that republishes the screen", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const t = await connected();
    const ended = vi.fn();
    t.on("screenEnded", ended);
    const { stream } = capture();
    pickScreen(Promise.resolve(stream));
    await t.startScreenShare();
    const pub = room().publications.get(Track.Source.ScreenShare);
    // LiveKit's full reconnect unpublishes, then publishes again.
    room().emit(RoomEvent.LocalTrackUnpublished, pub);
    await vi.advanceTimersByTimeAsync(2000);
    expect(ended).not.toHaveBeenCalled();
  });

  it("I4: ends the share when the server takes it down for good", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const t = await connected();
    const ended = vi.fn();
    t.on("screenEnded", ended);
    const { stream } = capture();
    pickScreen(Promise.resolve(stream));
    await t.startScreenShare();
    const pub = room().publications.get(Track.Source.ScreenShare);
    room().publications.delete(Track.Source.ScreenShare);
    room().emit(RoomEvent.LocalTrackUnpublished, pub);
    await vi.advanceTimersByTimeAsync(2000);
    expect(ended).toHaveBeenCalledTimes(1);
  });
});

describe("the microphone's noise suppression", () => {
  /** A microphone track as LiveKit publishes it, its processor watched. */
  const micTrack = () => {
    const track = Object.create(LocalAudioTrack.prototype) as LocalAudioTrack;
    let processor: unknown;
    Object.assign(track, {
      getProcessor: () => processor,
      setProcessor: vi.fn((p: unknown) => {
        processor = p;
        return Promise.resolve();
      }),
      stopProcessor: vi.fn(() => {
        processor = undefined;
        return Promise.resolve();
      }),
    });
    let settings = { noiseSuppression: false };
    // A getter on LocalTrack: defined, not assigned.
    Object.defineProperty(track, "mediaStreamTrack", {
      value: {
        getConstraints: () => ({}),
        getSettings: () => settings,
        applyConstraints: vi.fn((c: { noiseSuppression: boolean }) => {
          settings = { noiseSuppression: c.noiseSuppression };
          return Promise.resolve();
        }),
      },
    });
    return track;
  };

  const setProcessorOf = (track: LocalAudioTrack) => (track as unknown as { setProcessor: Mock }).setProcessor;

  const publish = (track: LocalAudioTrack) => {
    room().emit(RoomEvent.LocalTrackPublished, { source: Track.Source.Microphone, track });
  };

  it("puts the enhanced processor on a new microphone, once", async () => {
    const t = createLiveKitTransport(() => ({ inputId: null, noise: "enhanced" }));
    await t.connect("ws://localhost", "token");
    const track = micTrack();
    publish(track);
    await vi.waitFor(() => {
      expect(setProcessorOf(track)).toHaveBeenCalledTimes(1);
    });
    publish(track);
    await Promise.resolve();
    expect(setProcessorOf(track)).toHaveBeenCalledTimes(1);
  });

  it("switches the published microphone when the setting changes mid-call", async () => {
    let noise: "enhanced" | "standard" | "off" = "enhanced";
    const t = createLiveKitTransport(() => ({ inputId: null, noise }));
    const said = vi.fn();
    t.on("noise", said);
    await t.connect("ws://localhost", "token");
    const track = micTrack();
    room().publications.set(Track.Source.Microphone, { source: Track.Source.Microphone, track });
    publish(track);
    await vi.waitFor(() => {
      expect(setProcessorOf(track)).toHaveBeenCalledTimes(1);
    });
    noise = "standard";
    await t.setMicrophone(true);
    expect((track as unknown as { stopProcessor: Mock }).stopProcessor).toHaveBeenCalledTimes(1);
    expect(track.mediaStreamTrack.getSettings().noiseSuppression).toBe(true);
    expect(said).toHaveBeenLastCalledWith({ running: "standard" });
    // Push-to-talk switches the microphone often: an unchanged setting touches nothing.
    await t.setMicrophone(false);
    expect((track.mediaStreamTrack as unknown as { applyConstraints: Mock }).applyConstraints).toHaveBeenCalledTimes(1);
    noise = "enhanced";
    await t.setMicrophone(true);
    expect(setProcessorOf(track)).toHaveBeenCalledTimes(2);
    expect(track.mediaStreamTrack.getSettings().noiseSuppression).toBe(false);
  });

  it("says the browser's mode runs when enhanced is not chosen", async () => {
    const t = createLiveKitTransport(() => ({ inputId: null, noise: "standard" }));
    const noise = vi.fn();
    t.on("noise", noise);
    await t.connect("ws://localhost", "token");
    const track = micTrack();
    publish(track);
    await vi.waitFor(() => {
      expect(noise).toHaveBeenCalledWith({ running: "standard" });
    });
    expect(setProcessorOf(track)).not.toHaveBeenCalled();
  });
});

describe("the second review's cases", () => {
  it("1: a busy or vanished microphone is reported, and nothing else escapes", async () => {
    const t = await connected();
    const denied = vi.fn();
    t.on("micDenied", denied);
    room().localParticipant.setMicrophoneEnabled.mockRejectedValueOnce(
      new DOMException("Could not start audio source", "NotReadableError"),
    );
    await t.setMicrophone(true);
    room().localParticipant.setMicrophoneEnabled.mockRejectedValueOnce(
      new DOMException("no such device", "OverconstrainedError"),
    );
    await t.setMicrophone(true);
    expect(denied).toHaveBeenCalledTimes(2);
    room().localParticipant.setMicrophoneEnabled.mockRejectedValueOnce(new Error("engine closed"));
    await expect(t.setMicrophone(true)).resolves.toBeUndefined();
  });

  it("7: the capture stops even when unpublishing it fails", async () => {
    const t = await connected();
    const { video, stream } = capture();
    pickScreen(Promise.resolve(stream));
    await t.startScreenShare();
    (room().localParticipant as unknown as { unpublishTrack: Mock }).unpublishTrack.mockRejectedValueOnce(
      new Error("signal closed"),
    );
    await expect(t.stopScreenShare()).resolves.toBeUndefined();
    expect(video.stop).toHaveBeenCalled();
  });

  it("9: a device the room could not switch to is an error", async () => {
    const t = await connected();
    room().switchActiveDevice.mockResolvedValueOnce(false);
    await expect(t.useDevice("audioinput", "mic-9")).rejects.toThrow();
  });
});

describe("the camera", () => {
  /** A camera track as createLocalVideoTrack gives it, its stop watched. */
  const cameraTrack = () => ({ mediaStreamTrack: { addEventListener: vi.fn() }, stop: vi.fn() });

  /** Someone in the room with a camera published, everything done to it watched. */
  const filmingParticipant = (identity: string) => {
    const pub = {
      source: Track.Source.Camera,
      setSubscribed: vi.fn(),
      setVideoDimensions: vi.fn(),
      videoTrack: { attach: vi.fn(), detach: vi.fn() },
    };
    const participant = {
      identity,
      trackPublications: new Map([[Track.Source.Camera, pub]]),
      getTrackPublication: (source: Track.Source) => (source === Track.Source.Camera ? pub : undefined),
    };
    (room() as unknown as { remoteParticipants: Map<string, unknown> }).remoteParticipants.set(identity, participant);
    return { pub, participant };
  };

  it("publishes the camera in three layers, and turning it off stops it", async () => {
    const t = await connected();
    const track = cameraTrack();
    made.camera.mockResolvedValueOnce(track);
    expect(await t.startCamera()).toEqual({ ok: true });
    const [published, opts] = room().localParticipant.publishTrack.mock.calls[0] as [unknown, Record<string, unknown>];
    expect(published).toBe(track);
    expect(opts).toMatchObject({ source: Track.Source.Camera, simulcast: true, videoCodec: "vp8" });
    await t.stopCamera();
    expect(track.stop).toHaveBeenCalled();
  });

  it.each([
    ["NotAllowedError", "denied"],
    ["NotFoundError", "missing"],
    ["NotReadableError", "failed"],
  ])("says why there is no camera (%s)", async (name, reason) => {
    const t = await connected();
    made.camera.mockRejectedValueOnce(new DOMException("no camera", name));
    expect(await t.startCamera()).toEqual({ ok: false, reason });
    expect(room().localParticipant.publishTrack).not.toHaveBeenCalled();
  });

  it("stops the camera when publishing it is refused", async () => {
    const t = await connected();
    const track = cameraTrack();
    made.camera.mockResolvedValueOnce(track);
    room().localParticipant.publishTrack.mockRejectedValueOnce(new Error("403"));
    expect(await t.startCamera()).toEqual({ ok: false, reason: "failed" });
    expect(track.stop).toHaveBeenCalled();
  });

  it("stops the camera when the call ends", async () => {
    const t = await connected();
    const track = cameraTrack();
    made.camera.mockResolvedValueOnce(track);
    await t.startCamera();
    await t.disconnect();
    expect(track.stop).toHaveBeenCalled();
  });

  it("receives someone's camera only while a tile shows it", async () => {
    const t = await connected();
    const cameras = vi.fn();
    t.on("cameras", cameras);
    const { pub, participant } = filmingParticipant("200");
    room().emit(RoomEvent.TrackPublished, pub, participant);
    expect(cameras).toHaveBeenLastCalledWith(["200"]);
    expect(pub.setSubscribed).toHaveBeenLastCalledWith(false);

    const el = document.createElement("video");
    const release = t.attachCamera("200", el);
    expect(pub.setSubscribed).toHaveBeenLastCalledWith(true);
    expect(pub.videoTrack.attach).toHaveBeenCalledWith(el);
    release();
    expect(pub.setSubscribed).toHaveBeenLastCalledWith(false);
    expect(pub.videoTrack.detach).toHaveBeenCalledWith(el);
  });

  it("asks for the layer that fits the tile", async () => {
    const t = await connected();
    const { pub } = filmingParticipant("200");
    const el = document.createElement("video");
    Object.defineProperties(el, { clientWidth: { value: 320 }, clientHeight: { value: 180 } });
    t.attachCamera("200", el);
    expect(pub.setVideoDimensions).toHaveBeenCalledWith({ width: 320, height: 180 });
  });
});

describe("volumes", () => {
  /** A remote sound track as livekit.ts tells one: by its class. */
  const sound = () => {
    const track = Object.create(RemoteAudioTrack.prototype) as RemoteAudioTrack;
    const spies = { setVolume: vi.fn(), setAudioContext: vi.fn() };
    Object.assign(track, { kind: Track.Kind.Audio, attach: () => document.createElement("audio"), ...spies });
    return { track, ...spies };
  };

  /** Puts `userId` in the room with a subscribed voice or screen share sound. */
  const heard = (userId: string, source: Track.Source) => {
    const s = sound();
    const pub = { source, audioTrack: s.track };
    const participant = { identity: userId, audioTrackPublications: new Map([[source, pub]]) };
    (room() as unknown as { remoteParticipants: Map<string, unknown> }).remoteParticipants.set(userId, participant);
    room().emit(RoomEvent.TrackSubscribed, s.track, pub, participant);
    return s;
  };

  /** A browser audio context, as far as the transport touches one. */
  const context = { state: "running", close: vi.fn(() => Promise.resolve()) };
  beforeEach(() => {
    context.close.mockClear();
    vi.stubGlobal(
      "AudioContext",
      vi.fn(function AudioContext() {
        return context;
      }),
    );
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("plays everyone as sent until told otherwise, through the elements alone", async () => {
    await connected();
    const voice = heard("7", Track.Source.Microphone);
    expect(voice.setVolume).toHaveBeenLastCalledWith(1);
    expect(voice.setAudioContext).toHaveBeenLastCalledWith(undefined);
    expect(AudioContext).not.toHaveBeenCalled();
  });

  it("turns one person down or off without touching the others or their screen share", async () => {
    const t = await connected();
    const voice = heard("7", Track.Source.Microphone);
    const other = heard("8", Track.Source.Microphone);
    t.setVolume("7", "voice", 0.3);
    expect(voice.setVolume).toHaveBeenLastCalledWith(0.3);
    expect(other.setVolume).toHaveBeenCalledTimes(1);
    const share = heard("7", Track.Source.ScreenShareAudio);
    expect(share.setVolume).toHaveBeenLastCalledWith(1);
  });

  it("turns a person up past 100 % through a gain node, and back through the elements", async () => {
    const t = await connected();
    const voice = heard("7", Track.Source.Microphone);
    t.setVolume("7", "voice", 2);
    expect(voice.setAudioContext).toHaveBeenLastCalledWith(context);
    expect(voice.setVolume).toHaveBeenLastCalledWith(2);
    t.setVolume("7", "voice", 0.8);
    // The volume before the elements take the sound back: they cannot be set above 1.
    expect(voice.setVolume).toHaveBeenLastCalledWith(0.8);
    expect(voice.setAudioContext).toHaveBeenLastCalledWith(undefined);
    expect(voice.setVolume.mock.invocationCallOrder.at(-1)).toBeLessThan(
      voice.setAudioContext.mock.invocationCallOrder.at(-1) ?? 0,
    );
  });

  it("keeps a volume for someone who joins later, and a screen share's apart from the voice's", async () => {
    const t = await connected();
    t.setVolume("7", "stream", 1.5);
    const voice = heard("7", Track.Source.Microphone);
    const share = heard("7", Track.Source.ScreenShareAudio);
    expect(voice.setVolume).toHaveBeenLastCalledWith(1);
    expect(share.setVolume).toHaveBeenLastCalledWith(1.5);
  });

  it("silences everyone while deafened and brings each volume back after", async () => {
    const t = await connected();
    t.setVolume("7", "voice", 1.5);
    const voice = heard("7", Track.Source.Microphone);
    t.setListening(false);
    expect(voice.setVolume).toHaveBeenLastCalledWith(0);
    expect(voice.setAudioContext).toHaveBeenLastCalledWith(undefined);
    t.setListening(true);
    expect(voice.setVolume).toHaveBeenLastCalledWith(1.5);
  });

  it("closes the audio context with the call", async () => {
    const t = await connected();
    heard("7", Track.Source.Microphone);
    t.setVolume("7", "voice", 2);
    await t.disconnect();
    expect(context.close).toHaveBeenCalledTimes(1);
  });
});
