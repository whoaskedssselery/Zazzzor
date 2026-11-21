import { screen, waitFor } from "@testing-library/react";
import { HttpResponse } from "msw";
import { applyGatewayDispatch, resetGuilds } from "@/entities/guild";
import {
  apiHandler,
  makeChannel,
  makeGuild,
  makeGuildState,
  makeReady,
  makeUser,
  problem,
  renderRouted,
  server,
} from "@/shared/test";
import { CreateGuildDialog } from "./CreateGuildDialog";

const me = makeUser("100");

beforeEach(() => {
  applyGatewayDispatch("READY", makeReady(me, []));
});

afterEach(() => {
  resetGuilds();
});

const open = () =>
  renderRouted(<CreateGuildDialog open onOpenChange={() => {}} />, {
    path: "/channels/@me",
  });

describe("CreateGuildDialog", () => {
  it("checks the name (2–100 characters) before sending", async () => {
    let requests = 0;
    server.use(
      apiHandler("post", "/guilds", () => {
        requests += 1;
        return problem(500, "internal");
      }),
    );
    const { user } = await open();
    await user.type(await screen.findByLabelText("Название сервера"), " а ");
    await user.click(screen.getByRole("button", { name: "Создать" }));
    expect(await screen.findByText("Название: от 2 до 100 символов.")).toBeTruthy();
    expect(requests).toBe(0);
  });

  it("creates the server, waits for its GUILD_CREATE and opens its first text channel", async () => {
    let body: unknown;
    server.use(
      apiHandler("post", "/guilds", async ({ request }) => {
        body = await request.json();
        const guild = makeGuild("50", me.id, { name: "Дача" });
        // The gateway announces the server a moment after the answer.
        setTimeout(() => {
          applyGatewayDispatch(
            "GUILD_CREATE",
            makeGuildState("50", me, {
              guild,
              channels: [
                makeChannel("60", "50", { type: 4, name: "Текстовые каналы" }),
                makeChannel("62", "50", { type: 2, name: "Основной", position: 0 }),
                makeChannel("61", "50", { name: "общий", parent_id: "60" }),
              ],
            }),
          );
        }, 20);
        return HttpResponse.json(guild, { status: 201 });
      }),
    );
    const { user, router } = await open();
    await user.type(await screen.findByLabelText("Название сервера"), "  Дача ");
    await user.click(screen.getByRole("button", { name: "Создать" }));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/50/61");
    });
    // The default channels are named in the creator's language.
    expect(body).toEqual({ name: "Дача", locale: "ru" });
  });

  it("shows why the server refused", async () => {
    server.use(apiHandler("post", "/guilds", () => problem(400, "too_many_guilds")));
    const { user } = await open();
    await user.type(await screen.findByLabelText("Название сервера"), "Дача");
    await user.click(screen.getByRole("button", { name: "Создать" }));
    expect((await screen.findByRole("alert")).textContent).toBe("Достигнут предел числа серверов.");
  });

  it("says so when the server does not arrive in time, keeps the dialog open and opens the server once it does", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    onTestFinished(() => {
      vi.useRealTimers();
    });
    const guild = makeGuild("50", me.id, { name: "Дача" });
    let answered = false;
    server.use(
      apiHandler("post", "/guilds", () => {
        answered = true;
        return HttpResponse.json(guild, { status: 201 });
      }),
    );
    const { user, router } = await open();
    await user.type(await screen.findByLabelText("Название сервера"), "Дача");
    await user.click(screen.getByRole("button", { name: "Создать" }));
    await waitFor(() => {
      expect(answered).toBe(true);
    });

    vi.advanceTimersByTime(15_000);

    expect((await screen.findByRole("alert")).textContent).toBe(
      "Сервер «Дача» создан, но связь прервалась. Он появится в списке серверов, когда подключение восстановится.",
    );
    expect(router.state.location.pathname).toBe("/channels/@me");
    expect(screen.getByRole("dialog", { name: "Создать сервер" })).toBeTruthy();
    // Created already: no second server from a second click.
    expect(screen.queryByRole("button", { name: "Создать" })).toBeNull();

    applyGatewayDispatch("GUILD_CREATE", makeGuildState("50", me, { guild }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/50/501");
    });
  });
});
