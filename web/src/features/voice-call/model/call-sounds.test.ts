import { act, renderHook } from "@testing-library/react";
import { resetCall, setCall } from "@/entities/call";
import { applyGatewayDispatch, resetGuilds } from "@/entities/guild";
import { makeChannel, makeGuildState, makeReady, makeUser, makeVoiceState } from "@/shared/test";
import { useCallSounds } from "./call-sounds";

const played = vi.hoisted(() => [] as string[]);
vi.mock("@/shared/lib/sounds", () => ({ playSound: (name: string) => played.push(name), resetSounds: () => {} }));

const me = makeUser("100");
const anna = makeUser("200");
const petr = makeUser("300");
const G = "10";
const LOUNGE = "102";
const GAMES = "103";

const voice = (userId: string, channelId: string, left = false) => {
  act(() => {
    applyGatewayDispatch("VOICE_STATE_UPDATE", { ...makeVoiceState(G, channelId, userId), left });
  });
};

beforeEach(() => {
  played.length = 0;
  applyGatewayDispatch(
    "READY",
    makeReady(me, [
      makeGuildState(G, me, {
        channels: [makeChannel(LOUNGE, G, { type: 2 }), makeChannel(GAMES, G, { type: 2 })],
        voice_states: [makeVoiceState(G, LOUNGE, anna.id)],
      }),
    ]),
  );
});
afterEach(() => {
  resetCall();
  resetGuilds();
});

const inCall = (channelId: string) => {
  act(() => {
    setCall({ status: "connected", guildId: G, channelId });
  });
};

describe("useCallSounds", () => {
  it("makes no sound for who was there when the user joined", () => {
    renderHook(() => {
      useCallSounds();
    });
    voice(me.id, LOUNGE);
    inCall(LOUNGE);
    expect(played).toEqual([]);
  });

  it("chimes when someone comes into the user's channel and when someone leaves it", () => {
    renderHook(() => {
      useCallSounds();
    });
    inCall(LOUNGE);
    voice(petr.id, LOUNGE);
    expect(played).toEqual(["userJoin"]);
    voice(anna.id, LOUNGE, true);
    expect(played).toEqual(["userJoin", "userLeave"]);
  });

  it("takes someone moving to another channel as leaving, and stays silent outside a call", () => {
    renderHook(() => {
      useCallSounds();
    });
    voice(petr.id, LOUNGE);
    inCall(LOUNGE);
    voice(petr.id, GAMES);
    expect(played).toEqual(["userLeave"]);
    voice(anna.id, GAMES);
    expect(played).toEqual(["userLeave", "userLeave"]);
    act(() => {
      resetCall();
    });
    voice(anna.id, LOUNGE);
    expect(played).toEqual(["userLeave", "userLeave"]);
  });
});
