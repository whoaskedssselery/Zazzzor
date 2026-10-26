import { screen, waitFor } from "@testing-library/react";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { useChannelUi } from "@/entities/channel";
import { EMPTY_GUILDS, getGuildsState } from "@/entities/guild";
import { apiHandler, dispatchFrame, gateway, makeGuildState, makeReady, readyGateway, server } from "@/shared/test";

/** The connection banner: the only live region on these screens (no toast is shown). */
const banner = () => screen.getByRole("status");

describe("gateway", () => {
  it("connects once signed in and says «Подключение…» until READY", async () => {
    server.use(sessionHandler(testUser));
    renderApp("/channels/@me");
    await screen.findByText(/Вы вошли как/);
    // The default gateway of the tests accepts and never says HELLO.
    expect(banner().textContent).toBe("Подключение…");
  });

  it("fills the guild store from READY and clears the banner", async () => {
    server.use(sessionHandler(testUser), readyGateway(makeReady(testUser, [makeGuildState("10", testUser)])));
    renderApp("/channels/@me");
    await waitFor(() => {
      expect(getGuildsState().order).toEqual(["10"]);
    });
    await waitFor(() => {
      expect(banner().textContent).toBe("");
    });
  });

  it("does not connect without a session", async () => {
    let connections = 0;
    server.use(
      sessionHandler(null),
      gateway.addEventListener("connection", () => {
        connections += 1;
      }),
    );
    renderApp("/channels/@me");
    await screen.findByRole("heading", { name: "С возвращением!" });
    expect(connections).toBe(0);
  });

  it("says «Переподключение…» after the server restarts, and a fresh READY replaces the store", async () => {
    server.use(
      sessionHandler(testUser),
      readyGateway(
        (n) => makeReady(testUser, [makeGuildState(n === 1 ? "10" : "20", testUser)]),
        (client, n) => {
          if (n === 1) {
            setTimeout(() => {
              client.close(1012, "service_restart");
            }, 0);
          }
        },
      ),
    );
    renderApp("/channels/@me");
    await waitFor(() => {
      expect(banner().textContent).toBe("Переподключение…");
    });
    // The first retry waits 1 s ± 30 % (never under 1 s).
    await waitFor(
      () => {
        expect(getGuildsState().order).toEqual(["20"]);
      },
      { timeout: 3000 },
    );
    expect(getGuildsState().guilds["10"]).toBeUndefined();
    await waitFor(() => {
      expect(banner().textContent).toBe("");
    });
  });

  it("shows a USER_UPDATE of the signed-in user wherever the session user is read", async () => {
    server.use(
      sessionHandler(testUser),
      readyGateway(makeReady(testUser, [makeGuildState("10", testUser)]), (client) => {
        client.send(dispatchFrame("USER_UPDATE", 2, { ...testUser, display_name: "Василий" }));
      }),
    );
    renderApp("/channels/@me");
    expect(await screen.findByText(/Вы вошли как Василий/)).toBeTruthy();
  });

  it("goes to sign-in when the server closes with 4004", async () => {
    server.use(
      sessionHandler(testUser),
      readyGateway(makeReady(testUser, [makeGuildState("10", testUser)]), (client) => {
        setTimeout(() => {
          client.close(4004, "authentication_failed");
        }, 0);
      }),
    );
    const { router } = renderApp("/channels/@me");
    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    expect(router.state.location.search).toEqual({ next: "/channels/@me" });
    expect(getGuildsState()).toEqual(EMPTY_GUILDS);
  });

  it("closes the connection and empties the client stores on sign-out", async () => {
    let closed = false;
    server.use(
      sessionHandler(testUser),
      apiHandler("post", "/auth/logout", () => new HttpResponse(null, { status: 204 })),
      readyGateway(makeReady(testUser, [makeGuildState("10", testUser)]), (client) => {
        client.addEventListener("close", () => {
          closed = true;
        });
      }),
    );
    const { user } = renderApp("/channels/@me");
    await waitFor(() => {
      expect(getGuildsState().order).toEqual(["10"]);
    });
    useChannelUi.getState().setLastChannel("10", "101");

    await user.click(screen.getByRole("button", { name: "Выйти" }));

    expect(await screen.findByRole("heading", { name: "С возвращением!" })).toBeTruthy();
    expect(getGuildsState()).toEqual(EMPTY_GUILDS);
    expect(useChannelUi.getState().lastChannel).toEqual({});
    await waitFor(() => {
      expect(closed).toBe(true);
    });
  });
});
