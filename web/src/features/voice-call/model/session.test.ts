import { HttpResponse } from "msw";

import { getCall, resetCall } from "@/entities/call";
import { applyGatewayDispatch, resetGuilds } from "@/entities/guild";
import { ApiError, errorText } from "@/shared/api";
import {
  apiHandler,
  FakeTransport,
  makeChannel,
  makeGuildState,
  makeReady,
  makeUser,
  makeVoiceState,
  problem,
  server,
} from "@/shared/test";
import {
  CALL_REMOVED,
  CALL_REPLACED,
  endCall,
  joinVoice,
  leaveVoice,
  MIC_DENIED,
  onCallNotice,
  onVoiceMove,
  reconcileVoiceFlags,
  setSelfDeaf,
  setSelfMute,
  setTransportFactory,
} from "./session";

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
});

describe("voice session", () => {
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

  it("ends with a notice and no reconnect when the call is taken elsewhere", async () => {
    await joinVoice(G, LOUNGE);
    meIn(LOUNGE);
    fake.emit("link", "disconnected", "removed");
    expect(getCall().status).toBe("idle");
    expect(notices).toEqual([CALL_REPLACED]);
    expect(fake.argsOf("connect")).toHaveLength(1);
  });

  it("says the user was disconnected when the server no longer has them in voice", async () => {
    await joinVoice(G, LOUNGE);
    fake.emit("link", "disconnected", "removed");
    expect(notices).toEqual([CALL_REMOVED]);
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
