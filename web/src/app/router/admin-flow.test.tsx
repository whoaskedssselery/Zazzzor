import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { resetGuilds } from "@/entities/guild";
import { NO_SUCH_USER, PASSWORD_ONCE } from "@/features/admin-users";
import { apiHandler, makeReady, problem, readyGateway, server } from "@/shared/test";

const petya = {
  id: "77",
  username: "petya",
  display_name: "Петя",
  disabled: false,
  created_at: "2026-10-24T18:04:00.000Z",
};

const signedIn = (admin: boolean, registrationOpen = true) => {
  server.use(
    sessionHandler(testUser),
    readyGateway(makeReady(testUser, [])),
    apiHandler("get", "/admin/settings", () =>
      admin ? HttpResponse.json({ registration_open: registrationOpen }) : problem(403, "admin_only"),
    ),
  );
};

/** Finds petya through the search form. */
const findPetya = async (user: ReturnType<typeof renderApp>["user"]) => {
  await user.type(await screen.findByRole("textbox", { name: "Логин" }), "Petya");
  await user.click(screen.getByRole("button", { name: "Найти" }));
  return screen.findByRole("article", { name: "petya" });
};

afterEach(() => {
  resetGuilds();
});

describe("administration", () => {
  it("shows «Администрирование» in the user settings to an administrator only", async () => {
    signedIn(true);
    const { user, router } = renderApp("/settings/profile");
    await user.click(await screen.findByRole("link", { name: "Администрирование" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/admin/instance");
    });
    expect(await screen.findByRole("switch", { name: "Регистрация открыта" })).toBeTruthy();
  });

  it("hides it from everyone else, and sends them home from /admin", async () => {
    signedIn(false);
    const { router } = renderApp("/settings/profile");
    await screen.findByRole("heading", { name: "Профиль" });
    expect(screen.queryByRole("link", { name: "Администрирование" })).toBeNull();
    await router.navigate({ to: "/admin/$", params: { _splat: "users" } });
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me");
    });
  });

  it("closes registration with the switch", async () => {
    const bodies: unknown[] = [];
    signedIn(true);
    server.use(
      apiHandler("patch", "/admin/settings", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ registration_open: false });
      }),
    );
    const { user } = renderApp("/admin/instance");
    const toggle = await screen.findByRole("switch", { name: "Регистрация открыта" });
    expect(toggle).toHaveProperty("ariaChecked", "true");
    await user.click(toggle);
    await waitFor(() => {
      expect(toggle).toHaveProperty("ariaChecked", "false");
    });
    expect(bodies).toEqual([{ registration_open: false }]);
    expect(await screen.findByText("Регистрация закрыта")).toBeTruthy();
  });

  it("finds a user by login, lower-cased", async () => {
    const asked: (string | null)[] = [];
    signedIn(true);
    server.use(
      apiHandler("get", "/admin/users", ({ request }) => {
        asked.push(new URL(request.url).searchParams.get("username"));
        return HttpResponse.json(petya);
      }),
    );
    const { user } = renderApp("/admin/users");
    const card = await findPetya(user);
    expect(within(card).getByText("Петя")).toBeTruthy();
    expect(within(card).getByText("С нами с 24 октября 2026 г.")).toBeTruthy();
    expect(asked).toEqual(["petya"]);
  });

  it("says when no account has the login", async () => {
    signedIn(true);
    server.use(apiHandler("get", "/admin/users", () => problem(404, "not_found")));
    const { user } = renderApp("/admin/users");
    await user.type(await screen.findByRole("textbox", { name: "Логин" }), "nobody");
    await user.click(screen.getByRole("button", { name: "Найти" }));
    expect(await screen.findByText(NO_SUCH_USER)).toBeTruthy();
  });

  it("resets the password after asking and shows the new one once", async () => {
    let resets = 0;
    signedIn(true);
    server.use(
      apiHandler("get", "/admin/users", () => HttpResponse.json(petya)),
      apiHandler("post", "/admin/users/{user_id}/password-reset", () => {
        resets += 1;
        return HttpResponse.json({ temporary_password: "Aaaaaaaa11111111" });
      }),
    );
    const { user } = renderApp("/admin/users");
    const card = await findPetya(user);
    await user.click(within(card).getByRole("button", { name: "Сбросить пароль" }));
    const ask = await screen.findByRole("alertdialog", { name: "Сбросить пароль petya?" });
    expect(resets).toBe(0);
    await user.click(within(ask).getByRole("button", { name: "Сбросить пароль" }));
    const shown = await screen.findByRole("dialog", { name: "Временный пароль petya" });
    expect(within(shown).getByText(PASSWORD_ONCE)).toBeTruthy();
    expect(within(shown).getByRole("textbox", { name: "Пароль" })).toHaveProperty("value", "Aaaaaaaa11111111");
    await user.click(within(shown).getByRole("button", { name: "Копировать" }));
    expect(await navigator.clipboard.readText()).toBe("Aaaaaaaa11111111");
    await user.click(within(shown).getByRole("button", { name: "Готово" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(screen.queryByDisplayValue("Aaaaaaaa11111111")).toBeNull();
    expect(resets).toBe(1);
  });

  it("blocks and unblocks, asking each time", async () => {
    const calls: string[] = [];
    signedIn(true);
    server.use(
      apiHandler("get", "/admin/users", () => HttpResponse.json(petya)),
      apiHandler("post", "/admin/users/{user_id}/disable", () => {
        calls.push("disable");
        return new HttpResponse(null, { status: 204 });
      }),
      apiHandler("post", "/admin/users/{user_id}/enable", () => {
        calls.push("enable");
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user } = renderApp("/admin/users");
    const card = await findPetya(user);
    await user.click(within(card).getByRole("button", { name: "Заблокировать" }));
    await user.click(
      within(await screen.findByRole("alertdialog", { name: "Заблокировать petya?" })).getByRole("button", {
        name: "Заблокировать",
      }),
    );
    expect(await within(card).findByText("Заблокирован")).toBeTruthy();
    await user.click(within(card).getByRole("button", { name: "Разблокировать" }));
    expect(calls).toEqual(["disable"]);
    await user.click(
      within(await screen.findByRole("alertdialog", { name: "Разблокировать petya?" })).getByRole("button", {
        name: "Разблокировать",
      }),
    );
    await waitFor(() => {
      expect(within(card).queryByText("Заблокирован")).toBeNull();
    });
    expect(calls).toEqual(["disable", "enable"]);
  });

  it("says why a block failed", async () => {
    signedIn(true);
    server.use(
      apiHandler("get", "/admin/users", () => HttpResponse.json({ ...petya, id: testUser.id })),
      apiHandler("post", "/admin/users/{user_id}/disable", () => problem(400, "cannot_disable_self")),
    );
    const { user } = renderApp("/admin/users");
    const card = await findPetya(user);
    await user.click(within(card).getByRole("button", { name: "Заблокировать" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Заблокировать",
      }),
    );
    expect(await screen.findByText("Нельзя отключить собственный аккаунт.")).toBeTruthy();
    expect(within(card).queryByText("Заблокирован")).toBeNull();
  });
});
