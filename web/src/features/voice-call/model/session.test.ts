import { HttpResponse } from "msw";

import {
  DEFAULT_VOICE_PREFS,
  getCall,
  type NativeShareStatus,
  reloadVolumes,
  resetCall,
  resetNativeShare,
  type ShareSource,
  setVolume,
  writeVoicePrefs,
} from "@/entities/call";
import { applyGatewayDispatch, DEFAULT_EVERYONE, formatMask, PERM, resetGuilds } from "@/entities/guild";
import { ApiError, errorText } from "@/shared/api";
import {
  apiHandler,
  FakeTransport,
  makeChannel,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeReady,
  makeUser,
  makeVoiceState,
  problem,
  server,
} from "@/shared/test";
import { getNativeShare, resetNativeShareState } from "./native-share";
import {
  applyVoicePrefs,
  CALL_REMOVED,
  CALL_REPLACED,
  CAMERA_DENIED,
  CAMERA_FAILED,
  CAMERA_MISSING,
  DEVICE_FAILED,
  endCall,
  joinVoice,
  leaveVoice,
  MIC_DENIED,
  onCallNotice,
  onVoiceMove,
  reconcileVoiceFlags,
  SHARE_FAILED,
  SHARE_SOURCE_CLOSED,
  setSelfDeaf,
  setSelfMute,
  setTransportFactory,
  shareNatively,
  startCamera,
  startScreenShare,
  stopCamera,
  stopScreenShare,
  switchAudioDevice,
  watchStream,
} from "./session";

const played = vi.hoisted(() => [] as string[]);
vi.mock("@/shared/lib/sounds", () => ({ playSound: (name: string) => played.push(name), resetSounds: () => {} }));

const me = makeUser("100");
const G = "10";
const LOUNGE = "102";
const GAMES = "103";
const SIGNAL = "ws://localhost:4173";

type Voice = ReturnType<typeof makeVoiceState>;

let fake: FakeTransport;
let notices: string[];
let tokens: string[];
let left: number;
let flags: unknown[];

/** The server as READY tells it, with these voice states. */
const ready = (voice: Voice[] = []) => {
  applyGatewayDispatch(
    "READY",
    makeReady(me, [
      makeGuildState(G, me, {
        channels: [makeChannel(LOUNGE, G, { type: 2 }), makeChannel(GAMES, G, { type: 2 })],
        voice_states: voice,
      }),
    ]),
  );
};

/** The server reports me in `channelId` with these flags. */
const meIn = (channelId: string, over: Partial<Voice> = {}) => {
  applyGatewayDispatch("VOICE_STATE_UPDATE", { ...makeVoiceState(G, channelId, me.id, over), left: false });
};

beforeEach(() => {
  fake = new FakeTransport();
  setTransportFactory(() => fake);
  notices = [];
  tokens = [];
  left = 0;
  flags = [];
  onTestFinished(
    onCallNotice((text) => {
      notices.push(text);
    }),
  );
  server.use(
    apiHandler("post", "/channels/{channel_id}/voice", ({ params }) => {
      tokens.push(params.channel_id);
      return HttpResponse.json({ token: `t-${params.channel_id}`, url: SIGNAL });
    }),
    apiHandler("delete", "/users/@me/voice", () => {
      left += 1;
      return new HttpResponse(null, { status: 204 });
    }),
    apiHandler("patch", "/users/@me/voice-state", async ({ request }) => {
      flags.push(await request.json());
      return new HttpResponse(null, { status: 204 });
    }),
  );
  ready();
});

afterEach(() => {
  resetCall();
  resetGuilds();
  localStorage.clear();
  reloadVolumes();
});

