import { screen, waitFor } from "@testing-library/react";
import { HttpResponse } from "msw";
import { setConnectionStatus } from "@/entities/guild";
import { apiHandler, makeMe, renderRouted, server } from "@/shared/test";
import { UserPanel } from "./UserPanel";

const me = makeMe("100", { display_name: "Максим" });

beforeEach(() => {
  server.use(apiHandler("get", "/users/@me", () => HttpResponse.json(me)));
});

afterEach(() => {
  setConnectionStatus("connecting");
});

describe("UserPanel", () => {
  it("shows the avatar, the name and «В сети» once connected", async () => {
    setConnectionStatus("ready");
    await show();
    expect(await screen.findByRole("img", { name: "Максим, в сети" })).toBeTruthy();
    expect(screen.getByText("Максим", { selector: "[data-user-name]" })).toBeTruthy();
    expect(screen.getByText("В сети", { selector: "[data-user-status]" })).toBeTruthy();
  });

  it("says «Не в сети» while the gateway is not connected", async () => {
    await show();
    expect(await screen.findByRole("img", { name: "Максим, не в сети" })).toBeTruthy();
    expect(screen.getByText("Не в сети", { selector: "[data-user-status]" })).toBeTruthy();
  });

  it("opens the user settings", async () => {
    const { user, router } = await show();
    await user.click(await screen.findByRole("button", { name: "Настройки пользователя" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/settings/profile");
    });
  });

  it("has the microphone and sound switches, on by default", async () => {
    await show();
    expect(await screen.findByRole("button", { name: "Выключить микрофон" })).toHaveProperty("ariaPressed", "false");
    expect(screen.getByRole("button", { name: "Выключить звук" })).toHaveProperty("ariaPressed", "false");
  });

  it("picks a status from the menu and shows it", async () => {
    setConnectionStatus("ready");
    const bodies: unknown[] = [];
    server.use(
      apiHandler("patch", "/users/@me", async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        bodies.push(body);
        return HttpResponse.json({ ...me, ...body });
      }),
    );
    const { user } = await show();
    await user.click(await screen.findByRole("button", { name: "Статус: В сети" }));
    await user.click(await screen.findByRole("menuitemradio", { name: /Не беспокоить/ }));
    await waitFor(() => {
      expect(screen.getByText("Не беспокоить", { selector: "[data-user-status]" })).toBeTruthy();
    });
    expect(screen.getByRole("img", { name: "Максим, не беспокоить" })).toBeTruthy();
    expect(bodies).toEqual([{ status: "dnd" }]);
  });

  it("shows «Невидимка» to the user while others see them offline", async () => {
    setConnectionStatus("ready");
    server.use(apiHandler("get", "/users/@me", () => HttpResponse.json({ ...me, status: "invisible" })));
    await show();
    expect(await screen.findByText("Невидимка", { selector: "[data-user-status]" })).toBeTruthy();
    expect(screen.getByRole("img", { name: "Максим, не в сети" })).toBeTruthy();
  });

  it("sets and clears the status line", async () => {
    setConnectionStatus("ready");
    let line: string | null = null;
    server.use(
      apiHandler("patch", "/users/@me", async ({ request }) => {
        const body = (await request.json()) as { custom_status?: string | null };
        if (body.custom_status !== undefined) line = body.custom_status;
        return HttpResponse.json({ ...me, custom_status: line });
      }),
    );
    const { user } = await show();
    await user.click(await screen.findByRole("button", { name: "Статус: В сети" }));
    await user.click(await screen.findByRole("menuitem", { name: "Задать свой статус" }));
    await user.type(await screen.findByLabelText("Что у вас происходит?"), "  На паре  ");
    await user.click(screen.getByRole("button", { name: "Сохранить" }));
    expect(await screen.findByText("На паре", { selector: "[data-user-status]" })).toBeTruthy();
    expect(line).toBe("На паре");

    await user.click(screen.getByRole("button", { name: "Статус: В сети" }));
    await user.click(await screen.findByRole("menuitem", { name: "Убрать свой статус" }));
    expect(await screen.findByText("В сети", { selector: "[data-user-status]" })).toBeTruthy();
    expect(line).toBeNull();
  });
});

const show = () => renderRouted(<UserPanel />, { path: "/channels/@me" });
