import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { resetChannelUi } from "@/entities/channel";
import { DEFAULT_EVERYONE, formatMask, PERM, resetGuilds } from "@/entities/guild";
import {
  apiHandler,
  makeChannel,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeMember,
  makeReady,
  makeRole,
  makeUser,
  readyGateway,
  server,
} from "@/shared/test";
import { NOT_SYNCED, SYNCED } from "@/widgets/channel-settings";

const other = makeUser("1");
const G = "10";
const category = makeChannel("30", G, { name: "Текстовые каналы", type: 4 });
const mod = makeRole("51", { name: "Модератор", position: 1 });
// The user's own role, above Модератор, so its overwrites are theirs to change.
const admin = makeRole("52", { name: "Админ", position: 2 });

/** Server 10 with channel 101 in category 30; @everyone holds `everyone`. */
const signedIn = (everyone: bigint, channel = makeChannel("101", G, { name: "общий", parent_id: "30" })) => {
  const me = makeMember(testUser, { roles: [admin.id] });
  server.use(
    sessionHandler(testUser),
    readyGateway(
      makeReady(testUser, [
        makeGuildState(G, testUser, {
          guild: makeGuild(G, other.id),
          roles: [makeEveryone(G, formatMask(everyone)), mod, admin],
          channels: [category, channel],
          members: [me, makeMember(other)],
          my_member: me,
        }),
      ]),
    ),
  );
};

const manager = DEFAULT_EVERYONE | PERM.MANAGE_CHANNELS | PERM.MANAGE_ROLES;

afterEach(() => {
  resetChannelUi();
  resetGuilds();
});

describe("channel settings", () => {
  it("renames a channel and sets its topic", async () => {
    const bodies: unknown[] = [];
    signedIn(manager);
    server.use(
      apiHandler("patch", "/channels/{channel_id}", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(makeChannel("101", G));
      }),
    );
    const { user } = renderApp(`/guilds/${G}/channels/101/settings/overview`);
    const name = await screen.findByRole("textbox", { name: "Название канала" });
    await user.clear(name);
    await user.type(name, "болталка");
    await user.type(screen.getByRole("textbox", { name: "Тема канала" }), "  обо всём  ");
    expect(screen.getByRole("region", { name: "Есть несохранённые изменения" })).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(bodies).toEqual([{ name: "болталка", topic: "обо всём" }]);
    });
  });

  it("deletes a channel after asking and goes back to the server", async () => {
    let deleted = false;
    signedIn(manager);
    server.use(
      apiHandler("delete", "/channels/{channel_id}", () => {
        deleted = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user, router } = renderApp(`/guilds/${G}/channels/101/settings/overview`);
    await user.click(await screen.findByRole("button", { name: "Удалить канал" }));
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Удалить канал" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toMatch(/^\/channels\/10/);
    });
    expect(deleted).toBe(true);
  });

  it("allows a permission for a role and saves the overwrite", async () => {
    const puts: unknown[] = [];
    signedIn(manager | PERM.SEND_MESSAGES);
    server.use(
      apiHandler("put", "/channels/{channel_id}/permissions/{target_id}", async ({ request, params }) => {
        puts.push({ target: params.target_id, body: await request.json() });
        return HttpResponse.json(makeChannel("101", G));
      }),
    );
    const { user } = renderApp(`/guilds/${G}/channels/101/settings/permissions`);
    await user.click(await screen.findByRole("button", { name: /Добавить роль или участника/ }));
    await user.click(await screen.findByRole("menuitem", { name: "Модератор" }));
    const send = screen.getByRole("group", { name: "Отправлять сообщения" });
    await user.click(within(send).getByRole("radio", { name: "Запретить" }));
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(puts).toEqual([{ target: mod.id, body: { type: 0, allow: "0", deny: formatMask(PERM.SEND_MESSAGES) } }]);
    });
  });

  it("will not change a permission the user lacks", async () => {
    signedIn(manager);
    renderApp(`/guilds/${G}/channels/101/settings/permissions`);
    // MOVE_MEMBERS is not in the default @everyone permissions the user holds.
    const move = await screen.findByRole("group", { name: "Перемещать участников" });
    expect(move).toHaveProperty("disabled", true);
    expect(screen.getByRole("group", { name: "Отправлять сообщения" })).toHaveProperty("disabled", false);
  });

  it("says whether the channel follows its category, and syncs it", async () => {
    const bodies: unknown[] = [];
    signedIn(manager, makeChannel("101", G, { name: "общий", parent_id: "30", permission_synced: false }));
    server.use(
      apiHandler("patch", "/channels/{channel_id}", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(makeChannel("101", G));
      }),
    );
    const { user } = renderApp(`/guilds/${G}/channels/101/settings/permissions`);
    expect(await screen.findByText(`${NOT_SYNCED} «Текстовые каналы»`)).toBeTruthy();
    expect(screen.queryByText(SYNCED)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Синхронизировать" }));
    await waitFor(() => {
      expect(bodies).toEqual([{ sync_permissions: true }]);
    });
  });

  it("leaves for the server without MANAGE_CHANNELS or MANAGE_ROLES there", async () => {
    signedIn(DEFAULT_EVERYONE);
    const { router } = renderApp(`/guilds/${G}/channels/101/settings/overview`);
    await waitFor(() => {
      expect(router.state.location.pathname).toMatch(/^\/channels\/10/);
    });
  });
});
