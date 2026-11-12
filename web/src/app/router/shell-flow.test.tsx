import { screen, waitFor, within } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { HttpResponse } from "msw";
import { currentHref, renderApp, sessionHandler, testUser } from "@/app/test";
import { resetChannelUi } from "@/entities/channel";
import { getGuildsState, resetGuilds } from "@/entities/guild";
import {
  apiHandler,
  dispatchFrame,
  makeChannel,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeMember,
  makeReady,
  makeUser,
  readyGateway,
  server,
  setViewportWidth,
} from "@/shared/test";

type Client = WebSocketHandlerConnection["client"];

const owner = makeUser("1");
const G = "10";
const general = makeChannel("101", G, { name: "общий", position: 0 });
const memes = makeChannel("102", G, { name: "мемы", position: 1 });

/** Server 10 owned by someone else, as testUser sees it with these channels. */
const guildWith = (channels = [general, memes]) => {
  const me = makeMember(testUser);
  return makeGuildState(G, testUser, {
    guild: makeGuild(G, owner.id, { name: "Вечерний сеанс" }),
    roles: [makeEveryone(G)],
    channels,
    members: [me, makeMember(owner)],
    my_member: me,
  });
};

/** Signs in, answers READY with `guilds` and hands back the socket for later frames. */
const signedIn = (guilds = [guildWith()]) => {
  let client: Client | undefined;
  server.use(
    sessionHandler(testUser),
    readyGateway(makeReady(testUser, guilds), (c) => {
      client = c;
    }),
  );
  let seq = 1;
  return (t: string, d: unknown) => {
    if (!client) throw new Error("the gateway is not connected");
    seq += 1;
    client.send(dispatchFrame(t, seq, d));
  };
};

const channelHeading = (name: string) => screen.findByRole("heading", { level: 1, name });

afterEach(() => {
  resetChannelUi();
  resetGuilds();
});

describe("app shell", () => {
  it("home is the friends screen, and offers to add one when there are none", async () => {
    signedIn([]);
    renderApp("/channels/@me");
    const main = await screen.findByRole("main");
    expect(await within(main).findByRole("heading", { level: 1, name: "Друзья" })).toBeTruthy();
    expect(within(main).getByRole("tab", { name: "В сети", selected: true })).toBeTruthy();
    expect(within(main).getByText("Никого из друзей сейчас нет в сети.")).toBeTruthy();
    expect(within(main).getByRole("button", { name: "Добавить в друзья" })).toBeTruthy();
    expect(within(main).queryByText("Главная")).toBeNull();
  });

  it("opens a server from the rail on its first text channel and shows the channel's header", async () => {
    signedIn();
    const { user, router } = renderApp("/channels/@me");
    await user.click(await screen.findByRole("link", { name: "Вечерний сеанс" }));
    await waitFor(() => {
      expect(currentHref(router)).toBe(`/channels/${G}/101`);
    });
    expect(await channelHeading("общий")).toBeTruthy();
  });

  it("creates a server: it appears by GUILD_CREATE and its first text channel opens", async () => {
    const send = signedIn([]);
    const created = makeGuild("50", testUser.id, { name: "Дача" });
    server.use(
      apiHandler("post", "/guilds", () => {
        setTimeout(() => {
          send(
            "GUILD_CREATE",
            makeGuildState("50", testUser, {
              guild: created,
              channels: [
                makeChannel("60", "50", { type: 4, name: "Текстовые каналы", position: 0 }),
                makeChannel("61", "50", { type: 4, name: "Голосовые каналы", position: 1 }),
                makeChannel("62", "50", { name: "общий", parent_id: "60" }),
                makeChannel("63", "50", { type: 2, name: "Основной", parent_id: "61" }),
              ],
            }),
          );
        }, 20);
        return HttpResponse.json(created, { status: 201 });
      }),
    );
    const { user, router } = renderApp("/channels/@me");
    const rail = await screen.findByRole("navigation", { name: "Серверы" });
    await user.click(within(rail).getByRole("button", { name: "Создать сервер" }));
    await user.type(await screen.findByLabelText("Название сервера"), "Дача");
    await user.click(screen.getByRole("button", { name: "Создать" }));

    await waitFor(() => {
      expect(currentHref(router)).toBe("/channels/50/62");
    });
    const railNow = screen.getByRole("navigation", { name: "Серверы" });
    expect(within(railNow).getByRole("link", { name: "Дача" }).getAttribute("aria-current")).toBe("page");
    expect(await channelHeading("общий")).toBeTruthy();
  });

  describe("loss of access", () => {
    it("moves to the nearest channel with a toast when a GUILD_SYNC hides the open one", async () => {
      const send = signedIn();
      const { router } = renderApp(`/channels/${G}/102`);
      expect(await channelHeading("мемы")).toBeTruthy();

      send("GUILD_SYNC", guildWith([general]));

      await waitFor(() => {
        expect(currentHref(router)).toBe(`/channels/${G}/101`);
      });
      expect(await screen.findByText("Канал больше недоступен")).toBeTruthy();
      expect(await channelHeading("общий")).toBeTruthy();
    });

    it("goes home with a toast when the open server is deleted", async () => {
      const send = signedIn();
      const { router } = renderApp(`/channels/${G}/101`);
      expect(await channelHeading("общий")).toBeTruthy();

      send("GUILD_DELETE", { id: G });

      await waitFor(() => {
        expect(currentHref(router)).toBe("/channels/@me");
      });
      expect(await screen.findByText("Сервер больше недоступен")).toBeTruthy();
    });

    it("stays put when a GUILD_SYNC only adds access", async () => {
      const send = signedIn([guildWith([general])]);
      const { router } = renderApp(`/channels/${G}/101`);
      expect(await channelHeading("общий")).toBeTruthy();

      send("GUILD_SYNC", guildWith([general, memes]));

      expect(await screen.findByRole("link", { name: "мемы" })).toBeTruthy();
      expect(currentHref(router)).toBe(`/channels/${G}/101`);
      expect(screen.queryByText("Канал больше недоступен")).toBeNull();
    });

    it("waits for READY before deciding a channel from the address is gone", async () => {
      // A channel the user may not see: once the store is known, it is left without a toast.
      signedIn();
      const { router } = renderApp(`/channels/${G}/999`);
      await waitFor(() => {
        expect(currentHref(router)).toBe(`/channels/${G}/101`);
      });
      expect(screen.queryByText("Канал больше недоступен")).toBeNull();
    });
  });
});

