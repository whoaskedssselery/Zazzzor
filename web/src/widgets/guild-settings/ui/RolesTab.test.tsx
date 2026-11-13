import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse } from "msw";
import { applyReady, formatMask, PERM } from "@/entities/guild";
import {
  apiHandler,
  asMe,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeMember,
  makeRole,
  makeUser,
  server,
} from "@/shared/test";
import { UiProviders } from "@/shared/ui";
import { NOT_YOURS } from "./PermissionList";
import { ROLE_LOCKED } from "./RoleEditor";
import { positionAt, RolesTab } from "./RolesTab";

const owner = makeUser("1");
const me = makeUser("2");
const G = "10";
const mod = makeRole("51", { name: "Модератор", position: 1, color: 0x3498db });
const admin = makeRole("52", {
  name: "Админ",
  position: 2,
  permissions: formatMask(PERM.MANAGE_ROLES | PERM.KICK_MEMBERS | PERM.SEND_MESSAGES),
});
const boss = makeRole("53", { name: "Главный", position: 3 });

/** Server 10 owned by someone else; `me` holds Админ (position 2). */
const entry = () => {
  const mine = makeMember(me, { roles: [admin.id] });
  const state = makeGuildState(G, me, {
    guild: makeGuild(G, owner.id),
    roles: [makeEveryone(G), mod, admin, boss],
    members: [mine, makeMember(owner)],
    my_member: mine,
  });
  const e = applyReady({
    user: asMe(me),
    guilds: [state],
    presences: [],
    read_states: [],
    relationships: [],
    private_channels: [],
    user_guild_settings: [],
    private_voice_states: [],
    calls: [],
  }).guilds[G];
  if (!e) throw new Error("no entry");
  return e;
};

const renderTab = (meId = me.id) => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <UiProviders>
        <RolesTab entry={entry()} meId={meId} onDirtyChange={() => {}} />
      </UiProviders>
    </QueryClientProvider>,
  );
  return user;
};

const list = () => screen.getByRole("list", { name: "Роли сервера" });
const roleButton = (name: string) => within(list()).getByRole("button", { name: new RegExp(name) });

describe("positionAt", () => {
  it("counts positions from the lowest role up", () => {
    expect(positionAt(0, 3)).toBe(3);
    expect(positionAt(2, 3)).toBe(1);
  });
});

describe("RolesTab", () => {
  it("lists the roles from the highest down with @everyone last, locking those not below the user", () => {
    renderTab();
    const names = within(list())
      .getAllByRole("button")
      .map((b) => b.textContent);
    expect(names).toEqual(["Главный", "Админ", "Модератор", "@everyone"]);
    expect(within(roleButton("Главный")).getByLabelText("Роль не ниже вашей высшей")).toBeTruthy();
    expect(within(roleButton("Админ")).getByLabelText("Роль не ниже вашей высшей")).toBeTruthy();
    expect(within(roleButton("Модератор")).queryByLabelText("Роль не ниже вашей высшей")).toBeNull();
  });

  it("shows a role above the user read-only, saying why", async () => {
    const user = renderTab();
    await user.click(roleButton("Главный"));
    expect(screen.getByText(ROLE_LOCKED)).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "Название роли" })).toHaveProperty("disabled", true);
    expect(screen.queryByRole("button", { name: "Удалить роль" })).toBeNull();
  });

  it("creates «новая роль»", async () => {
    const bodies: unknown[] = [];
    server.use(
      apiHandler("post", "/guilds/{guild_id}/roles", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(makeRole("60", { name: "новая роль", position: 1 }), { status: 201 });
      }),
    );
    const user = renderTab();
    await user.click(screen.getByRole("button", { name: "Создать роль" }));
    await waitFor(() => {
      expect(bodies).toEqual([{ name: "новая роль" }]);
    });
  });

  it("moves a role with Alt+↑, but never up to or above the user's own", async () => {
    const moves: unknown[] = [];
    server.use(
      apiHandler("patch", "/guilds/{guild_id}/roles", async ({ request }) => {
        moves.push(await request.json());
        return HttpResponse.json([]);
      }),
    );
    const user = renderTab();
    // Модератор (position 1) up one would take position 2, Админ's: not below the user.
    roleButton("Модератор").focus();
    await user.keyboard("{Alt>}{ArrowUp}{/Alt}");
    expect(moves).toEqual([]);
    // Админ itself is not below the user, so it does not move at all.
    roleButton("Админ").focus();
    await user.keyboard("{Alt>}{ArrowDown}{/Alt}");
    expect(moves).toEqual([]);
  });

  it("moves a role for the owner, from the list index to its position", async () => {
    const moves: unknown[] = [];
    server.use(
      apiHandler("patch", "/guilds/{guild_id}/roles", async ({ request }) => {
        moves.push(await request.json());
        return HttpResponse.json([]);
      }),
    );
    const user = renderTab(owner.id);
    roleButton("Модератор").focus();
    await user.keyboard("{Alt>}{ArrowUp}{/Alt}");
    await waitFor(() => {
      expect(moves).toEqual([[{ id: mod.id, position: 2 }]]);
    });
  });

  it("saves only what changed of a role", async () => {
    const bodies: unknown[] = [];
    server.use(
      apiHandler("patch", "/guilds/{guild_id}/roles/{role_id}", async ({ request, params }) => {
        bodies.push({ id: params.role_id, body: await request.json() });
        return HttpResponse.json(mod);
      }),
    );
    const user = renderTab();
    await user.click(roleButton("Модератор"));
    const name = screen.getByRole("textbox", { name: "Название роли" });
    await user.clear(name);
    await user.type(name, "Модер");
    await user.click(screen.getByRole("checkbox", { name: "Показывать участников с этой ролью отдельно" }));
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(bodies).toEqual([{ id: mod.id, body: { name: "Модер", hoist: true } }]);
    });
  });

  it("lets the user grant only permissions they hold", async () => {
    const user = renderTab();
    await user.click(roleButton("Модератор"));
    await user.click(screen.getByRole("tab", { name: "Права" }));
    expect(screen.getByRole("switch", { name: "Выгонять участников" })).toHaveProperty("disabled", false);
    const ban = screen.getByRole("switch", { name: "Банить участников" });
    expect(ban).toHaveProperty("disabled", true);
    expect(screen.getAllByText(NOT_YOURS).length).toBeGreaterThan(0);
  });

  it("offers only the permissions of @everyone, and no deletion", async () => {
    const user = renderTab();
    await user.click(roleButton("@everyone"));
    expect(screen.queryByRole("tab", { name: "Отображение" })).toBeNull();
    expect(screen.getByRole("switch", { name: "Отправлять сообщения" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Удалить роль" })).toBeNull();
  });

  it("deletes a role after asking", async () => {
    let deleted: string | undefined;
    server.use(
      apiHandler("delete", "/guilds/{guild_id}/roles/{role_id}", ({ params }) => {
        deleted = params.role_id;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = renderTab();
    await user.click(roleButton("Модератор"));
    await user.click(screen.getByRole("button", { name: "Удалить роль" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Удалить роль" }));
    await waitFor(() => {
      expect(deleted).toBe(mod.id);
    });
  });
});
