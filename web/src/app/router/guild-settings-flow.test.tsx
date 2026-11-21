import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { resetChannelUi } from "@/entities/channel";
import { DEFAULT_EVERYONE, formatMask, PERM, resetGuilds } from "@/entities/guild";
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
  problem,
  readyGateway,
  server,
} from "@/shared/test";

type Client = WebSocketHandlerConnection["client"];

const other = makeUser("1");
const G = "10";
const guild = makeGuild(G, other.id, { name: "Вечерний сеанс" });

/** Server 10 as testUser sees it, @everyone with `everyone`, owned by `ownerId`. */
const state = (everyone: bigint, ownerId = other.id) => {
  const me = makeMember(testUser);
  return makeGuildState(G, testUser, {
    guild: { ...guild, owner_id: ownerId },
    roles: [makeEveryone(G, formatMask(everyone))],
    channels: [makeChannel("101", G, { name: "общий" })],
    members: [me, makeMember(other)],
    my_member: me,
  });
};

const signedIn = (everyone: bigint, ownerId?: string) => {
  let client: Client | undefined;
  let seq = 1;
  server.use(
    sessionHandler(testUser),
    readyGateway(makeReady(testUser, [state(everyone, ownerId)]), (c) => {
      client = c;
    }),
  );
  return (t: string, d: unknown) => {
    seq += 1;
    client?.send(dispatchFrame(t, seq, d));
  };
};

const nav = () => screen.findByRole("navigation", { name: "Настройки сервера" });

afterEach(() => {
  resetChannelUi();
  resetGuilds();
});

