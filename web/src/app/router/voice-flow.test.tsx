import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, signOut, testUser } from "@/app/test";
import { getCall, resetCall, setCall } from "@/entities/call";
import { DEFAULT_EVERYONE, formatMask, PERM, resetGuilds } from "@/entities/guild";
import {
  CALL_REPLACED,
  CAMERA_DENIED,
  joinVoice,
  NO_CONNECT,
  NO_STREAM,
  NO_VIDEO,
  onStreamWatch,
  STREAM_GRACE_MS,
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
    apiHandler("put", "/channels/{channel_id}/voice/watching", () => new HttpResponse(null, { status: 204 })),
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
    await signOut(user);
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
          makeVoiceState(G, LOUNGE, petya.id, { self_mute: true, self_video: true }),
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
    expect(within(rows[0] as HTMLElement).getByRole("img", { name: "Камера включена" })).toBeTruthy();
    expect(within(rows[1] as HTMLElement).queryByRole("img", { name: "Камера включена" })).toBeNull();
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

  it("asks before leaving a call for another channel, as Discord does", async () => {
    crowd();
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("button", { name: "Голосовой канал Игры" });
    await act(() => joinVoice(G, LOUNGE));
    tokens.length = 0;
    await user.click(screen.getByRole("button", { name: "Голосовой канал Игры" }));
    const ask = await screen.findByRole("alertdialog", { name: "Перейти в другой голосовой канал?" });
    expect(within(ask).getByText("Вы сейчас в «Лаунж». Выйти оттуда и подключиться к «Игры»?")).toBeTruthy();
    await user.click(within(ask).getByRole("button", { name: "Отмена" }));
    expect(tokens).toEqual([]);
    expect(getCall().channelId).toBe(LOUNGE);
    await user.click(screen.getByRole("button", { name: "Голосовой канал Игры" }));
    await user.click(await screen.findByRole("button", { name: "Перейти" }));
    await waitFor(() => {
      expect(tokens).toEqual([GAMES]);
    });
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

  it("shows the link's figures under the call's status", async () => {
    crowd();
    fake.stats = { ping: 212, jitter: 4, lossIn: 0.5, lossOut: null, upKbps: 48, downKbps: 120, route: "relay" };
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await lounge();
    await act(() => joinVoice(G, GAMES));
    const panel = await screen.findByRole("region", { name: "Голосовая связь" });
    await user.click(within(panel).getByRole("button", { name: /Голос подключён/ }));
    const details = await screen.findByRole("dialog");
    expect(await within(details).findByText("212 мс")).toBeTruthy();
    expect(within(details).getByText("0.5%")).toBeTruthy();
    expect(within(details).getByText("120 кбит/с")).toBeTruthy();
    expect(within(details).getByText("Через ретранслятор (TURN)")).toBeTruthy();
    // A figure not read yet says so instead of showing zero.
    expect(within(details).getByText("—")).toBeTruthy();
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
    // Watching brings the stream to the front, the others in the strip under it.
    expect(within(view).getByRole("button", { name: "Вернуть в сетку: Маша — демонстрация экрана" })).toBeTruthy();
    expect(within(view).getByRole("list", { name: "Остальные в звонке" })).toBeTruthy();
    const front = within(view).getByRole("article", { name: "Маша — демонстрация экрана" });
    const [[who, video]] = fake.argsOf("attachScreen").slice(-1) as [[string, HTMLVideoElement]];
    expect(who).toBe(masha.id);
    expect(video.tagName).toBe("VIDEO");
    await user.click(within(front).getByRole("button", { name: "Во весь экран" }));
    expect(full).toHaveBeenCalledTimes(1);
    // Full screen has its own way out on the picture, not only Esc.
    const exit = vi.fn(() => Promise.resolve());
    document.exitFullscreen = exit;
    Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => front });
    act(() => {
      document.dispatchEvent(new Event("fullscreenchange"));
    });
    await user.click(within(front).getByRole("button", { name: "Выйти из полноэкранного режима" }));
    expect(exit).toHaveBeenCalledTimes(1);
    Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => null });
    act(() => {
      document.dispatchEvent(new Event("fullscreenchange"));
    });
    expect(within(front).getByRole("button", { name: "Во весь экран" })).toBeTruthy();
    // On the whole screen again, even if the button missed the news: a press leaves it.
    Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => front });
    await user.click(within(front).getByRole("button", { name: "Во весь экран" }));
    expect(exit).toHaveBeenCalledTimes(2);
    expect(full).toHaveBeenCalledTimes(1);
    Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => null });
    await user.click(within(front).getByRole("button", { name: "Перестать смотреть" }));
    expect(fake.argsOf("watch").at(-1)).toEqual([masha.id, false]);
    expect(fake.argsOf("detachScreen")).toHaveLength(1);
  });

  it("brings a tile to the front on a click and sends it back on another", async () => {
    const { user } = renderApp(`/channels/${G}/${LOUNGE}`);
    const view = await stage();
    await act(() => joinVoice(G, LOUNGE));
    await user.click(within(view).getByRole("button", { name: "Развернуть: Петя" }));
    expect(within(view).getByRole("button", { name: "Вернуть в сетку: Петя" })).toHaveProperty("ariaPressed", "true");
    expect(within(view).getByRole("list", { name: "Остальные в звонке" })).toBeTruthy();
    await user.click(within(view).getByRole("button", { name: "Вернуть в сетку: Петя" }));
    expect(within(view).queryByRole("list", { name: "Остальные в звонке" })).toBeNull();
    expect(within(view).getByRole("button", { name: "Развернуть: Петя" })).toBeTruthy();
  });

  it("opens a person's menu with their volume on a right click on their tile", async () => {
    renderApp(`/channels/${G}/${LOUNGE}`);
    const view = await stage();
    await act(() => joinVoice(G, LOUNGE));
    fireEvent.contextMenu(within(view).getByRole("button", { name: "Развернуть: Петя" }));
    const menu = await screen.findByRole("menu");
    expect(within(menu).getByRole("slider", { name: "Громкость пользователя" })).toBeTruthy();
    expect(within(menu).queryByRole("slider", { name: "Громкость трансляции" })).toBeNull();
    // The tile stays where it was: a right click is not a click.
    expect(within(view).queryByRole("list", { name: "Остальные в звонке" })).toBeNull();
  });

  it("offers a screen share's volume in the menu of the person sharing it", async () => {
    renderApp(`/channels/${G}/${LOUNGE}`);
    const view = await stage();
    await act(() => joinVoice(G, LOUNGE));
    fireEvent.contextMenu(within(view).getByRole("button", { name: "Развернуть: Маша" }));
    expect(within(await screen.findByRole("menu")).getByRole("slider", { name: "Громкость трансляции" })).toBeTruthy();
  });

  it("sets volumes only in the call, not from the sidebar", async () => {
    renderApp(`/channels/${G}/${LOUNGE}`);
    await stage();
    await act(() => joinVoice(G, LOUNGE));
    const row = within(screen.getByRole("list", { name: "В канале Лаунж" })).getAllByRole("listitem")[0];
    if (!row) throw new Error("no voice row");
    fireEvent.contextMenu(row);
    const menu = await screen.findByRole("menu");
    expect(within(menu).queryByRole("slider")).toBeNull();
  });

  it("has the call's controls under the stage", async () => {
    const { user } = renderApp(`/channels/${G}/${LOUNGE}`);
    const view = await stage();
    await act(() => joinVoice(G, LOUNGE));
    const bar = within(view).getByRole("toolbar", { name: "Управление звонком" });
    await user.click(within(bar).getByRole("button", { name: "Выключить микрофон" }));
    expect(getCall().selfMute).toBe(true);
    expect(within(bar).getByRole("button", { name: "Включить микрофон" })).toHaveProperty("ariaPressed", "true");
    await user.click(within(bar).getByRole("button", { name: "Отключиться" }));
    await waitFor(() => {
      expect(getCall().status).toBe("idle");
    });
  });

  it("drops a stream tile when the stream ends", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    onTestFinished(() => {
      vi.useRealTimers();
    });
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
    // A watched stream is held a moment for its return.
    await act(() => vi.advanceTimersByTimeAsync(STREAM_GRACE_MS));
    await waitFor(() => {
      expect(within(view).queryByRole("article", { name: /демонстрация экрана/ })).toBeNull();
    });
    expect(getCall().watching).toEqual([]);
    expect(fake.argsOf("detachScreen")).toHaveLength(1);
  });

  it("shows the sharer who watches their stream", async () => {
    renderApp(`/channels/${G}/${LOUNGE}`);
    const view = await stage();
    await act(() => joinVoice(G, LOUNGE));
    act(() => {
      setCall({ sharing: true });
      onStreamWatch({ channel_id: LOUNGE, user_id: petya.id, watching: true });
      onStreamWatch({ channel_id: LOUNGE, user_id: masha.id, watching: true });
    });
    const own = await within(view).findByRole("article", { name: "Ваша демонстрация экрана" });
    expect(within(own).getByRole("status", { name: "Смотрят 2: Петя, Маша" })).toBeTruthy();
    act(() => {
      onStreamWatch({ channel_id: LOUNGE, user_id: petya.id, watching: false });
    });
    expect(within(own).getByRole("status", { name: "Смотрит 1: Маша" })).toBeTruthy();
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

  /** A browser that can share a screen (jsdom has no mediaDevices); a phone's cannot. */
  const canShare = (can: boolean) => {
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: {
        getSupportedConstraints: () => ({}),
        ...(can ? { getDisplayMedia: () => Promise.reject(new Error("fake")) } : {}),
      },
    });
  };
  beforeEach(() => {
    canShare(true);
  });
  afterEach(() => {
    Reflect.deleteProperty(navigator, "mediaDevices");
  });

  it("offers no share where the browser cannot share (a phone)", async () => {
    canShare(false);
    withEveryone(DEFAULT_EVERYONE);
    renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    const p = await panel();
    expect(within(p).queryByRole("button", { name: "Демонстрация экрана" })).toBeNull();
    expect(within(p).getByRole("button", { name: "Отключиться" })).toBeTruthy();
  });

  it("shares a screen, switches it and stops it", async () => {
    withEveryone(DEFAULT_EVERYONE);
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    await user.click(within(await panel()).getByRole("button", { name: "Демонстрация экрана" }));
    expect(fake.argsOf("startScreenShare")).toHaveLength(1);
    expect(getCall().sharing).toBe(true);
    // During a share the button opens a menu, as Discord's.
    await user.click(within(await panel()).getByRole("button", { name: "Демонстрация экрана" }));
    await user.click(await screen.findByRole("menuitem", { name: "Сменить источник" }));
    expect(fake.argsOf("switchScreenShare")).toHaveLength(1);
    expect(getCall().sharing).toBe(true);
    await user.click(within(await panel()).getByRole("button", { name: "Демонстрация экрана" }));
    await user.click(await screen.findByRole("menuitem", { name: "Остановить демонстрацию" }));
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
    const { user } = renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    act(() => {
      setCall({ sharing: true });
    });
    await user.click(within(await panel()).getByRole("button", { name: "Демонстрация экрана" }));
    expect(await screen.findByRole("menuitem", { name: "Остановить демонстрацию" })).toBeTruthy();
    // Something else may not be picked without STREAM.
    expect(screen.queryByRole("menuitem", { name: "Сменить источник" })).toBeNull();
  });

  it("is not offered without STREAM", async () => {
    withEveryone(DEFAULT_EVERYONE & ~PERM.STREAM);
    renderApp(`/channels/${G}/${TEXT}`);
    await screen.findByRole("textbox", { name: "Сообщение в #общий" });
    await act(() => joinVoice(G, LOUNGE));
    expect(within(await panel()).getByRole("button", { name: NO_STREAM })).toHaveProperty("disabled", true);
  });
});

