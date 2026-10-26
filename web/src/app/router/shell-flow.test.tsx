import { screen, waitFor, within } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { HttpResponse } from "msw";
import { currentHref, renderApp, sessionHandler, testUser } from "@/app/test";
import { resetChannelUi } from "@/entities/channel";
import { DEFAULT_EVERYONE, formatMask } from "@/entities/guild";
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
    roles: [makeEveryone(G, formatMask(DEFAULT_EVERYONE))],
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
});

describe("app shell", () => {
  it("home says where to start, with the buttons to create and to join a server", async () => {
    signedIn([]);
    renderApp("/channels/@me");
    const main = await screen.findByRole("main");
    expect(await within(main).findByText("Выберите сервер слева или создайте свой")).toBeTruthy();
    expect(within(main).getByRole("button", { name: "Создать сервер" })).toBeTruthy();
    expect(within(main).getByRole("button", { name: "Присоединиться к серверу" })).toBeTruthy();
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