/** A second server, 20, with its channel «кино». */
const other = () =>
  makeGuildState("20", testUser, {
    guild: makeGuild("20", owner.id, { name: "Книжный клуб" }),
    channels: [makeChannel("201", "20", { name: "кино" })],
    members: [makeMember(testUser), makeMember(owner)],
    my_member: makeMember(testUser),
  });

/** The toast texts on screen now (the connection banner is the other live region). */
const toastCount = (text: string) => screen.queryAllByText(text).length;

describe("switching servers", () => {
  it("keeps the shell mounted and the member list hidden, going straight to the server's channel", async () => {
    setViewportWidth(1440);
    signedIn([guildWith(), other()]);
    const { user, router } = renderApp(`/channels/${G}/101`);
    expect(await channelHeading("общий")).toBeTruthy();
    const rail = screen.getByRole("navigation", { name: "Серверы" });
    await user.click(screen.getByRole("button", { name: "Скрыть участников" }));
    expect(screen.queryByRole("complementary", { name: "Участники" })).toBeNull();

    const club = within(rail).getByRole("link", { name: "Книжный клуб" });
    expect(club.getAttribute("href")).toBe("/channels/20/201");
    const visited: string[] = [];
    const stop = router.history.subscribe(({ location }) => {
      visited.push(location.pathname);
    });
    await user.click(club);

    expect(await channelHeading("кино")).toBeTruthy();
    stop();
    expect(visited).toEqual(["/channels/20/201"]);
    // The very same rail element: nothing around the channel was mounted again.
    expect(screen.getByRole("navigation", { name: "Серверы" })).toBe(rail);
    expect(screen.getByRole("button", { name: "Показать участников" }).getAttribute("aria-pressed")).toBe("false");
    expect(screen.queryByRole("complementary", { name: "Участники" })).toBeNull();
  });
});

