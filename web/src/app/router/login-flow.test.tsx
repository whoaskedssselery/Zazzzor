import { screen } from "@testing-library/react";
import { HttpResponse } from "msw";
import { currentHref, renderApp, sessionHandler, testUser, type User } from "@/app/test";
import type { components } from "@/shared/api";
import { apiHandler, problem, server } from "@/shared/test";

type LoginBody = components["schemas"]["LoginRequest"];
type Problem = components["schemas"]["Problem"];

/** POST /auth/login that records the bodies it got and answers with `answer`. */
const loginHandler = (answer: () => HttpResponse<User | Problem>) => {
  const bodies: LoginBody[] = [];
  const handler = apiHandler("post", "/auth/login", async ({ request }) => {
    const body = await request.json();
    bodies.push(body);
    return answer();
  });
  return { handler, bodies };
};

const signIn = async (user: ReturnType<typeof renderApp>["user"], username: string, password: string) => {
  await user.type(await screen.findByRole("textbox", { name: "Логин" }), username);
  await user.type(screen.getByLabelText("Пароль"), password);
  await user.click(screen.getByRole("button", { name: "Войти" }));
};

describe("LoginPage", () => {
  beforeEach(() => {
    server.use(sessionHandler(null));
  });

  it("signs in with the right password and opens /channels/@me", async () => {
    const login = loginHandler(() => HttpResponse.json(testUser));
    server.use(login.handler);
    const { user, router } = renderApp("/login");

    await signIn(user, "vasya", "password1");

    expect(await screen.findByRole("heading", { level: 1, name: "Друзья" })).toBeTruthy();
    expect(currentHref(router)).toBe("/channels/@me");
    expect(login.bodies).toEqual([{ username: "vasya", password: "password1" }]);
  });

  it("shows «Неверный логин или пароль» for 401 invalid_credentials and stays put", async () => {
    server.use(loginHandler(() => problem(401, "invalid_credentials")).handler);
    const { user, router } = renderApp("/login?next=%2Finvite%2Fabc");

    await signIn(user, "vasya", "wrongpass1");

    expect((await screen.findByRole("alert")).textContent).toContain("Неверный логин или пароль");
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toEqual({ next: "/invite/abc" });
  });

  it("shows the wait from Retry-After on 429", async () => {
    server.use(loginHandler(() => problem(429, "rate_limited", undefined, { "Retry-After": "42" })).handler);
    const { user } = renderApp("/login");

    await signIn(user, "vasya", "password1");

    expect((await screen.findByRole("alert")).textContent).toBe("Слишком много попыток, подождите 42 с.");
  });

  it("lower-cases the login as it is typed", async () => {
    const login = loginHandler(() => HttpResponse.json(testUser));
    server.use(login.handler);
    const { user } = renderApp("/login");

    await user.type(await screen.findByRole("textbox", { name: "Логин" }), "Ivan.Petrov");

    expect(screen.getByRole<HTMLInputElement>("textbox", { name: "Логин" }).value).toBe("ivan.petrov");
    await user.type(screen.getByLabelText("Пароль"), "password1");
    await user.click(screen.getByRole("button", { name: "Войти" }));
    await screen.findByRole("heading", { level: 1, name: "Друзья" });
    expect(login.bodies[0]?.username).toBe("ivan.petrov");
  });

  it("shows the form errors before any request", async () => {
    const login = loginHandler(() => HttpResponse.json(testUser));
    server.use(login.handler);
    const { user } = renderApp("/login");

    await signIn(user, "a", "1234567");

    expect(await screen.findByText("Логин: 2–32 символа, латиница, цифры, «_» и «.».")).toBeTruthy();
    expect(screen.getByText("Пароль: от 8 до 128 символов.")).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "Логин" }).getAttribute("aria-invalid")).toBe("true");
    expect(login.bodies).toEqual([]);
  });

  it("returns to next after signing in, e.g. an invite", async () => {
    server.use(
      loginHandler(() => HttpResponse.json(testUser)).handler,
      apiHandler("get", "/invites/{code}", () => problem(404, "not_found")),
    );
    const { user, router } = renderApp("/invite/abc");
    await screen.findByRole("heading", { name: "С возвращением!" });
    expect(router.state.location.search).toEqual({ next: "/invite/abc" });

    await signIn(user, "vasya", "password1");

    expect(await screen.findByRole("heading", { name: "Приглашение недействительно" })).toBeTruthy();
    expect(currentHref(router)).toBe("/invite/abc");
  });

  it("ignores a next on another origin and opens /channels/@me", async () => {
    server.use(loginHandler(() => HttpResponse.json(testUser)).handler);
    const { user, router } = renderApp(`/login?next=${encodeURIComponent("//evil.com")}`);

    await signIn(user, "vasya", "password1");

    await screen.findByRole("heading", { level: 1, name: "Друзья" });
    expect(currentHref(router)).toBe("/channels/@me");
  });

  it("links to registration and keeps next", async () => {
    const { user, router } = renderApp("/login?next=%2Finvite%2Fabc");

    await user.click(await screen.findByRole("link", { name: "Зарегистрироваться" }));

    expect(await screen.findByRole("heading", { name: "Создать учётную запись" })).toBeTruthy();
    expect(router.state.location.pathname).toBe("/register");
    expect(router.state.location.search).toEqual({ next: "/invite/abc" });
  });
});
