import { screen, waitFor } from "@testing-library/react";
import { HttpResponse } from "msw";
import { currentHref, renderApp, sessionHandler, testUser } from "@/app/test";
import { api } from "@/shared/api";
import { apiHandler, problem, server } from "@/shared/test";

describe("session", () => {
  it("sends a visitor without a session to /login with the page as next", async () => {
    server.use(sessionHandler(null));
    const { router } = renderApp("/channels/1/2");

    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toEqual({ next: "/channels/1/2" });
  });

  it("sends / to /login without a session", async () => {
    server.use(sessionHandler(null));
    const { router } = renderApp("/");
    await screen.findByRole("heading", { name: "С возвращением!" });
    expect(currentHref(router)).toBe("/login");
  });

  it("sends / to /channels/@me with a session", async () => {
    server.use(sessionHandler(testUser));
    const { router } = renderApp("/");
    await screen.findByText(/Вы вошли как/);
    expect(router.state.location.pathname).toBe("/channels/@me");
  });

  it("sends a signed-in user away from /login to next", async () => {
    server.use(sessionHandler(testUser));
    const { router } = renderApp("/login?next=%2Finvite%2Fabc");
    expect(await screen.findByRole("heading", { name: "Приглашение" })).toBeTruthy();
    expect(currentHref(router)).toBe("/invite/abc");
  });

  it("sends a signed-in user on /login with a dot-segment next to /channels/@me", async () => {
    server.use(sessionHandler(testUser));
    const { router } = renderApp(`/login?next=${encodeURIComponent("/.//evil.com")}`);
    await screen.findByText(/Вы вошли как/);
    expect(currentHref(router)).toBe("/channels/@me");
  });

  it("goes to /login with next on a 401 from PATCH /users/@me", async () => {
    server.use(
      sessionHandler(testUser),
      apiHandler("patch", "/users/@me", () => problem(401, "unauthenticated")),
    );
    const { router } = renderApp("/channels/@me");
    await screen.findByText(/Вы вошли как/);

    await api.PATCH("/users/@me", { body: { display_name: "Вася" } });

    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    expect(router.state.location.search).toEqual({ next: "/channels/@me" });
  });

  it("goes to /login with next when the session check comes back anonymous on a refetch", async () => {
    server.use(sessionHandler(testUser));
    const { router, queryClient } = renderApp("/channels/1/2?x=1");
    await screen.findByRole("heading", { name: "Канал" });

    server.use(sessionHandler(null));
    await queryClient.invalidateQueries({ queryKey: ["me"] });

    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toEqual({ next: "/channels/1/2?x=1" });
  });

  it("serves the protected placeholders with a session", async () => {
    server.use(sessionHandler(testUser));
    const { router } = renderApp("/settings/profile");
    expect(await screen.findByRole("heading", { name: "Настройки" })).toBeTruthy();
    await router.navigate({ to: "/admin/$", params: { _splat: "users" } });
    expect(await screen.findByRole("heading", { name: "Администрирование" })).toBeTruthy();
    await router.navigate({ to: "/channels/$guildId/$channelId", params: { guildId: "1", channelId: "2" } });
    expect(await screen.findByRole("heading", { name: "Канал" })).toBeTruthy();
  });

  it("clears the cache and goes to /login with next on a 401 from any request", async () => {
    server.use(
      sessionHandler(testUser),
      apiHandler("get", "/guilds/{guild_id}", () => problem(401, "unauthenticated")),
    );
    const { router, queryClient } = renderApp("/channels/@me");
    await screen.findByText(/Вы вошли как/);
    queryClient.setQueryData(["guilds", "1"], { stale: true });

    await api.GET("/guilds/{guild_id}", { params: { path: { guild_id: "1" } } });

    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    expect(router.state.location.search).toEqual({ next: "/channels/@me" });
    expect(queryClient.getQueryData(["guilds", "1"])).toBeUndefined();
    expect(queryClient.getQueryData(["me"])).toBeNull();
  });

  it("signs out: POST /auth/logout, an empty cache and the sign-in screen", async () => {
    let loggedOut = false;
    server.use(
      sessionHandler(testUser),
      apiHandler("post", "/auth/logout", () => {
        loggedOut = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user, router, queryClient } = renderApp("/channels/@me");
    await screen.findByText(/Вы вошли как/);
    queryClient.setQueryData(["guilds", "1"], { stale: true });

    await user.click(screen.getByRole("button", { name: "Выйти" }));

    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    expect(loggedOut).toBe(true);
    expect(currentHref(router)).toBe("/login");
    expect(queryClient.getQueryData(["guilds", "1"])).toBeUndefined();
    expect(queryClient.getQueryData(["me"])).toBeNull();
  });

  it("stays signed in and shows a toast when signing out fails", async () => {
    server.use(
      sessionHandler(testUser),
      apiHandler("post", "/auth/logout", () => problem(429, "rate_limited")),
    );
    const { user, router } = renderApp("/channels/@me");
    await screen.findByText(/Вы вошли как/);

    await user.click(screen.getByRole("button", { name: "Выйти" }));

    await waitFor(() => {
      expect(screen.getByRole("status").textContent).toContain("Слишком много запросов");
    });
    expect(router.state.location.pathname).toBe("/channels/@me");
  });
});
