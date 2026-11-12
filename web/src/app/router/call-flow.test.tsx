import { screen, waitFor, within } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { getCall, resetCall } from "@/entities/call";
import { applyEvent, applyReady, resetGuilds } from "@/entities/guild";
import { setTransportFactory } from "@/features/voice-call";
import {
  apiHandler,
  dispatchFrame,
  FakeTransport,
  fakeListLayout,
  makeDMChannel,
  makeReady,
  makeUser,
  makeVoiceState,
  readyGateway,
  server,
} from "@/shared/test";
import { ringingCalls } from "@/widgets/incoming-call";

type Client = WebSocketHandlerConnection["client"];

const anya = makeUser("2", { display_name: "Аня", username: "anya" });
const DM = "900";

let fake: FakeTransport;
let tokens: string[];
let declined: string[];
let send: (t: string, d: unknown) => void;

beforeEach(() => {
  fake = new FakeTransport();
  setTransportFactory(() => fake);
  tokens = [];
  declined = [];
  let client: Client | undefined;
  let seq = 1;
  send = (t, d) => {
    if (!client) throw new Error("the gateway is not connected");
    seq += 1;
    client.send(dispatchFrame(t, seq, d));
  };
  server.use(
    sessionHandler(testUser),
    readyGateway({ ...makeReady(testUser), private_channels: [makeDMChannel(DM, [anya])] }, (c) => {
      client = c;
    }),
    apiHandler("get", "/channels/{channel_id}/messages", () => HttpResponse.json([])),
    apiHandler("post", "/channels/{channel_id}/voice", ({ params }) => {
      tokens.push(params.channel_id);
      return HttpResponse.json({ token: `t-${params.channel_id}`, url: "ws://localhost" });
    }),
    apiHandler("post", "/channels/{channel_id}/call/decline", ({ params }) => {
      declined.push(params.channel_id);
      return new HttpResponse(null, { status: 204 });
    }),
  );
});

let restoreLayout = () => {};
beforeEach(() => {
  restoreLayout = fakeListLayout();
});

afterEach(() => {
  restoreLayout();
  resetCall();
  resetGuilds();
});

const anyasCall = makeVoiceState("", DM, anya.id, { guild_id: null });

describe("calls in the store", () => {
  it("keeps who is in a call and whom it rings, and forgets both when it ends", () => {
    let s = applyReady({
      ...makeReady(testUser),
      private_channels: [makeDMChannel(DM, [anya])],
      private_voice_states: [anyasCall],
      calls: [{ channel_id: DM, ringing: [testUser.id] }],
    });
    expect(Object.keys(s.privates[DM]?.voice ?? {})).toEqual([anya.id]);
    expect(ringingCalls(s, null).map((p) => p.channel.id)).toEqual([DM]);
    // Not while the user is in that call already, nor once it no longer rings them.
    expect(ringingCalls(s, DM)).toEqual([]);
    s = applyEvent(s, "CALL_UPDATE", { channel_id: DM, ringing: [] });
    expect(ringingCalls(s, null)).toEqual([]);
    s = applyEvent(s, "VOICE_STATE_UPDATE", { ...anyasCall, left: true });
    s = applyEvent(s, "CALL_DELETE", { channel_id: DM });
    expect(s.privates[DM]?.voice).toEqual({});
    expect(s.calls).toEqual({});
  });

  it("does not ring in a muted DM", () => {
    const s = applyReady({
      ...makeReady(testUser),
      private_channels: [makeDMChannel(DM, [anya])],
      calls: [{ channel_id: DM, ringing: [testUser.id] }],
      user_guild_settings: [
        {
          guild_id: null,
          version: 1,
          muted: true,
          mute_until: null,
          message_notifications: 0,
          suppress_everyone: false,
          channel_overrides: [],
        },
      ],
    });
    expect(ringingCalls(s, null)).toEqual([]);
  });
});

describe("calls in the app", () => {
  it("rings on any screen; «Отклонить» declines", async () => {
    renderApp("/channels/@me");
    await screen.findByRole("heading", { level: 1, name: "Друзья" });
    send("CALL_CREATE", { channel_id: DM, ringing: [testUser.id] });
    const card = await screen.findByRole("alertdialog", { name: "Входящий звонок: Аня" });
    within(card).getByRole("button", { name: "Отклонить" }).click();
    await waitFor(() => {
      expect(declined).toEqual([DM]);
    });
    send("CALL_UPDATE", { channel_id: DM, ringing: [] });
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).toBeNull();
    });
  });

  it("«Принять» opens the DM and joins its call", async () => {
    const { user, router } = renderApp("/channels/@me");
    await screen.findByRole("heading", { level: 1, name: "Друзья" });
    send("CALL_CREATE", { channel_id: DM, ringing: [testUser.id] });
    send("VOICE_STATE_UPDATE", { ...anyasCall, left: false });
    const card = await screen.findByRole("alertdialog", { name: "Входящий звонок: Аня" });
    await user.click(within(card).getByRole("button", { name: "Принять" }));
    await waitFor(() => {
      expect(tokens).toEqual([DM]);
    });
    expect(router.state.location.pathname).toBe(`/channels/@me/${DM}`);
    await waitFor(() => {
      expect(getCall()).toMatchObject({ status: "connected", guildId: null, channelId: DM });
    });
    // In the call, its stage is above the chat, and the card is gone.
    expect(await screen.findByRole("region", { name: "Звонок" })).toBeTruthy();
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("starts a call from the DM's header, and shows one going on", async () => {
    const { user } = renderApp(`/channels/@me/${DM}`);
    await screen.findByRole("heading", { level: 1, name: "Аня" });
    send("VOICE_STATE_UPDATE", { ...anyasCall, left: false });
    const going = await screen.findByRole("region", { name: "Идёт звонок" });
    expect(within(going).getByText(/Аня/)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Присоединиться к звонку" }));
    await waitFor(() => {
      expect(tokens).toEqual([DM]);
    });
    expect(await screen.findByRole("region", { name: "Звонок" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Присоединиться к звонку" })).toBeNull();
  });
});
