import { screen } from "@testing-library/react";
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
    await screen.findByRole("heading", { level: 1, name: "Друзья" });
    expect(router.state.location.pathname).toBe("/channels/@me");
  });

  it("sends a signed-in user away from /login to next", async () => {
    server.use(
      sessionHandler(testUser),
      apiHandler("get", "/invites/{code}", () => problem(404, "not_found")),
    );
    const { router } = renderApp("/login?next=%2Finvite%2Fabc");
    expect(await screen.findByRole("heading", { name: "Приглашение недействительно" })).toBeTruthy();
    expect(currentHref(router)).toBe("/invite/abc");
  });

  it("sends a signed-in user on /login with a dot-segment next to /channels/@me", async () => {
    server.use(sessionHandler(testUser));
    const { router } = renderApp(`/login?next=${encodeURIComponent("/.//evil.com")}`);
    await screen.findByRole("heading", { level: 1, name: "Друзья" });
    expect(currentHref(router)).toBe("/channels/@me");
  });

  it("goes to /login with next on a 401 from PATCH /users/@me", async () => {
    server.use(
      sessionHandler(testUser),
      apiHandler("patch", "/users/@me", () => problem(401, "unauthenticated")),
    );
    const { router } = renderApp("/channels/@me");
    await screen.findByRole("heading", { level: 1, name: "Друзья" });

    await api.PATCH("/users/@me", { body: { display_name: "Вася" } });

    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    expect(router.state.location.search).toEqual({ next: "/channels/@me" });
  });

  it("goes to /login with next when the session check comes back anonymous on a refetch", async () => {
    server.use(sessionHandler(testUser));
    const { router, queryClient } = renderApp("/channels/1/2?x=1");
    await screen.findByRole("navigation", { name: "Серверы" });

    server.use(sessionHandler(null));
    await queryClient.invalidateQueries({ queryKey: ["me"] });

    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toEqual({ next: "/channels/1/2?x=1" });
  });

  it("serves the protected screens with a session", async () => {
    server.use(sessionHandler(testUser));
    const { router } = renderApp("/settings/profile");
    expect(await screen.findByRole("heading", { name: "Профиль" })).toBeTruthy();
    await router.navigate({ to: "/channels/$guildId/$channelId", params: { guildId: "1", channelId: "2" } });
    expect(await screen.findByRole("navigation", { name: "Серверы" })).toBeTruthy();
  });

  it("clears the cache and goes to /login with next on a 401 from any request", async () => {
    server.use(
      sessionHandler(testUser),
      apiHandler("get", "/guilds/{guild_id}", () => problem(401, "unauthenticated")),
    );
    const { router, queryClient } = renderApp("/channels/@me");
    await screen.findByRole("heading", { level: 1, name: "Друзья" });
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
    await screen.findByRole("heading", { level: 1, name: "Друзья" });
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
    await screen.findByRole("heading", { level: 1, name: "Друзья" });

    await user.click(screen.getByRole("button", { name: "Выйти" }));

    // The connection banner is a live region too; the toast is the one with the text.
    expect(await screen.findByText(/Слишком много запросов/)).toBeTruthy();
    expect(router.state.location.pathname).toBe("/channels/@me");
  });
});
