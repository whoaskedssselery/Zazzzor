import { screen, waitFor } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { getCall, resetCall } from "@/entities/call";
import { resetGuilds } from "@/entities/guild";
import { CALL_REPLACED, joinVoice, setTransportFactory } from "@/features/voice-call";
import {
  apiHandler,
  dispatchFrame,
  FakeTransport,
  makeChannel,
  makeGuildState,
  makeReady,
  makeVoiceState,
  readyGateway,
  server,
} from "@/shared/test";

type Client = WebSocketHandlerConnection["client"];

const G = "10";
const TEXT = "101";
const LOUNGE = "102";
const GAMES = "103";

let fake: FakeTransport;
let tokens: string[];
let flags: unknown[];
let send: (t: string, d: unknown) => void;

beforeEach(() => {
  fake = new FakeTransport();
  setTransportFactory(() => fake);
  tokens = [];
  flags = [];
  let client: Client | undefined;
  let seq = 1;
  send = (t, d) => {
    seq += 1;
    client?.send(dispatchFrame(t, seq, d));
  };
  server.use(
    sessionHandler(testUser),
    readyGateway(
      makeReady(testUser, [
        makeGuildState(G, testUser, {
          channels: [
            makeChannel(TEXT, G, { name: "общий" }),
            makeChannel(LOUNGE, G, { type: 2, name: "Лаунж" }),
            makeChannel(GAMES, G, { type: 2, name: "Игры" }),
          ],
        }),
      ]),
      (c) => {
        client = c;
      },
    ),
    apiHandler("post", "/channels/{channel_id}/voice", ({ params }) => {
      tokens.push(params.channel_id);
      return HttpResponse.json({ token: `t-${params.channel_id}`, url: "ws://localhost" });
    }),
    apiHandler("patch", "/users/@me/voice-state", async ({ request }) => {
      flags.push(await request.json());
      return new HttpResponse(null, { status: 204 });
    }),
    apiHandler("post", "/auth/logout", () => new HttpResponse(null, { status: 204 })),
  );
});

afterEach(() => {
  resetCall();
  resetGuilds();
});

/** The app on the text channel, connected, and the user in the lounge. */
const inLounge = async () => {
  const app = renderApp(`/channels/${G}/${TEXT}`);
  await screen.findByRole("textbox", { name: "Сообщение в #общий" });
  await waitFor(() => {
    expect(send).toBeDefined();
  });
  await joinVoice(G, LOUNGE);
  return app;
};

describe("voice in the app", () => {
  it("follows VOICE_MOVE to the new channel", async () => {
    await inLounge();
    send("VOICE_MOVE", { channel_id: GAMES });
    await waitFor(() => {
      expect(tokens).toEqual([LOUNGE, GAMES]);
    });
    expect(getCall().channelId).toBe(GAMES);
  });

  it("sends the switches when the server's state of the user differs", async () => {
    await inLounge();
    send("VOICE_STATE_UPDATE", { ...makeVoiceState(G, LOUNGE, testUser.id, { self_mute: true }), left: false });
    await waitFor(() => {
      expect(flags).toEqual([{ self_mute: false, self_deaf: false }]);
    });
  });

  it("shows why the call ended", async () => {
    await inLounge();
    send("VOICE_STATE_UPDATE", { ...makeVoiceState(G, LOUNGE, testUser.id), left: false });
    await waitFor(() => {
      expect(getCall().status).toBe("connected");
    });
    fake.emit("link", "disconnected", "removed");
    expect(await screen.findByText(CALL_REPLACED, { exact: true })).toBeTruthy();
  });

  it("drops the call when the user signs out", async () => {
    const { user } = await inLounge();
    await user.click(screen.getByRole("button", { name: "Выйти" }));
    await waitFor(() => {
      expect(getCall().status).toBe("idle");
    });
    expect(fake.argsOf("disconnect").length).toBeGreaterThan(1);
  });
});
