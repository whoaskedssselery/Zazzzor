import { screen, waitFor } from "@testing-library/react";
import { HttpResponse } from "msw";
import { setConnectionStatus } from "@/entities/guild";
import { apiHandler, makeUser, renderRouted, server } from "@/shared/test";
import { UserPanel } from "./UserPanel";

const me = makeUser("100", { display_name: "Максим" });

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

  it("has no microphone or headphones buttons until voice arrives", async () => {
    await show();
    await screen.findByRole("button", { name: "Настройки пользователя" });
    expect(screen.queryByRole("button", { name: /микрофон/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /звук|наушник/i })).toBeNull();
  });
});

const show = () => renderRouted(<UserPanel />, { path: "/channels/@me" });
