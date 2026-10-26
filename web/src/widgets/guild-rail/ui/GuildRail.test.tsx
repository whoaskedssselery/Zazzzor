import { screen, within } from "@testing-library/react";
import { applyGatewayDispatch, resetGuilds } from "@/entities/guild";
import { makeGuild, makeGuildState, makeReady, makeUser, renderRouted } from "@/shared/test";
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
});

const servers = () => within(screen.getByRole("navigation", { name: "Серверы" }));

describe("GuildRail", () => {
  it("lists the servers in the order of READY, each linking to the server", async () => {
    await renderRouted(<GuildRail activeGuildId={null} />);
    const links = servers()
      .getAllByRole("link")
      .filter((a) => a.getAttribute("href")?.startsWith("/channels/") && a.getAttribute("href") !== "/channels/@me");
    expect(links.map((a) => a.getAttribute("aria-label"))).toEqual(["Вечерний сеанс", "Книжный клуб", "Дача"]);
    expect(links.map((a) => a.getAttribute("href"))).toEqual(["/channels/30", "/channels/20", "/channels/10"]);
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
