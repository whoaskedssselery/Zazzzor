import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse } from "msw";
import { resetChannelUi, useChannelUi } from "@/entities/channel";
import { applyGatewayDispatch, DEFAULT_EVERYONE, formatMask, PERM, resetGuilds } from "@/entities/guild";
import {
  apiHandler,
  makeChannel,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeMember,
  makeReady,
  makeUser,
  renderRouted,
  server,
} from "@/shared/test";
import { ChannelSidebar } from "./ChannelSidebar";

const owner = makeUser("1");
const me = makeUser("100");
const G = "10";

const channels = [
  makeChannel("30", G, { name: "Текстовые каналы", type: 4, position: 0 }),
  makeChannel("31", G, { name: "Голосовые каналы", type: 4, position: 1 }),
  makeChannel("101", G, { name: "общий", parent_id: "30", position: 0 }),
  makeChannel("102", G, { name: "мемы", parent_id: "30", position: 1 }),
  makeChannel("201", G, { name: "Кинозал", type: 2, parent_id: "31", position: 0 }),
];

/** Server 10 as `me` sees it: owned by `ownerId`, @everyone with `everyone`. */
const ready = ({ ownerId = owner.id, everyone = DEFAULT_EVERYONE } = {}) => {
  const member = makeMember(me);
  applyGatewayDispatch(
    "READY",
    makeReady(me, [
      makeGuildState(G, me, {
        guild: makeGuild(G, ownerId, { name: "Вечерний сеанс" }),
        roles: [makeEveryone(G, formatMask(everyone))],
        channels,
        members: [member, makeMember(owner)],
        my_member: member,
      }),
    ]),
  );
};

afterEach(() => {
  resetGuilds();
  resetChannelUi();
});

const list = () => within(screen.getByRole("navigation", { name: "Каналы" }));

describe("ChannelSidebar", () => {
  it("lists the categories and their channels; text channels link to the channel", async () => {
    ready();
    await renderRouted(<ChannelSidebar guildId={G} channelId="101" />);
    expect(list().getByRole("button", { name: "Текстовые каналы" })).toBeTruthy();
    const general = list().getByRole("link", { name: "общий" });
    expect(general.getAttribute("href")).toBe(`/channels/${G}/101`);
    expect(general.getAttribute("aria-current")).toBe("page");
    expect(list().getByRole("link", { name: "мемы" }).getAttribute("aria-current")).toBeNull();
  });

  it("collapses a category and remembers it", async () => {
    ready();
    const { user, unmount } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />);
    const category = list().getByRole("button", { name: "Текстовые каналы" });
    expect(category.getAttribute("aria-expanded")).toBe("true");

    await user.click(category);

    expect(category.getAttribute("aria-expanded")).toBe("false");
    expect(list().queryByRole("link", { name: "мемы" })).toBeNull();
    // The open channel stays in sight, as in Discord.
    expect(list().getByRole("link", { name: "общий" })).toBeTruthy();
    expect(useChannelUi.getState().collapsed["30"]).toBe(true);

    unmount();
    await renderRouted(<ChannelSidebar guildId={G} channelId="101" />);
    expect(list().getByRole("button", { name: "Текстовые каналы" }).getAttribute("aria-expanded")).toBe("false");
  });

  it("offers «Пригласить людей» only with CREATE_INVITE", async () => {
    ready();
    const { user, unmount } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />);
    await user.click(screen.getByRole("button", { name: "Меню сервера Вечерний сеанс" }));
    expect(await screen.findByRole("menuitem", { name: "Пригласить людей" })).toBeTruthy();
    await user.keyboard("{Escape}");
    unmount();

    ready({ everyone: DEFAULT_EVERYONE & ~PERM.CREATE_INVITE });
    await renderRouted(<ChannelSidebar guildId={G} channelId="101" />);
    await user.click(screen.getByRole("button", { name: "Меню сервера Вечерний сеанс" }));
    expect(await screen.findByRole("menuitem", { name: "Покинуть сервер" })).toBeTruthy();
    expect(screen.queryByRole("menuitem", { name: "Пригласить людей" })).toBeNull();
  });

  it("opens the invite dialog for the open channel", async () => {
    ready();
    const { user } = await renderRouted(<ChannelSidebar guildId={G} channelId="102" />);
    await user.click(screen.getByRole("button", { name: "Меню сервера Вечерний сеанс" }));
    await user.click(await screen.findByRole("menuitem", { name: "Пригласить людей" }));
    expect(await screen.findByRole("dialog", { name: "Пригласить друзей в #мемы" })).toBeTruthy();
  });

  it("lets a member leave the server after confirming, and goes home", async () => {
    ready();
    let left: string | undefined;
    server.use(
      apiHandler("delete", "/users/@me/guilds/{guild_id}", ({ params }) => {
        left = params.guild_id;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user, router } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />, {
      path: `/channels/${G}/101`,
    });
    await user.click(screen.getByRole("button", { name: "Меню сервера Вечерний сеанс" }));
    await user.click(await screen.findByRole("menuitem", { name: "Покинуть сервер" }));

    const confirm = await screen.findByRole("alertdialog", { name: "Покинуть «Вечерний сеанс»?" });
    expect(left).toBeUndefined();
    await user.click(within(confirm).getByRole("button", { name: "Покинуть сервер" }));

    await waitFor(() => {
      expect(left).toBe(G);
    });
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me");
    });
    expect(await screen.findByText("Вы покинули сервер «Вечерний сеанс»")).toBeTruthy();
  });

  it("does not offer leaving to the owner", async () => {
    ready({ ownerId: me.id });
    const { user } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />);
    await user.click(screen.getByRole("button", { name: "Меню сервера Вечерний сеанс" }));
    expect(await screen.findByRole("menuitem", { name: "Пригласить людей" })).toBeTruthy();
    expect(screen.queryByRole("menuitem", { name: "Покинуть сервер" })).toBeNull();
  });
});