describe("voice session", () => {
  it("plays the call's sounds: joining, the switches, leaving", async () => {
    played.length = 0;
    await joinVoice(G, LOUNGE);
    setSelfMute(true);
    setSelfMute(true);
    setSelfDeaf(true);
    setSelfDeaf(false);
    setSelfMute(false);
    await leaveVoice();
    expect(played).toEqual(["join", "mute", "deafen", "undeafen", "unmute", "leave"]);
  });

  it("tells the call the volumes kept on this device, and each change as it is made", async () => {
    setVolume("voice", "7", 150);
    setVolume("stream", "8", 0);
    await joinVoice(G, LOUNGE);
    expect(fake.argsOf("setVolume")).toEqual([
      ["7", "voice", 1.5],
      ["8", "stream", 0],
    ]);
    setVolume("voice", "7", 60);
    expect(fake.argsOf("setVolume").at(-1)).toEqual(["7", "voice", 0.6]);
    // Back to 100 %: forgotten on the device, and the call is told all the same.
    setVolume("voice", "7", 100);
    expect(fake.argsOf("setVolume").at(-1)).toEqual(["7", "voice", 1]);
    expect(fake.argsOf("setVolume")).toHaveLength(4);
  });

  it("joins with a fresh token and turns the microphone and sound on", async () => {
    await joinVoice(G, LOUNGE);
    expect(tokens).toEqual([LOUNGE]);
    expect(fake.argsOf("connect")).toEqual([[SIGNAL, `t-${LOUNGE}`]]);
    expect(fake.argsOf("setMicrophone")).toEqual([[true]]);
    expect(fake.argsOf("setListening")).toEqual([[true]]);
    expect(getCall()).toMatchObject({ status: "connected", guildId: G, channelId: LOUNGE });
  });

  it("joins muted and deafened with the microphone and sound off", async () => {
    setSelfMute(true);
    setSelfDeaf(true);
    await joinVoice(G, LOUNGE);
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([false]);
    expect(fake.argsOf("setListening").at(-1)).toEqual([false]);
  });

  it("moves to another channel over one connection", async () => {
    await joinVoice(G, LOUNGE);
    await joinVoice(G, GAMES);
    expect(tokens).toEqual([LOUNGE, GAMES]);
    const links = fake.calls.map((c) => c.method).filter((m) => m === "connect" || m === "disconnect");
    expect(links).toEqual(["disconnect", "connect", "disconnect", "connect"]);
    expect(getCall().channelId).toBe(GAMES);
  });

  it("leaves: tells the server, drops the connection and keeps the switches", async () => {
    setSelfMute(true);
    await joinVoice(G, LOUNGE);
    await leaveVoice();
    expect(left).toBe(1);
    expect(fake.argsOf("disconnect").length).toBeGreaterThan(1);
    expect(getCall()).toMatchObject({ status: "idle", channelId: null, selfMute: true });
  });

  it("stays in the call without a microphone when it is refused", async () => {
    await joinVoice(G, LOUNGE);
    fake.emit("micDenied");
    expect(getCall()).toMatchObject({ status: "connected", micDenied: true });
    expect(notices).toEqual([MIC_DENIED]);
  });

  it("follows a move to the channel VOICE_MOVE names", async () => {
    await joinVoice(G, LOUNGE);
    await onVoiceMove(GAMES);
    expect(tokens).toEqual([LOUNGE, GAMES]);
    expect(getCall().channelId).toBe(GAMES);
  });

  it("tells the server its own switches when its state differs, also after READY", async () => {
    setSelfMute(true);
    await joinVoice(G, LOUNGE);
    meIn(LOUNGE);
    await reconcileVoiceFlags();
    expect(flags).toEqual([{ self_mute: true, self_deaf: false }]);
    meIn(LOUNGE, { self_mute: true });
    await reconcileVoiceFlags();
    expect(flags).toHaveLength(1);
    // The API restarted: READY has the state with its flags back at false.
    ready([makeVoiceState(G, LOUNGE, me.id)]);
    await reconcileVoiceFlags();
    expect(flags).toHaveLength(2);
  });

  it("says why it could not join and stays out", async () => {
    server.use(apiHandler("post", "/channels/{channel_id}/voice", () => problem(403, "missing_permission")));
    await joinVoice(G, LOUNGE);
    expect(getCall().status).toBe("idle");
    expect(notices).toEqual([errorText(new ApiError(403, "missing_permission"))]);
    expect(fake.argsOf("connect")).toHaveLength(0);
  });

  it("drops the call without asking the server when the session ends", async () => {
    await joinVoice(G, LOUNGE);
    await endCall("left");
    expect(getCall().status).toBe("idle");
    expect(left).toBe(0);
    expect(notices).toEqual([]);
  });

  it("keeps who speaks and who shares a screen", async () => {
    await joinVoice(G, LOUNGE);
    fake.emit("speaking", [me.id]);
    fake.emit("streams", ["200"]);
    expect(getCall()).toMatchObject({ speaking: [me.id], streams: ["200"] });
  });
});

