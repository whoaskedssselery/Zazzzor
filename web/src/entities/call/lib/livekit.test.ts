import { DisconnectReason, RoomEvent, Track } from "livekit-client";
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
const made = vi.hoisted(() => ({ rooms: [] as unknown[] }));

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
  return { ...real, Room };
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
