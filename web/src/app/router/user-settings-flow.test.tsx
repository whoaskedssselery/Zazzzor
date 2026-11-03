import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { resetGuilds } from "@/entities/guild";
import { PASSWORD_CHANGED, PASSWORDS_DIFFER } from "@/features/change-password";
import { NAME_RULE } from "@/features/edit-profile";
import { WRONG_IMAGE } from "@/shared/lib";
import { apiHandler, dispatchFrame, makeReady, problem, readyGateway, server } from "@/shared/test";
import { LEAVE_UNSAVED, UNSAVED } from "@/shared/ui";

const AVATAR = "/api/v1/files/avatars/1234567890123456789/0123456789abcdef.png";

const signedIn = (user = testUser) => {
  server.use(sessionHandler(user), readyGateway(makeReady(user, [])));
};

/** The file input behind «Сменить аватар». */
const picker = () => {
  const input = document.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("no file input");
  return input;
};

const png = () => new File([new Uint8Array(8)], "me.png", { type: "image/png" });

const panelName = () => document.querySelector("[data-user-name]")?.textContent;

afterEach(() => {
  resetGuilds();
});

describe("user settings", () => {
  it("opens the first section for an unknown one", async () => {
    signedIn();
    const { router } = renderApp("/settings/nope");
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/settings/profile");
    });
    expect(await screen.findByRole("heading", { name: "Профиль" })).toBeTruthy();
  });

  it("renames the user, and the user panel follows once the settings close", async () => {
    const bodies: unknown[] = [];
    signedIn();
    server.use(
      apiHandler("patch", "/users/@me", async ({ request }) => {
        const body = (await request.json()) as { display_name: string };
        bodies.push(body);
        return HttpResponse.json({ ...testUser, display_name: body.display_name });
      }),
    );
    const { user, router } = renderApp("/channels/@me");
    await user.click(await screen.findByRole("button", { name: "Настройки пользователя" }));
    const name = await screen.findByRole("textbox", { name: "Отображаемое имя" });
    await user.clear(name);
    await user.type(name, "  Василий  ");
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: UNSAVED })).toBeNull();
    });
    expect(bodies).toEqual([{ display_name: "Василий" }]);
    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me");
    });
    expect(panelName()).toBe("Василий");
  });

  it("checks the display name before sending it", async () => {
    signedIn();
    const { user } = renderApp("/settings/profile");
    const name = await screen.findByRole("textbox", { name: "Отображаемое имя" });
    await user.clear(name);
    await user.type(name, "   ");
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    expect(await screen.findByText(NAME_RULE)).toBeTruthy();
  });

  it("uploads a new avatar after showing it", async () => {
    let uploaded = false;
    signedIn();
    server.use(
      apiHandler("put", "/users/@me/avatar", () => {
        uploaded = true;
        return HttpResponse.json({ ...testUser, avatar_url: AVATAR });
      }),
    );
    const { user } = renderApp("/settings/profile");
    await screen.findByRole("textbox", { name: "Отображаемое имя" });
    fireEvent.change(picker(), { target: { files: [png()] } });
    expect(await screen.findByRole("region", { name: UNSAVED })).toBeTruthy();
    expect(uploaded).toBe(false);
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(uploaded).toBe(true);
    });
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: UNSAVED })).toBeNull();
    });
  });

  it("refuses a file that is not an image before uploading it", async () => {
    signedIn();
    renderApp("/settings/profile");
    await screen.findByRole("textbox", { name: "Отображаемое имя" });
    fireEvent.change(picker(), { target: { files: [new File(["x"], "a.txt", { type: "text/plain" })] } });
    expect((await screen.findByRole("alert")).textContent).toBe(WRONG_IMAGE);
    expect(screen.queryByRole("region", { name: UNSAVED })).toBeNull();
  });

  it("removes the avatar on save", async () => {
    let removed = false;
    signedIn({ ...testUser, avatar_url: AVATAR });
    server.use(
      apiHandler("delete", "/users/@me/avatar", () => {
        removed = true;
        return HttpResponse.json(testUser);
      }),
    );
    const { user } = renderApp("/settings/profile");
    await user.click(await screen.findByRole("button", { name: "Удалить аватар" }));
    expect(screen.queryByRole("button", { name: "Удалить аватар" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(removed).toBe(true);
    });
  });

  it("takes a USER_UPDATE into the form while it holds nothing of its own", async () => {
    let send: ((d: unknown) => void) | undefined;
    server.use(
      sessionHandler(testUser),
      readyGateway(makeReady(testUser, []), (client) => {
        send = (d) => {
          client.send(dispatchFrame("USER_UPDATE", 2, d));
        };
      }),
    );
    renderApp("/settings/profile");
    const name = await screen.findByRole("textbox", { name: "Отображаемое имя" });
    await waitFor(() => {
      expect(send).toBeDefined();
    });
    send?.({ ...testUser, display_name: "Васёк" });
    await waitFor(() => {
      expect(name).toHaveProperty("value", "Васёк");
    });
    expect(screen.queryByRole("region", { name: UNSAVED })).toBeNull();
  });

  it("asks before closing with unsaved changes", async () => {
    signedIn();
    const { user, router } = renderApp("/settings/profile");
    await user.type(await screen.findByRole("textbox", { name: "Отображаемое имя" }), "!");
    await user.click(screen.getByRole("button", { name: "Закрыть настройки" }));
    const dialog = await screen.findByRole("alertdialog", { name: LEAVE_UNSAVED });
    expect(router.state.location.pathname).toBe("/settings/profile");
    await user.click(within(dialog).getByRole("button", { name: "Уйти" }));
    // Opened directly, so there is nothing to go back to: home.
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me");
    });
  });
});