describe("voice settings in the call", () => {
  afterEach(() => {
    localStorage.clear();
  });

  const key = (type: "keydown" | "keyup") => {
    window.dispatchEvent(new KeyboardEvent(type, { code: "KeyV" }));
  };

  it("talks only while the push-to-talk key is held", async () => {
    writeVoicePrefs({ ...DEFAULT_VOICE_PREFS, mode: "ptt" });
    await joinVoice(G, LOUNGE);
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([false]);
    key("keydown");
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([true]);
    key("keyup");
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([false]);
    await leaveVoice();
    key("keydown");
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([false]);
  });

  it("uses push-to-talk where voice activity is not allowed", async () => {
    applyGatewayDispatch(
      "READY",
      makeReady(me, [
        makeGuildState(G, me, {
          guild: makeGuild(G, "999"),
          roles: [makeEveryone(G, formatMask(DEFAULT_EVERYONE & ~PERM.USE_VOICE_ACTIVITY))],
          channels: [makeChannel(LOUNGE, G, { type: 2 })],
        }),
      ]),
    );
    await joinVoice(G, LOUNGE);
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([false]);
    key("keydown");
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([true]);
    key("keyup");
  });

  it("plays through the chosen output from the start", async () => {
    writeVoicePrefs({ ...DEFAULT_VOICE_PREFS, outputId: "out-1" });
    await joinVoice(G, LOUNGE);
    expect(fake.argsOf("useDevice")).toEqual([["audiooutput", "out-1"]]);
  });

  it("joins with system audio when the saved output is unavailable", async () => {
    writeVoicePrefs({ ...DEFAULT_VOICE_PREFS, outputId: "unavailable" });
    fake.deviceError = new DOMException("no such device", "NotFoundError");

    await joinVoice(G, LOUNGE);

    expect(fake.argsOf("useDevice")).toEqual([
      ["audiooutput", "unavailable"],
      ["audiooutput", "default"],
    ]);
    expect(getCall().status).toBe("connected");
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([true]);
    expect(notices).toEqual([]);
  });

  it("applies a change of mode in the call at once", async () => {
    await joinVoice(G, LOUNGE);
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([true]);
    writeVoicePrefs({ ...DEFAULT_VOICE_PREFS, mode: "ptt" });
    applyVoicePrefs();
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([false]);
  });
});

