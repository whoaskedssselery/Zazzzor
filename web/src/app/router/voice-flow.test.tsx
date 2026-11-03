import { act, screen, waitFor, within } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { getCall, resetCall, setCall } from "@/entities/call";
import { DEFAULT_EVERYONE, formatMask, PERM, resetGuilds } from "@/entities/guild";
import {
  CALL_REPLACED,
  joinVoice,
  NO_CONNECT,
  NO_STREAM,
  setTransportFactory,
  WINDOW_NO_AUDIO,
} from "@/features/voice-call";
import { ApiError, errorText } from "@/shared/api";
import {
  apiHandler,
  dispatchFrame,
  FakeTransport,
  makeChannel,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeMember,
  makeReady,
  makeUser,
  makeVoiceState,
  problem,
  readyGateway,
  server,
} from "@/shared/test";
import { MODERATOR_MUTED } from "@/widgets/user-panel";

type Client = WebSocketHandlerConnection["client"];

const G = "10";
const TEXT = "101";
const LOUNGE = "102";
const GAMES = "103";

let fake: FakeTransport;
let tokens: string[];
let flags: unknown[];
let send: (t: string, d: unknown) => void;
/** What READY tells; a test may set its own before rendering. */
let guildState: () => ReturnType<typeof makeGuildState>;

