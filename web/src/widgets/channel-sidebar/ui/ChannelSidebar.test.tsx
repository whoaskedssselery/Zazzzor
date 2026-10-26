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

  it("says voice channels come in the next update when one is clicked", async () => {
    ready();
    const { user, router } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />, {
      path: `/channels/${G}/101`,
    });
    await user.click(list().getByRole("button", { name: "Кинозал" }));
    expect(await screen.findByText("Голосовые каналы появятся в следующем обновлении")).toBeTruthy();
    expect(router.state.location.pathname).toBe(`/channels/${G}/101`);
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
  });

  it("does not offer leaving to the owner", async () => {
    ready({ ownerId: me.id });
    const { user } = await renderRouted(<ChannelSidebar guildId={G} channelId="101" />);
    await user.click(screen.getByRole("button", { name: "Меню сервера Вечерний сеанс" }));
    expect(await screen.findByRole("menuitem", { name: "Пригласить людей" })).toBeTruthy();
    expect(screen.queryByRole("menuitem", { name: "Покинуть сервер" })).toBeNull();
  });
});