describe("the final review's cases", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  /** A promise and the function that settles it. */
  const gate = () => {
    let open = () => {};
    const promise = new Promise<void>((resolve) => {
      open = resolve;
    });
    return { promise, open };
  };

  it("C1: switches channels silently even when the server drops the old room before answering", async () => {
    await joinVoice(G, LOUNGE);
    meIn(LOUNGE);
    server.use(
      apiHandler("post", "/channels/{channel_id}/voice", ({ params }) => {
        tokens.push(params.channel_id);
        // The server removes the old participant from LiveKit before it answers.
        fake.emit("link", "disconnected", "removed");
        return HttpResponse.json({ token: `t-${params.channel_id}`, url: SIGNAL });
      }),
    );
    await joinVoice(G, GAMES);
    expect(notices).toEqual([]);
    expect(getCall()).toMatchObject({ status: "connected", channelId: GAMES });
  });

  it("C2: a double click on the first join makes one transport and one call", async () => {
    let made = 0;
    setTransportFactory(async () => {
      made += 1;
      await Promise.resolve();
      return fake;
    });
    await Promise.all([joinVoice(G, LOUNGE), joinVoice(G, LOUNGE)]);
    expect(made).toBe(1);
    expect(getCall()).toMatchObject({ status: "connected", guildId: G, channelId: LOUNGE });
    expect(notices).toEqual([]);
  });

  it("I5: leaving while the connection is still being made says nothing", async () => {
    const held = gate();
    fake.connectGate = held.promise;
    const joining = joinVoice(G, LOUNGE);
    await vi.waitFor(() => {
      expect(fake.argsOf("connect")).toHaveLength(1);
    });
    await leaveVoice();
    held.open();
    await joining;
    expect(notices).toEqual([]);
    expect(getCall().status).toBe("idle");
  });

  it("I1: a moderator's disconnect says the user was disconnected, once the server says they left", async () => {
    await joinVoice(G, LOUNGE);
    meIn(LOUNGE);
    fake.emit("link", "disconnected", "removed");
    expect(notices).toEqual([]);
    applyGatewayDispatch("VOICE_STATE_UPDATE", { ...makeVoiceState(G, LOUNGE, me.id), left: true });
    await reconcileVoiceFlags();
    expect(notices).toEqual([CALL_REMOVED]);
  });

  it("I1: a removal while the server still has the user in voice is a takeover, after a moment", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    await joinVoice(G, LOUNGE);
    meIn(LOUNGE);
    fake.emit("link", "disconnected", "removed");
    expect(notices).toEqual([]);
    await vi.advanceTimersByTimeAsync(2500);
    expect(notices).toEqual([CALL_REPLACED]);
  });

  it("I2: turns the microphone back on when a moderator lifts the mute", async () => {
    await joinVoice(G, LOUNGE);
    meIn(LOUNGE, { mute: true });
    await reconcileVoiceFlags();
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([false]);
    meIn(LOUNGE, { mute: false });
    await reconcileVoiceFlags();
    expect(fake.argsOf("setMicrophone").at(-1)).toEqual([true]);
  });

  it("I3: a switch flipped back while its PATCH is in flight still reaches the server", async () => {
    const held = gate();
    server.use(
      apiHandler("patch", "/users/@me/voice-state", async ({ request }) => {
        flags.push(await request.json());
        if (flags.length === 1) await held.promise;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    await joinVoice(G, LOUNGE);
    meIn(LOUNGE);
    setSelfMute(true);
    await vi.waitFor(() => {
      expect(flags).toHaveLength(1);
    });
    setSelfMute(false);
    // The echo of the first PATCH arrives while it is still in flight.
    meIn(LOUNGE, { self_mute: true });
    await reconcileVoiceFlags();
    held.open();
    await vi.waitFor(() => {
      expect(flags.at(-1)).toEqual({ self_mute: false, self_deaf: false });
    });
  });

  it("I9: after the call ended from outside, the switches do not touch the microphone", async () => {
    await joinVoice(G, LOUNGE);
    fake.emit("link", "disconnected", "failed");
    const before = fake.argsOf("setMicrophone").length;
    setSelfMute(true);
    setSelfMute(false);
    expect(fake.argsOf("setMicrophone")).toHaveLength(before);
  });

  it("I6: a share picked after the call ended is stopped at once", async () => {
    await joinVoice(G, LOUNGE);
    let pick: (r: { ok: true; surface: "browser"; audio: boolean }) => void = () => {};
    fake.screenResult = new Promise((resolve) => {
      pick = resolve;
    });
    const sharing = startScreenShare();
    await leaveVoice();
    pick({ ok: true, surface: "browser", audio: true });
    await sharing;
    expect(getCall().sharing).toBe(false);
    expect(fake.argsOf("stopScreenShare")).toHaveLength(1);
  });

  it("I7: says when a device cannot be switched", async () => {
    await joinVoice(G, LOUNGE);
    fake.deviceError = new DOMException("no such device", "OverconstrainedError");
    await switchAudioDevice("audioinput", "default");
    expect(notices).toEqual([DEVICE_FAILED]);
  });
});

describe("noise suppression in the call", () => {
  it("keeps what cleans the microphone, and forgets it when the call ends", async () => {
    await joinVoice(G, LOUNGE);
    fake.emit("noise", { running: "loading" });
    fake.emit("noise", { running: "rnnoise", reason: "overload" });
    expect(getCall().noise).toEqual({ running: "rnnoise", reason: "overload" });
    await leaveVoice();
    expect(getCall().noise).toBeNull();
    fake.emit("noise", { running: "enhanced" });
    expect(getCall().noise).toBeNull();
  });
});

describe("the second review's cases", () => {
  it("5: a call that dies right after connecting ends instead of showing as connected", async () => {
    writeVoicePrefs({ ...DEFAULT_VOICE_PREFS, outputId: "out-1" });
    onTestFinished(() => {
      localStorage.clear();
    });
    // The room goes away while the output device is switched, after the connection.
    fake.useDevice = () => {
      fake.emit("link", "disconnected", "replaced");
      return Promise.resolve();
    };
    await joinVoice(G, LOUNGE);
    expect(getCall().status).toBe("idle");
    expect(notices).toEqual([CALL_REPLACED]);
  });

  it("6: a second click while the picker is open does not open another capture", async () => {
    await joinVoice(G, LOUNGE);
    let pick: (r: { ok: true; surface: "browser"; audio: boolean }) => void = () => {};
    fake.screenResult = new Promise((resolve) => {
      pick = resolve;
    });
    const first = startScreenShare();
    const second = startScreenShare();
    pick({ ok: true, surface: "browser", audio: true });
    await Promise.all([first, second]);
    expect(fake.argsOf("startScreenShare")).toHaveLength(1);
  });

  it("8: a stream that ends stops being watched in the transport too", async () => {
    await joinVoice(G, LOUNGE);
    fake.emit("streams", ["200"]);
    watchStream("200", true);
    fake.emit("streams", []);
    expect(getCall().watching).toEqual([]);
    expect(fake.argsOf("watch").at(-1)).toEqual(["200", false]);
  });

  it("11: joining again soon after a removal does not say the call moved elsewhere", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    onTestFinished(() => {
      vi.useRealTimers();
    });
    await joinVoice(G, LOUNGE);
    meIn(LOUNGE);
    fake.emit("link", "disconnected", "removed");
    await joinVoice(G, LOUNGE);
    meIn(LOUNGE);
    await vi.advanceTimersByTimeAsync(3000);
    expect(notices).toEqual([]);
  });
});