beforeEach(() => {
  fake = new FakeTransport();
  setTransportFactory(() => fake);
  guildState = () =>
    makeGuildState(G, testUser, {
      channels: [
        makeChannel(TEXT, G, { name: "общий" }),
        makeChannel(LOUNGE, G, { type: 2, name: "Лаунж" }),
        makeChannel(GAMES, G, { type: 2, name: "Игры" }),
      ],
    });
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
      () => makeReady(testUser, [guildState()]),
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
    fake.emit("link", "disconnected", "replaced");
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

describe("voice in the sidebar and the panels", () => {
  const petya = makeUser("200", { display_name: "Петя" });
  const masha = makeUser("300", { display_name: "Маша" });

  /** Server 10 with Petya muted in the lounge and Masha deafened and streaming there. */
  const crowd = (everyone = DEFAULT_EVERYONE, mine: Partial<ReturnType<typeof makeVoiceState>> | null = null) => {
    guildState = () =>
      makeGuildState(G, testUser, {
        guild: makeGuild(G, petya.id),
        roles: [makeEveryone(G, formatMask(everyone))],
        channels: [
          makeChannel(TEXT, G, { name: "общий" }),
          makeChannel(LOUNGE, G, { type: 2, name: "Лаунж" }),
          makeChannel(GAMES, G, { type: 2, name: "Игры" }),
        ],
        members: [makeMember(testUser), makeMember(petya), makeMember(masha)],
        voice_states: [
          makeVoiceState(G, LOUNGE, petya.id, { self_mute: true }),
          makeVoiceState(G, LOUNGE, masha.id, { deaf: true, self_stream: true }),
          ...(mine ? [makeVoiceState(G, LOUNGE, testUser.id, mine)] : []),
        ],
      });
  };

  const lounge = () => screen.findByRole("list", { name: "В канале Лаунж" });

  it("lists who is in a voice channel with their switches", async () => {
    crowd();
    renderApp(`/channels/${G}/${TEXT}`);
    const list = await lounge();
    const rows = within(list).getAllByRole("listitem");
    expect(rows.map((r) => r.textContent)).toEqual([expect.stringContaining("Петя"), expect.stringContaining("Маша")]);
    expect(within(rows[0] as HTMLElement).getByRole("img", { name: "Микрофон выключен" })).toBeTruthy();
    expect(within(rows[1] as HTMLElement).getByRole("img", { name: "Звук выключен модератором" })).toBeTruthy();
    expect(within(rows[1] as HTMLElement).getByText("В ЭФИРЕ")).toBeTruthy();
  });

  it("rings whoever speaks", async () => {
    crowd();
    renderApp(`/channels/${G}/${TEXT}`);
    const list = await lounge();
    act(() => {
      setCall({ speaking: [petya.id] });
    });
    const [first, second] = within(list).getAllByRole("listitem");
    expect(first?.querySelector("[data-speaking]")).not.toBeNull();
    expect(second?.querySelector("[data-speaking]")).toBeNull();
  });

  it("joins a voice channel on a click", async () => {
    crowd();
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await user.click(await screen.findByRole("button", { name: "Голосовой канал Игры" }));
    await waitFor(() => {
      expect(tokens).toEqual([GAMES]);
    });
  });

  it("will not join without CONNECT, and says why", async () => {
    crowd(DEFAULT_EVERYONE & ~PERM.CONNECT);
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await user.click(await screen.findByRole("button", { name: "Голосовой канал Игры" }));
    expect(await screen.findByText(NO_CONNECT, { exact: true })).toBeTruthy();
    expect(tokens).toEqual([]);
  });

  it("shows the call panel and leaves from it", async () => {
    let left = 0;
    crowd();
    server.use(
      apiHandler("delete", "/users/@me/voice", () => {
        left += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await lounge();
    await act(() => joinVoice(G, GAMES));
    const panel = await screen.findByRole("region", { name: "Голосовая связь" });
    expect(within(panel).getByText("Голос подключён")).toBeTruthy();
    expect(within(panel).getByRole("link", { name: /Игры/ })).toBeTruthy();
    await user.click(within(panel).getByRole("button", { name: "Отключиться" }));
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Голосовая связь" })).toBeNull();
    });
    expect(left).toBe(1);
  });

  it("switches the microphone and sound; undeafening brings the microphone back as it was", async () => {
    crowd();
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    const profile = await screen.findByRole("region", { name: "Ваш профиль" });
    await user.click(within(profile).getByRole("button", { name: "Выключить звук" }));
    expect(getCall()).toMatchObject({ selfDeaf: true, selfMute: false });
    expect(within(profile).getByRole("button", { name: "Включить микрофон" })).toHaveProperty("ariaPressed", "true");
    await user.click(within(profile).getByRole("button", { name: "Включить звук" }));
    expect(within(profile).getByRole("button", { name: "Выключить микрофон" })).toHaveProperty("ariaPressed", "false");
    await user.click(within(profile).getByRole("button", { name: "Выключить микрофон" }));
    expect(getCall().selfMute).toBe(true);
  });

  it("locks the microphone a moderator turned off", async () => {
    crowd(DEFAULT_EVERYONE, { mute: true });
    renderApp(`/channels/${G}/${TEXT}`);
    const profile = await screen.findByRole("region", { name: "Ваш профиль" });
    await waitFor(() => {
      expect(within(profile).getByRole("button", { name: MODERATOR_MUTED })).toHaveProperty("disabled", true);
    });
  });
});

describe("the voice channel screen", () => {
  const petya = makeUser("200", { display_name: "Петя" });
  const masha = makeUser("300", { display_name: "Маша" });

  beforeEach(() => {
    guildState = () =>
      makeGuildState(G, testUser, {
        channels: [
          makeChannel(TEXT, G, { name: "общий" }),
          makeChannel(LOUNGE, G, { type: 2, name: "Лаунж" }),
          makeChannel(GAMES, G, { type: 2, name: "Игры" }),
        ],
        members: [makeMember(testUser), makeMember(petya), makeMember(masha)],
        voice_states: [makeVoiceState(G, LOUNGE, petya.id), makeVoiceState(G, LOUNGE, masha.id, { self_stream: true })],
      });
  });

  const stage = () => screen.findByRole("region", { name: "Голосовой канал Лаунж" });

  it("opens a voice channel on its own screen with who is there and a way in", async () => {
    const { user } = renderApp(`/channels/${G}/${LOUNGE}`);
    const view = await stage();
    expect(screen.getByRole("heading", { level: 1, name: "Лаунж" })).toBeTruthy();
    expect(within(view).getByText("Петя")).toBeTruthy();
    expect(within(view).getByText("Маша")).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: /Сообщение в/ })).toBeNull();
    await user.click(within(view).getByRole("button", { name: "Присоединиться" }));
    await waitFor(() => {
      expect(tokens).toEqual([LOUNGE]);
    });
  });

  it("opens the channel clicked in the sidebar", async () => {
    const { user, router } = renderApp(`/channels/${G}/${TEXT}`);
    await user.click(await screen.findByRole("button", { name: "Голосовой канал Лаунж" }));
    await stage();
    expect(router.state.location.pathname).toBe(`/channels/${G}/${LOUNGE}`);
  });

  it("watches a stream on demand, stops, and goes full screen", async () => {
    const full = vi.fn(() => Promise.resolve());
    HTMLElement.prototype.requestFullscreen = full;
    const { user } = renderApp(`/channels/${G}/${LOUNGE}`);
    const view = await stage();
    await act(() => joinVoice(G, LOUNGE));
    act(() => {
      fake.emit("streams", [masha.id]);
    });
    const tile = await within(view).findByRole("article", { name: "Маша — демонстрация экрана" });
    expect(fake.argsOf("watch")).toEqual([]);
    await user.click(within(tile).getByRole("button", { name: "Смотреть" }));
    expect(fake.argsOf("watch")).toEqual([[masha.id, true]]);
    const [[who, video]] = fake.argsOf("attachScreen") as [[string, HTMLVideoElement]];
    expect(who).toBe(masha.id);
    expect(video.tagName).toBe("VIDEO");
    await user.click(within(tile).getByRole("button", { name: "Во весь экран" }));
    expect(full).toHaveBeenCalledTimes(1);
    await user.click(within(tile).getByRole("button", { name: "Перестать смотреть" }));
    expect(fake.argsOf("watch").at(-1)).toEqual([masha.id, false]);
    expect(fake.argsOf("detachScreen")).toHaveLength(1);
  });

  it("drops a stream tile when the stream ends", async () => {
    const { user } = renderApp(`/channels/${G}/${LOUNGE}`);
    const view = await stage();
    await act(() => joinVoice(G, LOUNGE));
    act(() => {
      fake.emit("streams", [masha.id]);
    });
    const tile = await within(view).findByRole("article", { name: "Маша — демонстрация экрана" });
    await user.click(within(tile).getByRole("button", { name: "Смотреть" }));
    act(() => {
      fake.emit("streams", []);
    });
    await waitFor(() => {
      expect(within(view).queryByRole("article", { name: /демонстрация экрана/ })).toBeNull();
    });
    expect(getCall().watching).toEqual([]);
    expect(fake.argsOf("detachScreen")).toHaveLength(1);
  });

  it("rings whoever speaks on their tile", async () => {
    renderApp(`/channels/${G}/${LOUNGE}`);
    const view = await stage();
    await act(() => joinVoice(G, LOUNGE));
    act(() => {
      fake.emit("speaking", [petya.id]);
    });
    const tile = within(view).getByRole("listitem", { name: "Петя" });
    expect(tile.querySelector("[data-speaking]")).not.toBeNull();
  });
});

