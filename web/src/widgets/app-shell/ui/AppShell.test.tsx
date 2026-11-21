import { screen, within } from "@testing-library/react";
import { renderRouted, setViewportWidth } from "@/shared/test";
import { AppShell, MembersButton, NavButton } from "./AppShell";

const Shell = ({ channel = false }: { channel?: boolean }) => (
  <AppShell
    rail={
      <nav aria-label="Серверы">
        <a href="/channels/@me">Главная</a>
        <a href="/channels/10/101" aria-current="page">
          Вечерний сеанс
        </a>
      </nav>
    }
    sidebar={
      <nav data-channels aria-label="Каналы">
        {channel && (
          <a href="/channels/10/101" aria-current="page">
            общий
          </a>
        )}
      </nav>
    }
    userPanel={<section aria-label="Ваш профиль" />}
    members={<aside aria-label="Участники" />}
  >
    <NavButton />
    <MembersButton />
  </AppShell>
);

const members = () => screen.queryAllByRole("complementary", { name: "Участники" });

describe("AppShell", () => {
  it("shows the member list beside the chat on a wide window, and the toggle hides it", async () => {
    setViewportWidth(1440);
    const { user } = await renderRouted(<Shell />);
    expect(members()).toHaveLength(1);
    const toggle = screen.getByRole("button", { name: "Скрыть участников" });
    expect(toggle.getAttribute("aria-pressed")).toBe("true");
    await user.click(toggle);
    expect(members()).toHaveLength(0);
    expect(screen.getByRole("button", { name: "Показать участников" }).getAttribute("aria-pressed")).toBe("false");
  });

  it("keeps the member list unmounted on a narrower window until it is opened over the chat", async () => {
    setViewportWidth(1024);
    const { user } = await renderRouted(<Shell />);
    expect(members()).toHaveLength(0);
    await user.click(screen.getByRole("button", { name: "Показать участников" }));
    expect(members()).toHaveLength(1);
    expect(screen.getByRole("dialog", { name: "Участники" })).toBeTruthy();
  });

  it("on a phone keeps the rail out of the page until the menu opens it once, focused on the open place", async () => {
    setViewportWidth(375);
    const { user } = await renderRouted(<Shell />);
    expect(screen.queryAllByRole("navigation", { name: "Серверы" })).toHaveLength(0);

    await user.click(screen.getByRole("button", { name: "Серверы и каналы" }));

    const sheet = await screen.findByRole("dialog", { name: "Серверы и каналы" });
    expect(screen.getAllByRole("navigation", { name: "Серверы" })).toHaveLength(1);
    expect(document.activeElement).toBe(within(sheet).getByRole("link", { name: "Вечерний сеанс" }));
  });

  it("on a phone focuses the open channel first, so the server's tooltip does not cover the list", async () => {
    setViewportWidth(375);
    const { user } = await renderRouted(<Shell channel />);
    await user.click(screen.getByRole("button", { name: "Серверы и каналы" }));
    const sheet = await screen.findByRole("dialog", { name: "Серверы и каналы" });
    expect(document.activeElement).toBe(within(sheet).getByRole("link", { name: "общий" }));
  });
});
