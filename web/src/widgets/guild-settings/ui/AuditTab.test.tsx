import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse } from "msw";
import { applyReady, type GuildEntry, PERM } from "@/entities/guild";
import {
  apiHandler,
  makeEveryone,
  makeGuildState,
  makeMember,
  makeReady,
  makeRole,
  makeUser,
  server,
} from "@/shared/test";
import { UiProviders } from "@/shared/ui";
import type { AuditEntry, AuditLog } from "../model/audit";
import { AUDIT_PAGE, AuditTab } from "./AuditTab";

const G = "10";
const me = makeUser("100", { display_name: "Аня" });
const petr = makeUser("200", { display_name: "Пётр" });
const gone = makeUser("300", { display_name: "Ушедший" });
const mods = makeRole("500", { name: "модераторы", position: 1 });

const entry = ((): GuildEntry => {
  const state = makeGuildState(G, me, {
    roles: [makeEveryone(G), mods],
    members: [makeMember(me), makeMember(petr)],
  });
  const e = applyReady(makeReady(me, [state])).guilds[G];
  if (!e) throw new Error("unreachable");
  return e;
})();

const at = "2026-11-13T10:05:00Z";
const row = (id: string, over: Partial<AuditEntry>): AuditEntry => ({
  id,
  action_type: 20,
  user_id: me.id,
  target_id: null,
  changes: [],
  options: {},
  reason: null,
  created_at: at,
  ...over,
});

const renderTab = () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <UiProviders>
        <AuditTab entry={entry} />
      </UiProviders>
    </QueryClientProvider>,
  );
  return user;
};

describe("AuditTab", () => {
  it("says who did what to whom, with the changes and the reason", async () => {
    const page: AuditLog = {
      audit_log_entries: [
        row("9", { action_type: 22, target_id: gone.id, reason: "спам" }),
        row("8", { action_type: 25, target_id: petr.id, changes: [{ key: "$add", new_value: [mods.id] }] }),
        row("7", {
          action_type: 31,
          target_id: mods.id,
          changes: [
            { key: "name", old_value: "помощники", new_value: "модераторы" },
            { key: "permissions", old_value: "0", new_value: String(PERM.KICK_MEMBERS) },
          ],
        }),
        row("6", { action_type: 12, target_id: "999", changes: [{ key: "name", old_value: "флуд" }] }),
      ],
      users: [me, petr, gone],
    };
    server.use(apiHandler("get", "/guilds/{guild_id}/audit-logs", () => HttpResponse.json(page)));
    renderTab();
    const list = await screen.findByRole("list", { name: "Записи журнала" });
    const items = within(list)
      .getAllByRole("listitem")
      .filter((li) => li.parentElement === list);
    expect(items.map((li) => li.querySelector("p")?.textContent)).toEqual([
      "Участник забанен: Ушедший",
      "Роли участника изменены: Пётр",
      "Роль изменена: модераторы",
      "Канал удалён: #флуд",
    ]);
    expect(within(list).getByText("Причина: спам")).toBeTruthy();
    expect(within(list).getByText("Выданы роли: модераторы")).toBeTruthy();
    expect(within(list).getByText("Название: помощники → модераторы")).toBeTruthy();
    expect(within(list).getByText("Права: + Выгонять участников")).toBeTruthy();
    expect(within(list).getByText("Название: флуд")).toBeTruthy();
  });

  it("filters by action and pages back", async () => {
    const asked: URLSearchParams[] = [];
    server.use(
      apiHandler("get", "/guilds/{guild_id}/audit-logs", ({ request }) => {
        const q = new URL(request.url).searchParams;
        asked.push(q);
        const before = q.get("before");
        const start = before === null ? 1000 : Number(before) - 1;
        const n = before === null ? AUDIT_PAGE : 2;
        const entries = Array.from({ length: n }, (_, i) =>
          row(String(start - i), { action_type: 20, target_id: petr.id }),
        );
        return HttpResponse.json({ audit_log_entries: entries, users: [me, petr] });
      }),
    );
    const user = renderTab();
    const list = await screen.findByRole("list", { name: "Записи журнала" });
    await user.click(screen.getByRole("button", { name: "Показать ещё" }));
    await waitFor(() => {
      expect(
        within(list).getAllByText((_, el) => el?.tagName === "P" && el.textContent === "Участник выгнан: Пётр"),
      ).toHaveLength(AUDIT_PAGE + 2);
    });
    expect(screen.queryByRole("button", { name: "Показать ещё" })).toBeNull();
    expect(asked.at(-1)?.get("before")).toBe(String(1000 - AUDIT_PAGE + 1));

    await user.click(screen.getByRole("combobox", { name: "Действие" }));
    await user.click(await screen.findByRole("option", { name: "Участник забанен" }));
    await waitFor(() => {
      expect(asked.at(-1)?.get("action_type")).toBe("22");
    });
  });

  it("says when nothing happened yet", async () => {
    server.use(
      apiHandler("get", "/guilds/{guild_id}/audit-logs", () => HttpResponse.json({ audit_log_entries: [], users: [] })),
    );
    renderTab();
    expect(await screen.findByText("Пока ничего не происходило.")).toBeTruthy();
  });
});