describe("screen sharing", () => {
  /** Server 10 owned by someone else, @everyone with `everyone`. */
  const withEveryone = (everyone: bigint) => {
    guildState = () =>
      makeGuildState(G, testUser, {
        guild: makeGuild(G, "999"),
        roles: [makeEveryone(G, formatMask(everyone))],
        channels: [makeChannel(TEXT, G, { name: "общий" }), makeChannel(LOUNGE, G, { type: 2, name: "Лаунж" })],
      });
  };

  const panel = () => screen.findByRole("region", { name: "Голосовая связь" });

  it("shares a screen and stops it", async () => {
    withEveryone(DEFAULT_EVERYONE);
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    await user.click(within(await panel()).getByRole("button", { name: "Демонстрация экрана" }));
    expect(fake.argsOf("startScreenShare")).toHaveLength(1);
    expect(getCall().sharing).toBe(true);
    await user.click(within(await panel()).getByRole("button", { name: "Остановить демонстрацию" }));
    expect(fake.argsOf("stopScreenShare")).toHaveLength(1);
    expect(getCall().sharing).toBe(false);
  });

  it("changes nothing and says nothing when the picker is closed", async () => {
    withEveryone(DEFAULT_EVERYONE);
    fake.screenResult = { ok: false, reason: "cancelled" };
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    await user.click(within(await panel()).getByRole("button", { name: "Демонстрация экрана" }));
    expect(getCall().sharing).toBe(false);
    expect(screen.queryByRole("status", { name: /Уведомление/ })).toBeNull();
  });

  it("says when a window is shared without its sound", async () => {
    withEveryone(DEFAULT_EVERYONE);
    fake.screenResult = { ok: true, surface: "window", audio: false };
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    await user.click(within(await panel()).getByRole("button", { name: "Демонстрация экрана" }));
    expect(await screen.findByText(WINDOW_NO_AUDIO, { exact: true })).toBeTruthy();
  });

  it("goes off the air when the browser stops the share", async () => {
    withEveryone(DEFAULT_EVERYONE);
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    await user.click(within(await panel()).getByRole("button", { name: "Демонстрация экрана" }));
    act(() => {
      fake.emit("screenEnded");
    });
    expect(getCall().sharing).toBe(false);
    expect(within(await panel()).getByRole("button", { name: "Демонстрация экрана" })).toBeTruthy();
  });

  it("can still be stopped after STREAM is taken away", async () => {
    withEveryone(DEFAULT_EVERYONE & ~PERM.STREAM);
    renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    act(() => {
      setCall({ sharing: true });
    });
    const stop = within(await panel()).getByRole("button", { name: "Остановить демонстрацию" });
    expect(stop).toHaveProperty("disabled", false);
  });

  it("is not offered without STREAM", async () => {
    withEveryone(DEFAULT_EVERYONE & ~PERM.STREAM);
    renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    expect(within(await panel()).getByRole("button", { name: NO_STREAM })).toHaveProperty("disabled", true);
  });
});

