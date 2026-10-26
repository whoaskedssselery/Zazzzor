import { screen, within } from "@testing-library/react";
import { HttpResponse } from "msw";
import type { components } from "../../api/schema.gen";
import { currentHref, renderApp, sessionHandler, testUser, type User } from "../../test/app";
import { apiHandler, problem, server } from "../../test/msw";

type RegisterBody = components["schemas"]["RegisterRequest"];
type Problem = components["schemas"]["Problem"];

/** POST /auth/register that records the bodies it got and answers with `answer`. */
function registerHandler(answer: () => HttpResponse<User | Problem>) {
  const bodies: RegisterBody[] = [];
  const handler = apiHandler("post", "/auth/register", async ({ request }) => {
    bodies.push(await request.json());
    return answer();
  });
  return { handler, bodies };
}

const created = () => HttpResponse.json<User>(testUser, { status: 201 });

/** The text an element's aria-describedby points at. */
function description(el: HTMLElement): string {
  return (el.getAttribute("aria-describedby") ?? "")
    .split(" ")
    .filter(Boolean)
    .map((id) => document.getElementById(id)?.textContent ?? "")
    .join(" | ");
}

async function fill(
  user: ReturnType<typeof renderApp>["user"],
  values: { displayName?: string; username: string; password: string },
) {
  const name = await screen.findByRole("textbox", { name: "Отображаемое имя" });
  if (values.displayName) await user.type(name, values.displayName);
  await user.type(screen.getByRole("textbox", { name: "Логин" }), values.username);
  await user.type(screen.getByLabelText("Пароль"), values.password);
  await user.click(screen.getByRole("button", { name: "Зарегистрироваться" }));
}

describe("RegisterPage", () => {
  beforeEach(() => {
    server.use(sessionHandler(null));
  });

  it("creates the account, sends the trimmed display name and opens /channels/@me", async () => {
    const register = registerHandler(created);
    server.use(register.handler);
    const { user, router } = renderApp("/register");

    await fill(user, { displayName: "  Вася  ", username: "Vasya", password: "password1" });

    expect(await screen.findByText(/Вы вошли как/)).toBeTruthy();
    expect(currentHref(router)).toBe("/channels/@me");
    expect(register.bodies).toEqual([{ username: "vasya", password: "password1", display_name: "Вася" }]);
  });

  it("leaves display_name out when it is empty", async () => {
    const register = registerHandler(created);
    server.use(register.handler);
    const { user } = renderApp("/register");

    await fill(user, { username: "vasya", password: "password1" });

    await screen.findByText(/Вы вошли как/);
    expect(register.bodies).toEqual([{ username: "vasya", password: "password1" }]);
  });

  it("explains registration_closed and links to sign-in", async () => {
    server.use(registerHandler(() => problem(403, "registration_closed")).handler);
    const { user, router } = renderApp("/register?next=%2Finvite%2Fabc");

    await fill(user, { username: "vasya", password: "password1" });

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Регистрация на этом сервере закрыта.");
    const link = within(alert).getByRole("link", { name: "Войти" });
    await user.click(link);
    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    expect(router.state.location.search).toEqual({ next: "/invite/abc" });
  });

  it("shows the wait from Retry-After on 429", async () => {
    server.use(registerHandler(() => problem(429, "rate_limited", undefined, { "Retry-After": "3600" })).handler);
    const { user } = renderApp("/register");

    await fill(user, { username: "vasya", password: "password1" });

    expect((await screen.findByRole("alert")).textContent).toBe("Слишком много попыток, подождите 60 мин.");
  });

  it("states the login rule, length included, in the hint", async () => {
    renderApp("/register");
    expect(description(await screen.findByRole("textbox", { name: "Логин" }))).toBe(
      "От 2 до 32 символов: латиница, цифры, «_» и «.».",
    );
  });

  it("lower-cases the login as it is typed", async () => {
    const { user } = renderApp("/register");

    await user.type(await screen.findByRole("textbox", { name: "Логин" }), "Ivan.Petrov");

    expect(screen.getByRole<HTMLInputElement>("textbox", { name: "Логин" }).value).toBe("ivan.petrov");
  });

  it("shows the form errors for «a» and a 7-character password before any request", async () => {
    const register = registerHandler(created);
    server.use(register.handler);
    const { user } = renderApp("/register");

    await fill(user, { username: "a", password: "1234567" });

    const login = screen.getByRole("textbox", { name: "Логин" });
    const password = screen.getByLabelText("Пароль");
    expect(description(login)).toBe("Логин: 2–32 символа, латиница, цифры, «_» и «.».");
    expect(description(password)).toBe("Пароль: от 8 до 128 символов.");
    expect(login.getAttribute("aria-invalid")).toBe("true");
    expect(password.getAttribute("aria-invalid")).toBe("true");
    expect(register.bodies).toEqual([]);
  });

  it("counts the password in characters, not UTF-16 units", async () => {
    const register = registerHandler(created);
    server.use(register.handler);
    const { user } = renderApp("/register");

    // 7 emoji are 14 UTF-16 units but 7 characters for the server.
    await fill(user, { username: "vasya", password: String.fromCodePoint(0x1f600).repeat(7) });

    expect(description(screen.getByLabelText("Пароль"))).toBe("Пароль: от 8 до 128 символов.");
    expect(register.bodies).toEqual([]);
  });

  it("puts the server's errors[].field under the matching field", async () => {
    server.use(
      registerHandler(() =>
        problem(400, "validation_failed", [
          { field: "display_name", message: "must not contain bidirectional control characters" },
        ]),
      ).handler,
    );
    const { user } = renderApp("/register");

    await fill(user, { displayName: `Вася${String.fromCodePoint(0x202e)}`, username: "vasya", password: "password1" });

    const name = await screen.findByRole("textbox", { name: "Отображаемое имя" });
    await vi.waitFor(() => {
      expect(name.getAttribute("aria-invalid")).toBe("true");
    });
    expect(description(name)).toBe("Имя: от 1 до 32 символов, без управляющих и невидимых символов.");
    expect(document.activeElement).toBe(name);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("puts username_taken under the login field", async () => {
    server.use(registerHandler(() => problem(409, "username_taken")).handler);
    const { user } = renderApp("/register");

    await fill(user, { username: "vasya", password: "password1" });

    const login = screen.getByRole("textbox", { name: "Логин" });
    await vi.waitFor(() => {
      expect(description(login)).toBe("Этот логин уже занят.");
    });
    expect(login.getAttribute("aria-invalid")).toBe("true");
    expect(document.activeElement).toBe(login);
  });
});
