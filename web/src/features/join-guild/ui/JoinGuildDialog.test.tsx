import { screen, waitFor } from "@testing-library/react";
import { HttpResponse } from "msw";
import { applyGatewayDispatch, DEFAULT_EVERYONE, formatMask, resetGuilds } from "@/entities/guild";
import type { components } from "@/shared/api";
import {
  apiHandler,
  makeChannel,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeReady,
  makeUser,
  problem,
  renderRouted,
  server,
} from "@/shared/test";
import { JoinGuildDialog } from "./JoinGuildDialog";

const me = makeUser("100");
const inviter = makeUser("1", { display_name: "Аня" });
const guild = makeGuild("50", inviter.id, { name: "Дача" });

const invite: components["schemas"]["Invite"] = {
  code: "AbCd1234",
  guild: { id: guild.id, name: guild.name, icon_url: null },
  channel: { id: "62", name: "огород" },
  inviter,
  uses: 0,
  max_uses: 0,
  expires_at: null,
};

beforeEach(() => {
  applyGatewayDispatch("READY", makeReady(me, []));
});

afterEach(() => {
  resetGuilds();
});

const open = () => renderRouted(<JoinGuildDialog open onOpenChange={() => {}} />, { path: "/channels/@me" });

/** The server answers the join and announces the server over the gateway a moment later. */
const joinable = (joined: string[]) => {
  server.use(
    apiHandler("get", "/invites/{code}", ({ params }) =>
      params.code === invite.code ? HttpResponse.json(invite) : problem(404, "not_found"),
    ),
    apiHandler("post", "/invites/{code}", ({ params }) => {
      joined.push(params.code);
      setTimeout(() => {
        applyGatewayDispatch(
          "GUILD_CREATE",
          makeGuildState(guild.id, me, {
            guild,
            roles: [makeEveryone(guild.id, formatMask(DEFAULT_EVERYONE))],
            channels: [makeChannel("61", guild.id, { name: "общий" }), makeChannel("62", guild.id, { name: "огород" })],
          }),
        );
      }, 20);
      return HttpResponse.json(guild);
    }),
  );
};

describe("JoinGuildDialog", () => {
  it.each([
    ["a bare code", "AbCd1234"],
    ["a link", "https://zz.example.org/invite/AbCd1234"],
  ])("joins by %s and opens the channel of the invite", async (_, input) => {
    const joined: string[] = [];
    joinable(joined);
    const { user, router } = await open();
    await user.type(await screen.findByLabelText("Ссылка-приглашение или код"), input);
    await user.click(screen.getByRole("button", { name: "Присоединиться" }));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/50/62");
    });
    expect(joined).toEqual(["AbCd1234"]);
  });

  it("says what a valid input looks like, without asking the server", async () => {
    const joined: string[] = [];
    joinable(joined);
    const { user } = await open();
    await user.type(await screen.findByLabelText("Ссылка-приглашение или код"), "https://zz.example.org/AbCd1234");
    await user.click(screen.getByRole("button", { name: "Присоединиться" }));
    expect(
      await screen.findByText("Вставьте ссылку вида https://сайт/invite/AbCd1234 или код из 8 букв и цифр."),
    ).toBeTruthy();
    expect(joined).toEqual([]);
  });

  it("says the invite is no longer valid", async () => {
    server.use(apiHandler("get", "/invites/{code}", () => problem(410, "invite_expired")));
    const { user } = await open();
    await user.type(await screen.findByLabelText("Ссылка-приглашение или код"), "AbCd1234");
    await user.click(screen.getByRole("button", { name: "Присоединиться" }));
    expect((await screen.findByRole("alert")).textContent).toBe("Приглашение недействительно");
  });
});