describe("server settings", () => {
  it("lists only the sections the user's permissions open", async () => {
    signedIn(DEFAULT_EVERYONE | PERM.MANAGE_GUILD);
    renderApp(`/guilds/${G}/settings/overview`);
    const sections = within(await nav()).getAllByRole("link");
    expect(sections.map((l) => l.textContent)).toEqual(["Обзор", "Приглашения"]);
    expect(screen.queryByRole("button", { name: "Удалить сервер" })).toBeNull();
  });

  it("moves to a section the user may open, and out when there is none", async () => {
    signedIn(DEFAULT_EVERYONE | PERM.BAN_MEMBERS);
    const { router } = renderApp(`/guilds/${G}/settings/overview`);
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/guilds/${G}/settings/members`);
    });
  });

  it("leaves for the server when the user may open nothing", async () => {
    signedIn(DEFAULT_EVERYONE);
    const { router } = renderApp(`/guilds/${G}/settings/overview`);
    await waitFor(() => {
      expect(router.state.location.pathname).toMatch(/^\/channels\/10/);
    });
  });

  it("renames the server on «Сохранить изменения» and closes with Esc", async () => {
    const bodies: unknown[] = [];
    const send = signedIn(DEFAULT_EVERYONE | PERM.MANAGE_GUILD);
    server.use(
      apiHandler("patch", "/guilds/{guild_id}", async ({ request }) => {
        const body = await request.json();
        bodies.push(body);
        const renamed = { ...guild, name: body.name ?? guild.name };
        send("GUILD_UPDATE", renamed);
        return HttpResponse.json(renamed);
      }),
    );
    const { user, router } = renderApp(`/guilds/${G}/settings/overview`);
    const name = await screen.findByRole("textbox", { name: "Название сервера" });
    await user.clear(name);
    await user.type(name, "Киноклуб");
    expect(screen.getByRole("region", { name: "Есть несохранённые изменения" })).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Есть несохранённые изменения" })).toBeNull();
    });
    expect(bodies).toEqual([{ name: "Киноклуб" }]);
    expect(within(await nav()).getByText("Киноклуб")).toBeTruthy();
    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(router.state.location.pathname).toMatch(/^\/channels\/10/);
    });
  });

  it("asks before leaving unsaved changes for another section", async () => {
    signedIn(DEFAULT_EVERYONE | PERM.MANAGE_GUILD);
    const { user, router } = renderApp(`/guilds/${G}/settings/overview`);
    await user.type(await screen.findByRole("textbox", { name: "Название сервера" }), "!");
    await user.click(within(await nav()).getByRole("link", { name: "Приглашения" }));
    const dialog = await screen.findByRole("alertdialog", { name: "Уйти без сохранения?" });
    expect(router.state.location.pathname).toBe(`/guilds/${G}/settings/overview`);
    await user.click(within(dialog).getByRole("button", { name: "Уйти" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/guilds/${G}/settings/invites`);
    });
  });

  it("keeps an icon picked while the previous one uploads", async () => {
    const sizes: number[] = [];
    const appended = vi.spyOn(FormData.prototype, "append");
    onTestFinished(() => {
      appended.mockRestore();
    });
    let finish = () => {};
    signedIn(DEFAULT_EVERYONE | PERM.MANAGE_GUILD);
    server.use(
      apiHandler("put", "/guilds/{guild_id}/icon", async () => {
        // The file as the app put it in the form: since Node 26 fetch cannot read a
        // jsdom file back out of the request body (the browser has no such problem).
        const file = appended.mock.calls.at(-1)?.[1];
        sizes.push(file instanceof Blob ? file.size : -1);
        // The first upload waits for the test; later ones answer at once.
        if (sizes.length === 1) {
          await new Promise<void>((resolve) => {
            finish = resolve;
          });
        }
        return HttpResponse.json(guild);
      }),
    );
    const { user } = renderApp(`/guilds/${G}/settings/overview`);
    await screen.findByRole("textbox", { name: "Название сервера" });
    const input = document.querySelector<HTMLInputElement>('input[type="file"]');
    if (!input) throw new Error("no file input");
    fireEvent.change(input, { target: { files: [new File([new Uint8Array(8)], "a.png", { type: "image/png" })] } });
    await user.click(await screen.findByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(sizes).toEqual([8]);
    });
    fireEvent.change(input, { target: { files: [new File([new Uint8Array(16)], "b.png", { type: "image/png" })] } });
    finish();
    // The first save is done, the second pick still waits for its own.
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Сохранить изменения" })).toHaveProperty("disabled", false);
    });
    expect(screen.getByRole("region", { name: "Есть несохранённые изменения" })).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Есть несохранённые изменения" })).toBeNull();
    });
    expect(sizes).toEqual([8, 16]);
  });

  it("refuses an icon over 4 MB before uploading it", async () => {
    signedIn(DEFAULT_EVERYONE | PERM.MANAGE_GUILD);
    renderApp(`/guilds/${G}/settings/overview`);
    await screen.findByRole("textbox", { name: "Название сервера" });
    const input = document.querySelector<HTMLInputElement>('input[type="file"]');
    if (!input) throw new Error("no file input");
    const big = new File([new Uint8Array(4 * 1024 * 1024 + 1)], "big.png", { type: "image/png" });
    fireEvent.change(input, { target: { files: [big] } });
    expect((await screen.findByRole("alert")).textContent).toBe("Файл больше 4 МБ.");
    expect(screen.queryByRole("region", { name: "Есть несохранённые изменения" })).toBeNull();
  });

  it("deletes the server for its owner once its name is typed", async () => {
    let deleted = false;
    signedIn(DEFAULT_EVERYONE, testUser.id);
    server.use(
      apiHandler("delete", "/guilds/{guild_id}", () => {
        deleted = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user, router } = renderApp(`/guilds/${G}/settings/overview`);
    await user.click(await screen.findByRole("button", { name: "Удалить сервер" }));
    const dialog = await screen.findByRole("dialog", { name: "Удалить «Вечерний сеанс»?" });
    const confirm = within(dialog).getByRole("button", { name: "Удалить сервер" });
    expect(confirm).toHaveProperty("disabled", true);
    await user.type(within(dialog).getByRole("textbox"), "Вечерний сеанс");
    await user.click(confirm);
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me");
    });
    expect(deleted).toBe(true);
  });

  it("says why a rename failed", async () => {
    signedIn(DEFAULT_EVERYONE | PERM.MANAGE_GUILD);
    server.use(apiHandler("patch", "/guilds/{guild_id}", () => problem(403, "missing_permission")));
    const { user } = renderApp(`/guilds/${G}/settings/overview`);
    await user.type(await screen.findByRole("textbox", { name: "Название сервера" }), "!");
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    expect((await screen.findByRole("alert")).textContent).toBe("Недостаточно прав для этого действия.");
  });
});