describe("voice moderation", () => {
  const petya = makeUser("200", { display_name: "Петя" });
  let patches: { user: string; body: unknown }[];

  beforeEach(() => {
    patches = [];
    // I own the server, so I act on anyone.
    guildState = () =>
      makeGuildState(G, testUser, {
        channels: [
          makeChannel(TEXT, G, { name: "общий" }),
          makeChannel(LOUNGE, G, { type: 2, name: "Лаунж" }),
          makeChannel(GAMES, G, { type: 2, name: "Игры", position: 1 }),
        ],
        members: [makeMember(testUser), makeMember(petya)],
        voice_states: [makeVoiceState(G, LOUNGE, petya.id)],
      });
    server.use(
      apiHandler("patch", "/guilds/{guild_id}/members/{user_id}/voice", async ({ request, params }) => {
        patches.push({ user: params.user_id, body: await request.json() });
        return new HttpResponse(null, { status: 204 });
      }),
    );
  });

  const petyaRow = async () =>
    within(await screen.findByRole("list", { name: "В канале Лаунж" })).getByRole("listitem");

  it("mutes, moves and disconnects a member in voice from their menu", async () => {
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await user.pointer({ keys: "[MouseRight]", target: await petyaRow() });
    await user.click(await screen.findByRole("menuitem", { name: "Выключить микрофон на сервере" }));
    await user.pointer({ keys: "[MouseRight]", target: await petyaRow() });
    await user.click(await screen.findByRole("menuitem", { name: "Переместить в «Игры»" }));
    await user.pointer({ keys: "[MouseRight]", target: await petyaRow() });
    await user.click(await screen.findByRole("menuitem", { name: "Отключить от голоса" }));
    await waitFor(() => {
      expect(patches).toEqual([
        { user: petya.id, body: { mute: true } },
        { user: petya.id, body: { channel_id: GAMES } },
        { user: petya.id, body: { channel_id: null } },
      ]);
    });
  });

  it("says why the server refused", async () => {
    server.use(apiHandler("patch", "/guilds/{guild_id}/members/{user_id}/voice", () => problem(403, "role_hierarchy")));
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await user.pointer({ keys: "[MouseRight]", target: await petyaRow() });
    await user.click(await screen.findByRole("menuitem", { name: "Выключить звук на сервере" }));
    expect(await screen.findByText(errorText(new ApiError(403, "role_hierarchy")), { exact: true })).toBeTruthy();
  });
});

describe("call notices outside the shell", () => {
  it("I8: tells why the call ended while the user is in the settings", async () => {
    const { router } = renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    await act(() => router.navigate({ to: "/settings/$", params: { _splat: "profile" } }));
    await screen.findByRole("heading", { name: "Профиль" });
    act(() => {
      fake.emit("link", "disconnected", "replaced");
    });
    expect(await screen.findByText(CALL_REPLACED, { exact: true })).toBeTruthy();
  });
});