describe("server settings in the menu", () => {
  it("opens the first section the user may open", async () => {
    ready({ everyone: DEFAULT_EVERYONE | PERM.BAN_MEMBERS });
    const { user, router } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />, {
      path: "/channels/10/101",
    });
    await user.click(screen.getByRole("button", { name: "Меню сервера Вечерний сеанс" }));
    await user.click(await screen.findByRole("menuitem", { name: "Настройки сервера" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/guilds/10/settings/members");
    });
  });

  it("is not offered when no section is open to the user", async () => {
    ready();
    const { user } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />, { path: "/channels/10/101" });
    await user.click(screen.getByRole("button", { name: "Меню сервера Вечерний сеанс" }));
    await screen.findByRole("menu");
    expect(screen.queryByRole("menuitem", { name: "Настройки сервера" })).toBeNull();
  });
});

describe("arranging channels", () => {
  const manager = DEFAULT_EVERYONE | PERM.MANAGE_CHANNELS;

  it("creates a channel in a category from its «+»", async () => {
    const bodies: unknown[] = [];
    server.use(
      apiHandler("post", "/guilds/{guild_id}/channels", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(makeChannel("103", G, { name: "кино" }), { status: 201 });
      }),
    );
    ready({ everyone: manager });
    const { user } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />, { path: "/channels/10/101" });
    await user.click(screen.getByRole("button", { name: "Создать канал в Текстовые каналы" }));
    const dialog = await screen.findByRole("dialog", { name: "Создать канал" });
    await user.click(within(dialog).getByRole("radio", { name: /Голосовой/ }));
    await user.type(within(dialog).getByRole("textbox", { name: "Название канала" }), "Кинозал 2");
    await user.click(within(dialog).getByRole("button", { name: "Создать" }));
    await waitFor(() => {
      expect(bodies).toEqual([{ name: "Кинозал 2", type: 2, parent_id: "30" }]);
    });
  });

  it("creates a category from the server menu", async () => {
    const bodies: unknown[] = [];
    server.use(
      apiHandler("post", "/guilds/{guild_id}/channels", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(makeChannel("40", G, { type: 4 }), { status: 201 });
      }),
    );
    ready({ everyone: manager });
    const { user } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />, { path: "/channels/10/101" });
    await user.click(screen.getByRole("button", { name: "Меню сервера Вечерний сеанс" }));
    await user.click(await screen.findByRole("menuitem", { name: "Создать категорию" }));
    const dialog = await screen.findByRole("dialog", { name: "Создать категорию" });
    await user.type(within(dialog).getByRole("textbox", { name: "Название категории" }), "Игры");
    await user.click(within(dialog).getByRole("button", { name: "Создать" }));
    await waitFor(() => {
      expect(bodies).toEqual([{ name: "Игры", type: 4, parent_id: null }]);
    });
  });

  it("moves a channel with Alt+↓ as one PATCH", async () => {
    const bodies: unknown[] = [];
    server.use(
      apiHandler("patch", "/guilds/{guild_id}/channels", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json([]);
      }),
    );
    ready({ everyone: manager });
    const { user } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />, { path: "/channels/10/101" });
    screen.getByRole("link", { name: "общий" }).focus();
    await user.keyboard("{Alt>}{ArrowDown}{/Alt}");
    await waitFor(() => {
      expect(bodies).toEqual([[{ id: "101", position: 9999 }]]);
    });
  });

  it("offers no arranging and no gear without the permissions", async () => {
    ready();
    const { user } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />, { path: "/channels/10/101" });
    expect(screen.queryByRole("button", { name: /^Создать канал в/ })).toBeNull();
    expect(screen.queryByRole("link", { name: /^Настройки канала/ })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Меню сервера Вечерний сеанс" }));
    await screen.findByRole("menu");
    expect(screen.queryByRole("menuitem", { name: "Создать канал" })).toBeNull();
  });

  it("links each channel to its settings with MANAGE_CHANNELS", async () => {
    ready({ everyone: manager });
    await renderRouted(<ChannelSidebar guildId={G} channelId="101" />, { path: "/channels/10/101" });
    expect(screen.getByRole("link", { name: "Настройки канала мемы" }).getAttribute("href")).toBe(
      "/guilds/10/channels/102/settings/overview",
    );
  });
});