describe("password", () => {
  const fill = async (user: ReturnType<typeof renderApp>["user"], current: string, next: string, repeat: string) => {
    await user.type(await screen.findByLabelText("Текущий пароль"), current);
    await user.type(screen.getByLabelText("Новый пароль"), next);
    await user.type(screen.getByLabelText("Новый пароль ещё раз"), repeat);
    await user.click(screen.getByRole("button", { name: "Сменить пароль" }));
  };

  it("changes the password and says the other sessions ended", async () => {
    const bodies: unknown[] = [];
    signedIn();
    server.use(
      apiHandler("post", "/users/@me/password", async ({ request }) => {
        bodies.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user } = renderApp("/settings/account");
    await fill(user, "old-secret", "new-secret-1", "new-secret-1");
    expect(await screen.findByText(PASSWORD_CHANGED)).toBeTruthy();
    expect(bodies).toEqual([{ current_password: "old-secret", new_password: "new-secret-1" }]);
    expect(screen.getByLabelText("Текущий пароль")).toHaveProperty("value", "");
  });

  it("catches new passwords that differ before sending them", async () => {
    let sent = false;
    signedIn();
    server.use(
      apiHandler("post", "/users/@me/password", () => {
        sent = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user } = renderApp("/settings/account");
    await fill(user, "old-secret", "new-secret-1", "new-secret-2");
    expect(await screen.findByText(PASSWORDS_DIFFER)).toBeTruthy();
    expect(sent).toBe(false);
  });

  it("puts a wrong current password under its field", async () => {
    signedIn();
    server.use(
      apiHandler("post", "/users/@me/password", () =>
        problem(400, "validation_failed", [{ field: "current_password", message: "wrong" }]),
      ),
    );
    const { user } = renderApp("/settings/account");
    await fill(user, "not-it", "new-secret-1", "new-secret-1");
    expect(await screen.findByText("Неверный текущий пароль.")).toBeTruthy();
    expect(document.activeElement).toBe(screen.getByLabelText("Текущий пароль"));
  });
});