describe("the camera in the call", () => {
  it("turns on and off, and keeps whose cameras are on", async () => {
    await joinVoice(G, LOUNGE);
    await startCamera();
    expect(getCall().camera).toBe(true);
    fake.emit("cameras", ["200", "201"]);
    expect(getCall().cameras).toEqual(["200", "201"]);
    await stopCamera();
    expect(getCall().camera).toBe(false);
    expect(fake.argsOf("stopCamera")).toHaveLength(1);
  });

  it.each([
    ["denied", CAMERA_DENIED],
    ["missing", CAMERA_MISSING],
    ["failed", CAMERA_FAILED],
  ] as const)("says why the camera stayed off (%s)", async (reason, text) => {
    await joinVoice(G, LOUNGE);
    fake.cameraResult = { ok: false, reason };
    await startCamera();
    expect(getCall().camera).toBe(false);
    expect(notices).toEqual([text]);
  });

  it("does nothing outside a call", async () => {
    await startCamera();
    expect(fake.argsOf("startCamera")).toHaveLength(0);
    expect(getCall().camera).toBe(false);
  });

  it("turns a camera allowed after the call ended back off", async () => {
    await joinVoice(G, LOUNGE);
    let allow: (r: { ok: true }) => void = () => {};
    fake.cameraResult = new Promise((resolve) => {
      allow = resolve;
    });
    const starting = startCamera();
    await leaveVoice();
    allow({ ok: true });
    await starting;
    expect(getCall().camera).toBe(false);
    expect(fake.argsOf("stopCamera")).toHaveLength(1);
  });

  it("shows the camera as off when it went off by itself, and forgets cameras with the call", async () => {
    await joinVoice(G, LOUNGE);
    await startCamera();
    fake.emit("cameras", ["200"]);
    fake.emit("cameraEnded");
    expect(getCall().camera).toBe(false);
    await leaveVoice();
    expect(getCall().cameras).toEqual([]);
  });
});

