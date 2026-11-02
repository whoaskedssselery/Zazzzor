import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse } from "msw";
import { applyGatewayDispatch, formatMask, getGuildsState, PERM, resetGuilds } from "@/entities/guild";
import {
  apiHandler,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeMember,
  makeReady,
  makeRole,
  makeUser,
  problem,
  server,
} from "@/shared/test";
import { UiProviders } from "@/shared/ui";
import { MembersTab } from "./MembersTab";

const owner = makeUser("1", { display_name: "Хозяин" });
const me = makeUser("2", { display_name: "Я" });
const anna = makeUser("3", { display_name: "Аня", username: "anna" });
const petr = makeUser("4", { display_name: "Пётр", username: "petya" });
const G = "10";
const low = makeRole("51", { name: "Новичок", position: 1 });
const mod = makeRole("52", {
  name: "Модератор",
  position: 2,
  permissions: formatMask(PERM.KICK_MEMBERS | PERM.BAN_MEMBERS | PERM.MANAGE_NICKNAMES | PERM.MANAGE_ROLES),
});

beforeEach(() => {
  const mine = makeMember(me, { roles: [mod.id] });
  applyGatewayDispatch(
    "READY",
    makeReady(me, [
      makeGuildState(G, me, {
        guild: makeGuild(G, owner.id),
        roles: [makeEveryone(G, "0"), low, mod],
        members: [
          mine,
          makeMember(owner),
          makeMember(anna, { roles: [low.id], nick: "Анечка" }),
          makeMember(petr, { roles: [low.id] }),
        ],
        my_member: mine,
      }),
    ]),
  );
});

afterEach(() => {
  resetGuilds();
});

const renderTab = () => {
  const user = userEvent.setup();
  const entry = getGuildsState().guilds[G];
  if (!entry) throw new Error("no entry");
  render(
    <QueryClientProvider client={new QueryClient()}>
      <UiProviders>
        <MembersTab entry={entry} />
      </UiProviders>
    </QueryClientProvider>,
  );
  return user;
};

const openMenu = async (user: ReturnType<typeof userEvent.setup>, name: string) => {
  await user.click(screen.getByRole("button", { name: `Действия с ${name}` }));
  return screen.findByRole("menu");
};

describe("MembersTab", () => {
  it("finds members by nickname, display name or login", async () => {
    const user = renderTab();
    const names = () =>
      within(screen.getByRole("list", { name: "Участники сервера" }))
        .getAllByRole("listitem")
        .filter((li) => li.querySelector("bdi"))
        .map((li) => li.querySelector("bdi")?.textContent);
    expect(names()).toEqual(["Анечка", "Пётр", "Хозяин", "Я"]);
    await user.type(screen.getByRole("searchbox", { name: "Поиск участников" }), "PETYA");
    expect(names()).toEqual(["Пётр"]);
    await user.clear(screen.getByRole("searchbox", { name: "Поиск участников" }));
    await user.type(screen.getByRole("searchbox", { name: "Поиск участников" }), "аня");
    expect(names()).toEqual(["Анечка"]);
  });

  it("offers only roles on the owner (the server checks the roles, not the target's rank)", async () => {
    const user = renderTab();
    const menu = await openMenu(user, "Хозяин");
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((i) => i.textContent),
    ).toEqual(["Роли"]);
  });

  it("kicks after asking", async () => {
    let kicked: string | undefined;
    server.use(
      apiHandler("delete", "/guilds/{guild_id}/members/{user_id}", ({ params }) => {
        kicked = params.user_id;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = renderTab();
    await user.click(within(await openMenu(user, "Пётр")).getByRole("menuitem", { name: "Выгнать" }));
    const dialog = await screen.findByRole("alertdialog", { name: "Выгнать Пётр?" });
    await user.click(within(dialog).getByRole("button", { name: "Выгнать" }));
    await waitFor(() => {
      expect(kicked).toBe(petr.id);
    });
  });

  it("bans with a reason", async () => {
    const bodies: unknown[] = [];
    server.use(
      apiHandler("put", "/guilds/{guild_id}/bans/{user_id}", async ({ request }) => {
        bodies.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = renderTab();
    await user.click(within(await openMenu(user, "Анечка")).getByRole("menuitem", { name: "Забанить" }));
    const dialog = await screen.findByRole("dialog", { name: "Забанить Анечка?" });
    await user.type(within(dialog).getByRole("textbox", { name: "Причина" }), "  спам  ");
    await user.click(within(dialog).getByRole("button", { name: "Забанить" }));
    await waitFor(() => {
      expect(bodies).toEqual([{ reason: "спам" }]);
    });
  });

  it("changes a nickname and clears it when emptied", async () => {
    const bodies: unknown[] = [];
    server.use(
      apiHandler("patch", "/guilds/{guild_id}/members/{user_id}", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(makeMember(anna));
      }),
    );
    const user = renderTab();
    await user.click(within(await openMenu(user, "Анечка")).getByRole("menuitem", { name: "Изменить ник" }));
    const dialog = await screen.findByRole("dialog", { name: "Изменить ник" });
    await user.clear(within(dialog).getByRole("textbox", { name: "Ник" }));
    await user.click(within(dialog).getByRole("button", { name: "Сохранить" }));
    await waitFor(() => {
      expect(bodies).toEqual([{ nick: null }]);
    });
  });

  it("gives and takes only the roles below the user", async () => {
    const bodies: unknown[] = [];
    server.use(
      apiHandler("patch", "/guilds/{guild_id}/members/{user_id}", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json(makeMember(petr));
      }),
    );
    const user = renderTab();
    await user.click(within(await openMenu(user, "Пётр")).getByRole("menuitem", { name: "Роли" }));
    const dialog = await screen.findByRole("dialog", { name: "Роли — Пётр" });
    expect(within(dialog).getAllByRole("checkbox")).toHaveLength(1);
    await user.click(within(dialog).getByRole("checkbox", { name: "Новичок" }));
    await user.click(within(dialog).getByRole("button", { name: "Сохранить" }));
    await waitFor(() => {
      expect(bodies).toEqual([{ roles: [] }]);
    });
  });

  it("says when the server refuses for rank", async () => {
    server.use(apiHandler("delete", "/guilds/{guild_id}/members/{user_id}", () => problem(403, "role_hierarchy")));
    const user = renderTab();
    await user.click(within(await openMenu(user, "Пётр")).getByRole("menuitem", { name: "Выгнать" }));
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Выгнать" }));
    expect(await screen.findByText("Нельзя: ваша роль не выше роли участника.")).toBeTruthy();
  });
});