describe("the camera", () => {
  const petya = makeUser("200", { display_name: "Петя" });

  /** Server 10 owned by someone else, @everyone with `everyone`, Петя in the lounge. */
  const withEveryone = (everyone: bigint) => {
    guildState = () =>
      makeGuildState(G, testUser, {
        guild: makeGuild(G, "999"),
        roles: [makeEveryone(G, formatMask(everyone))],
        channels: [makeChannel(TEXT, G, { name: "общий" }), makeChannel(LOUNGE, G, { type: 2, name: "Лаунж" })],
        members: [makeMember(testUser), makeMember(petya)],
        voice_states: [makeVoiceState(G, LOUNGE, petya.id)],
      });
  };

  /** A browser with a camera API (jsdom has no mediaDevices). */
  beforeEach(() => {
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: { getSupportedConstraints: () => ({}), getUserMedia: () => Promise.reject(new Error("fake")) },
    });
  });
  afterEach(() => {
    Reflect.deleteProperty(navigator, "mediaDevices");
  });

  const controls = () => screen.findByRole("toolbar", { name: "Управление звонком" });

  it("turns the camera on from the call's controls and shows it in the user's own tile", async () => {
    withEveryone(DEFAULT_EVERYONE);
    const { user } = renderApp(`/channels/${G}/${LOUNGE}`);
    await screen.findByRole("region", { name: "Голосовой канал Лаунж" });
    await act(() => joinVoice(G, LOUNGE));
    act(() => {
      send("VOICE_STATE_UPDATE", { ...makeVoiceState(G, LOUNGE, testUser.id), left: false });
    });
    await user.click(within(await controls()).getByRole("button", { name: "Включить камеру" }));
    expect(fake.argsOf("startCamera")).toHaveLength(1);
    await waitFor(() => {
      expect(fake.argsOf("attachOwnCamera")).toHaveLength(1);
    });
    await user.click(within(await controls()).getByRole("button", { name: "Выключить камеру" }));
    expect(fake.argsOf("stopCamera")).toHaveLength(1);
    await waitFor(() => {
      expect(fake.argsOf("detachOwnCamera")).toHaveLength(1);
    });
  });

  it("shows someone's camera in their tile while it is on", async () => {
    withEveryone(DEFAULT_EVERYONE);
    renderApp(`/channels/${G}/${LOUNGE}`);
    await screen.findByRole("region", { name: "Голосовой канал Лаунж" });
    await act(() => joinVoice(G, LOUNGE));
    act(() => {
      fake.emit("cameras", [petya.id]);
    });
    await waitFor(() => {
      expect(fake.argsOf("attachCamera").map(([id]) => id)).toEqual([petya.id]);
    });
    expect(within(screen.getByRole("listitem", { name: "Петя" })).getByText("Петя")).toBeTruthy();
    act(() => {
      fake.emit("cameras", []);
    });
    await waitFor(() => {
      expect(fake.argsOf("detachCamera").map(([id]) => id)).toEqual([petya.id]);
    });
  });

  it("says why the camera stayed off", async () => {
    withEveryone(DEFAULT_EVERYONE);
    fake.cameraResult = { ok: false, reason: "denied" };
    const { user } = renderApp(`/channels/${G}/${LOUNGE}`);
    await screen.findByRole("region", { name: "Голосовой канал Лаунж" });
    await act(() => joinVoice(G, LOUNGE));
    await user.click(within(await controls()).getByRole("button", { name: "Включить камеру" }));
    expect(await screen.findByText(CAMERA_DENIED, { exact: true })).toBeTruthy();
    expect(getCall().camera).toBe(false);
  });

  it("is not offered without STREAM", async () => {
    withEveryone(DEFAULT_EVERYONE & ~PERM.STREAM);
    renderApp(`/channels/${G}/${LOUNGE}`);
    await screen.findByRole("region", { name: "Голосовой канал Лаунж" });
    await act(() => joinVoice(G, LOUNGE));
    expect(within(await controls()).getByRole("button", { name: NO_VIDEO })).toHaveProperty("disabled", true);
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
