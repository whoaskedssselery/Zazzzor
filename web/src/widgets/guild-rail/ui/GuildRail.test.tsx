import { screen, within } from "@testing-library/react";
import { resetChannelUi, useChannelUi } from "@/entities/channel";
import { applyGatewayDispatch, resetGuilds } from "@/entities/guild";
import {
  makeChannel,
  makeGuild,
  makeGuildState,
  makeReady,
  makeUser,
  makeVoiceState,
  renderRouted,
} from "@/shared/test";
import { GuildRail } from "./GuildRail";

const me = makeUser("100");
const ICON = "/api/v1/files/icons/20/0123456789abcdef.png";

beforeEach(() => {
  applyGatewayDispatch(
    "READY",
    makeReady(me, [
      makeGuildState("30", me, { guild: makeGuild("30", me.id, { name: "Вечерний сеанс" }) }),
      makeGuildState("20", me, { guild: makeGuild("20", "1", { name: "Книжный клуб", icon_url: ICON }) }),
      makeGuildState("10", me, { guild: makeGuild("10", "1", { name: "Дача" }) }),
    ]),
  );
});

afterEach(() => {
  resetGuilds();
  resetChannelUi();
});

const servers = () => within(screen.getByRole("navigation", { name: "Серверы" }));

describe("GuildRail", () => {
  it("lists the servers in the order of READY, each linking straight to the channel it opens on", async () => {
    await renderRouted(<GuildRail activeGuildId={null} />);
    const links = servers()
      .getAllByRole("link")
      .filter((a) => a.getAttribute("href")?.startsWith("/channels/") && a.getAttribute("href") !== "/channels/@me");
    expect(links.map((a) => a.getAttribute("aria-label"))).toEqual(["Вечерний сеанс", "Книжный клуб", "Дача"]);
    expect(links.map((a) => a.getAttribute("href"))).toEqual([
      "/channels/30/301",
      "/channels/20/201",
      "/channels/10/101",
    ]);
  });

  it("links to the channel last opened in a server, and to the server itself when it has no text channel", async () => {
    useChannelUi.getState().setLastChannel("30", "302");
    applyGatewayDispatch("GUILD_SYNC", {
      ...makeGuildState("30", me, { guild: makeGuild("30", me.id, { name: "Вечерний сеанс" }) }),
      channels: [makeChannel("301", "30"), makeChannel("302", "30", { position: 1 })],
    });
    applyGatewayDispatch("GUILD_SYNC", {
      ...makeGuildState("10", me, { guild: makeGuild("10", "1", { name: "Дача" }) }),
      channels: [makeChannel("103", "10", { type: 2 })],
    });
    await renderRouted(<GuildRail activeGuildId={null} />);
    expect(servers().getByRole("link", { name: "Вечерний сеанс" }).getAttribute("href")).toBe("/channels/30/302");
    expect(servers().getByRole("link", { name: "Дача" }).getAttribute("href")).toBe("/channels/10");
  });

  it("marks a server with people in voice, as Discord's speaker", async () => {
    applyGatewayDispatch("VOICE_STATE_UPDATE", makeVoiceState("20", "203", "1"));
    applyGatewayDispatch("VOICE_STATE_UPDATE", makeVoiceState("20", "203", "2"));
    await renderRouted(<GuildRail activeGuildId={null} />);
    const club = servers().getByRole("link", { name: "Книжный клуб, в голосе: 2" });
    expect(club.querySelector("[data-voice]")).not.toBeNull();
    expect(servers().getByRole("link", { name: "Дача" }).querySelector("[data-voice]")).toBeNull();
  });

  it("shows the server icon, or the initials of its name", async () => {
    await renderRouted(<GuildRail activeGuildId={null} />);
    const club = servers().getByRole("link", { name: "Книжный клуб" });
    expect(club.querySelector("img")?.getAttribute("src")).toBe(ICON);
    const evening = servers().getByRole("link", { name: "Вечерний сеанс" });
    expect(evening.querySelector("img")).toBeNull();
    expect(evening.textContent).toBe("ВС");
  });

  it("marks the open server and nothing else, with no owner badge", async () => {
    await renderRouted(<GuildRail activeGuildId="20" />);
    expect(servers().getByRole("link", { name: "Книжный клуб" }).getAttribute("aria-current")).toBe("page");
    expect(servers().getByRole("link", { name: "Дача" }).getAttribute("aria-current")).toBeNull();
    expect(servers().getByRole("link", { name: "Главная" }).getAttribute("aria-current")).toBeNull();
    expect(screen.queryByText(/владел/i)).toBeNull();
  });

  it("marks home on /channels/@me", async () => {
    await renderRouted(<GuildRail activeGuildId={null} />);
    expect(servers().getByRole("link", { name: "Главная" }).getAttribute("aria-current")).toBe("page");
    // Home is the Zazzzor mark, not a house: its three letters, decorative inside the named link.
    const mark = servers().getByRole("link", { name: "Главная" }).querySelector("svg[aria-hidden]");
    expect(mark?.querySelectorAll(".logo-face")).toHaveLength(3);
  });

  it("opens the dialogs to create and to join a server", async () => {
    const { user } = await renderRouted(<GuildRail activeGuildId={null} />);
    await user.click(servers().getByRole("button", { name: "Создать сервер" }));
    expect(await screen.findByRole("dialog", { name: "Создать сервер" })).toBeTruthy();
    await user.keyboard("{Escape}");
    await user.click(servers().getByRole("button", { name: "Присоединиться к серверу" }));
    expect(await screen.findByRole("dialog", { name: "Присоединиться к серверу" })).toBeTruthy();
  });
});
