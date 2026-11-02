import { screen, waitFor } from "@testing-library/react";
import { HttpResponse } from "msw";
import { applyGatewayDispatch, resetGuilds } from "@/entities/guild";
import type { components } from "@/shared/api";
import {
  apiHandler,
  makeChannel,
  makeGuild,
  makeGuildState,
  makeReady,
  makeUser,
  problem,
  renderRouted,
  server,
} from "@/shared/test";
import { InvitePage } from "./InvitePage";

const me = makeUser("100");
const inviter = makeUser("1", { display_name: "Аня Соколова" });
const guild = makeGuild("50", inviter.id, { name: "Вечерний сеанс" });

const invite: components["schemas"]["Invite"] = {
  code: "AbCd1234",
  guild: { id: guild.id, name: guild.name, icon_url: null },
  channel: { id: "62", name: "кино" },
  inviter,
  uses: 3,
  max_uses: 0,
  expires_at: null,
};

beforeEach(() => {
  applyGatewayDispatch("READY", makeReady(me, []));
});

afterEach(() => {
  resetGuilds();
});

const open = (code = invite.code) => renderRouted(<InvitePage code={code} />, { path: `/invite/${code}` });

describe("InvitePage", () => {
  it("shows the server, the channel and who invited", async () => {
    server.use(apiHandler("get", "/invites/{code}", () => HttpResponse.json(invite)));
    await open();
    expect(await screen.findByRole("heading", { name: "Вечерний сеанс" })).toBeTruthy();
    expect(screen.getByText("#кино")).toBeTruthy();
    expect(screen.getByText("Аня Соколова")).toBeTruthy();
    expect(screen.getByText(/приглашает вас/)).toBeTruthy();
  });

  it("tells a member they are already on the server and opens the invite's channel", async () => {
    applyGatewayDispatch(
      "READY",
      makeReady(me, [
        makeGuildState(guild.id, me, {
          guild,
          channels: [makeChannel("61", guild.id), makeChannel("62", guild.id, { position: 1 })],
        }),
      ]),
    );
    let joins = 0;
    server.use(
      apiHandler("get", "/invites/{code}", () => HttpResponse.json(invite)),
      apiHandler("post", "/invites/{code}", () => {
        joins += 1;
        return HttpResponse.json(guild);
      }),
    );
    const { user, router } = await open();
    expect(await screen.findByText("Вы уже на этом сервере")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Принять приглашение" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Открыть" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/50/62");
    });
    expect(joins).toBe(0);
  });

  it("joins and opens the channel of the invite", async () => {
    server.use(
      apiHandler("get", "/invites/{code}", () => HttpResponse.json(invite)),
      apiHandler("post", "/invites/{code}", () => {
        setTimeout(() => {
          applyGatewayDispatch(
            "GUILD_CREATE",
            makeGuildState(guild.id, me, {
              guild,
              channels: [makeChannel("62", guild.id, { name: "кино" })],
            }),
          );
        }, 20);
        return HttpResponse.json(guild);
      }),
    );
    const { user, router } = await open();
    // A render that flashes «already a member» between the join and the
    // navigation is caught here, however briefly it lasts.
    let flashed = false;
    const observer = new MutationObserver(() => {
      flashed ||= document.body.textContent.includes("Вы уже на этом сервере");
    });
    observer.observe(document.body, { childList: true, subtree: true, characterData: true });
    await user.click(await screen.findByRole("button", { name: "Принять приглашение" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/50/62");
    });
    observer.disconnect();
    expect(flashed).toBe(false);
  });

  it("says «Вы заблокированы на этом сервере» to a banned user", async () => {
    server.use(
      apiHandler("get", "/invites/{code}", () => HttpResponse.json(invite)),
      apiHandler("post", "/invites/{code}", () => problem(403, "banned")),
    );
    const { user } = await open();
    await user.click(await screen.findByRole("button", { name: "Принять приглашение" }));
    expect((await screen.findByRole("alert")).textContent).toBe("Вы заблокированы на этом сервере");
  });

  it.each([
    ["expired", 410, "invite_expired"],
    ["used up", 410, "invite_exhausted"],
    ["unknown", 404, "not_found"],
  ])("says «Приглашение недействительно» for an %s invite", async (_, status, code) => {
    server.use(apiHandler("get", "/invites/{code}", () => problem(status, code)));
    await open();
    expect(await screen.findByRole("heading", { name: "Приглашение недействительно" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "На главную" }).getAttribute("href")).toBe("/channels/@me");
    expect(screen.queryByRole("button", { name: "Принять приглашение" })).toBeNull();
  });
});