describe("the desktop app's screen share", () => {
  const WINDOW: ShareSource = {
    id: "window:42",
    kind: "window",
    name: "Игра",
    app: "game.exe",
    width: 1920,
    height: 1080,
  };

  let calls: [string, Record<string, unknown> | undefined][];
  let status: NativeShareStatus;
  let commands: boolean;
  let screenTokens: number;

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    calls = [];
    status = { state: "idle" };
    commands = true;
    screenTokens = 0;
    resetNativeShare();
    Object.assign(globalThis, {
      __TAURI_INTERNALS__: {
        invoke: (cmd: string, args?: Record<string, unknown>) => {
          calls.push([cmd, args]);
          if (!commands) return Promise.reject(new Error(`Command ${cmd} not allowed by ACL`));
          return Promise.resolve(cmd === "share_status" ? status : null);
        },
      },
    });
    server.use(
      apiHandler("post", "/channels/{channel_id}/voice/screen", ({ params }) => {
        screenTokens += 1;
        return HttpResponse.json({ token: `s-${params.channel_id}`, url: SIGNAL });
      }),
    );
  });

  afterEach(() => {
    Reflect.deleteProperty(globalThis, "__TAURI_INTERNALS__");
    resetNativeShareState();
    vi.useRealTimers();
  });

  const started = () => calls.filter(([cmd]) => cmd === "share_start").map(([, args]) => args);
  const poll = () => vi.advanceTimersByTimeAsync(2000);

  it("opens the app's picker instead of the browser's", async () => {
    await joinVoice(G, LOUNGE);
    await startScreenShare();
    expect(getNativeShare().picking).toBe(true);
    expect(fake.argsOf("startScreenShare")).toHaveLength(0);
  });

  it("uses the browser's picker in an app without the share's commands", async () => {
    commands = false;
    await joinVoice(G, LOUNGE);
    await startScreenShare();
    expect(getNativeShare().picking).toBe(false);
    expect(fake.argsOf("startScreenShare")).toHaveLength(1);
  });

  it("hands the app a companion token for the call's channel", async () => {
    await joinVoice(G, LOUNGE);
    played.length = 0;
    await shareNatively(WINDOW, { sound: true, fps: 60 });
    expect(started()).toEqual([{ url: SIGNAL, token: `s-${LOUNGE}`, id: WINDOW.id, sound: true, fps: 60 }]);
    expect(getCall().sharing).toBe(true);
    expect(getNativeShare().source).toEqual(WINDOW);
    expect(played).toEqual(["streamStart"]);
  });

  it("says why the server refused and starts nothing", async () => {
    server.use(apiHandler("post", "/channels/{channel_id}/voice/screen", () => problem(409, "not_in_voice")));
    await joinVoice(G, LOUNGE);
    await shareNatively(WINDOW, { sound: false, fps: 60 });
    expect(started()).toEqual([]);
    expect(getCall().sharing).toBe(false);
    expect(notices).toEqual([errorText(new ApiError(409, "not_in_voice"))]);
  });

  it("says the share failed when the app could not start it", async () => {
    await joinVoice(G, LOUNGE);
    commands = false;
    await shareNatively(WINDOW, { sound: false, fps: 60 });
    expect(getCall().sharing).toBe(false);
    expect(notices).toEqual([SHARE_FAILED]);
  });

  it("stops the app's share", async () => {
    await joinVoice(G, LOUNGE);
    await shareNatively(WINDOW, { sound: true, fps: 60 });
    played.length = 0;
    await stopScreenShare();
    expect(calls.map(([cmd]) => cmd)).toContain("share_stop");
    expect(fake.argsOf("stopScreenShare")).toHaveLength(0);
    expect(getCall().sharing).toBe(false);
    expect(getNativeShare().source).toBeNull();
    expect(played).toEqual(["streamStop"]);
  });

  it("shows the app's figures while it shares", async () => {
    await joinVoice(G, LOUNGE);
    await shareNatively(WINDOW, { sound: true, fps: 60 });
    const stats = {
      captureFps: 60,
      sentFps: 59.8,
      width: 1920,
      height: 1080,
      mbitS: 7.5,
      estimateMbitS: 12,
      encoder: "NVENC",
      sound: true,
      level: "p1080" as const,
    };
    status = { state: "sharing", stats };
    await poll();
    expect(getNativeShare().stats).toEqual(stats);
  });

  it("ends the share when the shared window closes, and says so", async () => {
    await joinVoice(G, LOUNGE);
    await shareNatively(WINDOW, { sound: true, fps: 60 });
    played.length = 0;
    status = { state: "ended", end: "sourceClosed" };
    await poll();
    expect(getCall().sharing).toBe(false);
    expect(played).toEqual(["streamStop"]);
    expect(notices).toEqual([SHARE_SOURCE_CLOSED]);
  });

  it("shares again, quietly, when the server drops the companion while the call goes on", async () => {
    await joinVoice(G, LOUNGE);
    await shareNatively(WINDOW, { sound: true, fps: 60 });
    played.length = 0;
    status = { state: "ended", end: "disconnected" };
    await poll();
    await vi.waitFor(() => {
      expect(started()).toHaveLength(2);
    });
    expect(screenTokens).toBe(2);
    expect(getCall().sharing).toBe(true);
    expect(played).toEqual([]);
    // Dropped again before it went well: that is the end.
    await poll();
    expect(getCall().sharing).toBe(false);
    expect(played).toEqual(["streamStop"]);
  });

  it("stops the app's share when the user leaves the call", async () => {
    await joinVoice(G, LOUNGE);
    await shareNatively(WINDOW, { sound: true, fps: 60 });
    await leaveVoice();
    expect(calls.map(([cmd]) => cmd)).toContain("share_stop");
    expect(getNativeShare().source).toBeNull();
  });
});
