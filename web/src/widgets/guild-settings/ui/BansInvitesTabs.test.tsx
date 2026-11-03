import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse } from "msw";
import type { ReactElement } from "react";
import type { components } from "@/shared/api";
import { apiHandler, makeUser, server } from "@/shared/test";
import { UiProviders } from "@/shared/ui";
import { BansTab } from "./BansTab";
import { expiresText, InvitesTab, usesText } from "./InvitesTab";

type Ban = components["schemas"]["Ban"];
type Invite = components["schemas"]["Invite"];

const G = "10";
const anna = makeUser("3", { display_name: "Аня", username: "anna" });
const petr = makeUser("4", { display_name: "Пётр", username: "petya" });

const renderWith = (ui: ReactElement) => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <UiProviders>{ui}</UiProviders>
    </QueryClientProvider>,
  );
  return user;
};

describe("BansTab", () => {
  it("lists the bans with their reason and date, and lifts one after asking", async () => {
    let bans: Ban[] = [
      { user: anna, reason: "спам", created_at: "2026-10-20T12:00:00Z" },
      { user: petr, reason: null, created_at: "2026-10-21T12:00:00Z" },
    ];
    const lifted: string[] = [];
    server.use(
      apiHandler("get", "/guilds/{guild_id}/bans", () => HttpResponse.json(bans)),
      apiHandler("delete", "/guilds/{guild_id}/bans/{user_id}", ({ params }) => {
        lifted.push(params.user_id);
        bans = bans.filter((b) => b.user?.id !== params.user_id);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = renderWith(<BansTab guildId={G} />);
    const list = await screen.findByRole("list", { name: "Забаненные" });
    expect(within(list).getByText("спам")).toBeTruthy();
    expect(within(list).getByText("Без причины")).toBeTruthy();
    expect(within(list).getByText("Забанен 20 октября 2026 г.")).toBeTruthy();
    await user.click(within(list).getAllByRole("button", { name: "Разбанить" })[0] as HTMLElement);
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Разбанить" }));
    await waitFor(() => {
      expect(screen.queryByText("спам")).toBeNull();
    });
    expect(lifted).toEqual([anna.id]);
  });

  it("says when nobody is banned", async () => {
    server.use(apiHandler("get", "/guilds/{guild_id}/bans", () => HttpResponse.json([])));
    renderWith(<BansTab guildId={G} />);
    expect(await screen.findByText("Никто не забанен.")).toBeTruthy();
  });
});

describe("InvitesTab", () => {
  const invite = (code: string, over: Partial<Invite> = {}): Invite => ({
    code,
    guild: { id: G, name: "Сервер", icon_url: null },
    channel: { id: "101", name: "общий" },
    inviter: anna,
    uses: 3,
    max_uses: 0,
    expires_at: null,
    ...over,
  });

  it("words uses and expiry", () => {
    expect(usesText({ uses: 3, max_uses: 0 })).toBe("3");
    expect(usesText({ uses: 3, max_uses: 10 })).toBe("3 из 10");
    expect(expiresText(null)).toBe("бессрочно");
    expect(expiresText(new Date(2026, 9, 24, 21, 4).toISOString())).toBe("до 24 октября в 21:04");
  });

  it("lists the invites and deletes one", async () => {
    let invites = [invite("AbCd1234"), invite("XyZw5678", { inviter: petr, max_uses: 5 })];
    server.use(
      apiHandler("get", "/guilds/{guild_id}/invites", () => HttpResponse.json(invites)),
      apiHandler("delete", "/invites/{code}", ({ params }) => {
        invites = invites.filter((i) => i.code !== params.code);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = renderWith(<InvitesTab guildId={G} />);
    expect(await screen.findByText("AbCd1234")).toBeTruthy();
    expect(screen.getByText("3 из 5")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Удалить приглашение AbCd1234" }));
    await waitFor(() => {
      expect(screen.queryByText("AbCd1234")).toBeNull();
    });
    expect(screen.getByText("XyZw5678")).toBeTruthy();
  });
});