describe("before READY", () => {
  it("leaves out the member column while the server is not known", async () => {
    setViewportWidth(1440);
    // The default gateway never sends READY.
    server.use(sessionHandler(testUser));
    renderApp(`/channels/${G}/101`);
    expect(await screen.findByRole("navigation", { name: "Серверы" })).toBeTruthy();
    expect(document.querySelector('[class*="--members-width"]')).toBeNull();
  });
});

describe("loss of access, more cases", () => {
  it("says it once: exactly one toast when a GUILD_SYNC hides the open channel", async () => {
    const send = signedIn();
    const { router } = renderApp(`/channels/${G}/102`);
    expect(await channelHeading("мемы")).toBeTruthy();
    send("GUILD_SYNC", guildWith([general]));
    await waitFor(() => {
      expect(currentHref(router)).toBe(`/channels/${G}/101`);
    });
    await screen.findByText("Канал больше недоступен");
    await new Promise((r) => setTimeout(r, 50));
    expect(toastCount("Канал больше недоступен")).toBe(1);
  });

  it("moves away when a GUILD_SYNC keeps the channel but denies VIEW_CHANNEL on it", async () => {
    const send = signedIn();
    const { router } = renderApp(`/channels/${G}/102`);
    expect(await channelHeading("мемы")).toBeTruthy();
    const deny = { id: G, type: 0 as const, allow: "0", deny: String(1 << 10) };
    send("GUILD_SYNC", guildWith([general, { ...memes, permission_overwrites: [deny] }]));
    await waitFor(() => {
      expect(currentHref(router)).toBe(`/channels/${G}/101`);
    });
    expect(await screen.findByText("Канал больше недоступен")).toBeTruthy();
  });

  it("goes home with a toast when the server is deleted while its channel-less view is open", async () => {
    const voiceOnly = makeChannel("103", G, { name: "Кинозал", type: 2 });
    const send = signedIn([guildWith([voiceOnly])]);
    const { router } = renderApp(`/channels/${G}`);
    expect(await screen.findByText("На этом сервере нет текстовых каналов, доступных вам.")).toBeTruthy();
    send("GUILD_DELETE", { id: G });
    await waitFor(() => {
      expect(currentHref(router)).toBe("/channels/@me");
    });
    expect(await screen.findByText("Сервер больше недоступен")).toBeTruthy();
  });

  it("does not move when a GUILD_SYNC gives the channel-less view a text channel", async () => {
    const voiceOnly = makeChannel("103", G, { name: "Кинозал", type: 2 });
    const send = signedIn([guildWith([voiceOnly])]);
    const { router } = renderApp(`/channels/${G}`);
    expect(await screen.findByText("На этом сервере нет текстовых каналов, доступных вам.")).toBeTruthy();
    send("GUILD_SYNC", guildWith([voiceOnly, general]));
    expect(await screen.findByRole("link", { name: "общий" })).toBeTruthy();
    await new Promise((r) => setTimeout(r, 50));
    expect(currentHref(router)).toBe(`/channels/${G}`);
  });

  it("goes home with a toast when a fresh READY after a reconnect no longer has the server", async () => {
    server.use(
      sessionHandler(testUser),
      readyGateway(
        (n) => makeReady(testUser, n === 1 ? [guildWith()] : [other()]),
        (client, n) => {
          if (n === 1) {
            setTimeout(() => {
              client.close(1012, "service_restart");
            }, 50);
          }
        },
      ),
    );
    const { router } = renderApp(`/channels/${G}/101`);
    expect(await channelHeading("общий")).toBeTruthy();
    await waitFor(
      () => {
        expect(currentHref(router)).toBe("/channels/@me");
      },
      { timeout: 3000 },
    );
    expect(await screen.findByText("Сервер больше недоступен")).toBeTruthy();
    expect(getGuildsState().order).toEqual(["20"]);
  });

  it("says nothing about lost access when the user signs out from a channel", async () => {
    signedIn();
    server.use(apiHandler("post", "/auth/logout", () => new HttpResponse(null, { status: 204 })));
    const { user } = renderApp(`/channels/${G}/101`);
    expect(await channelHeading("общий")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Выйти" }));
    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByText("Сервер больше недоступен")).toBeNull();
    expect(screen.queryByText("Канал больше недоступен")).toBeNull();
  });
});
