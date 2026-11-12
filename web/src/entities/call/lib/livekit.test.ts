import { DisconnectReason, LocalAudioTrack, RoomEvent, Track } from "livekit-client";
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
