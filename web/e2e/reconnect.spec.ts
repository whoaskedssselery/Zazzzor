import { execFileSync } from "node:child_process";
import type { Page, WebSocketRoute } from "@playwright/test";
import { API_PORT, COMPOSE_FILE } from "./env";
import { call, composer, createGuild, expect, invite, join, messages, openChannel, signUp, test } from "./fixtures";

test.setTimeout(120_000);

const compose = (...args: string[]) => {
  execFileSync("docker", ["compose", "-f", COMPOSE_FILE, ...args], { stdio: "inherit" });
};

const apiReady = async () => {
  await expect
    .poll(
      async () => {
        try {
          return (await fetch(`http://127.0.0.1:${API_PORT}/api/v1/readyz`)).ok;
        } catch {
          return false;
        }
      },
      { timeout: 60_000 },
    )
    .toBe(true);
};

/** The connection banner: «Переподключение…» while the gateway is lost, empty once ready. */
const banner = (page: Page) => page.locator("[data-connection-banner]");

/**
 * Puts the page's gateway behind a switch: while `down`, its socket is
 * dropped and every reconnect is refused, as if this one person lost the
 * network; everyone else stays connected.
 */
const gatewaySwitch = async (page: Page) => {
  let down = false;
  const open = new Set<WebSocketRoute>();
  await page.routeWebSocket(/\/api\/v1\/gateway/, (ws) => {
    if (down) {
      void ws.close({ code: 1001, reason: "e2e: offline" });
      return;
    }
    ws.connectToServer();
    open.add(ws);
    ws.onClose(() => open.delete(ws));
  });
  return {
    drop: async () => {
      down = true;
      await Promise.all([...open].map((ws) => ws.close({ code: 1001, reason: "e2e: offline" })));
    },
    restore: () => {
      down = false;
    },
  };
};

test("the chat survives a restart of the API and a dropped connection", async ({ browser, people }) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId, channelName } = await createGuild(anna, "Перезапуск");
  await join(petr, await invite(anna, channelId));
  const petrGateway = await gatewaySwitch(petr.page);
  await openChannel(anna, guildId, channelId, channelName);
  await openChannel(petr, guildId, channelId, channelName);
  const petrList = messages(petr.page, channelName);

  // The API restarts under both: the banner says so, then goes.
  compose("stop", "api");
  await expect(banner(petr.page)).toHaveText("Переподключение…");
  compose("start", "api");
  await apiReady();
  await expect(banner(anna.page)).toHaveText("", { timeout: 45_000 });
  await expect(banner(petr.page)).toHaveText("", { timeout: 45_000 });
  await composer(anna.page, channelName).fill("снова на связи");
  await composer(anna.page, channelName).press("Enter");
  await expect(petrList.getByText("снова на связи")).toBeVisible();

  await composer(anna.page, channelName).fill("это я удалю");
  await composer(anna.page, channelName).press("Enter");
  await expect(petrList.getByText("это я удалю")).toBeVisible();

  // Petr alone drops off. Meanwhile Anna writes, edits and deletes: he gets all of it once, after.
  await petrGateway.drop();
  await expect(banner(petr.page)).toHaveText("Переподключение…");
  await composer(anna.page, channelName).fill("пока тебя не было");
  await composer(anna.page, channelName).press("Enter");
  await expect(messages(anna.page, channelName).getByText("пока тебя не было")).toBeVisible();
  const sent = await call<{ id: string; content: string }[]>(anna, "GET", `/channels/${channelId}/messages`);
  const idOf = (text: string) => {
    const m = sent.find((x) => x.content === text);
    if (!m) throw new Error(`no message «${text}»`);
    return m.id;
  };
  await call(anna, "PATCH", `/channels/${channelId}/messages/${idOf("снова на связи")}`, {
    content: "снова на связи!",
  });
  await call(anna, "DELETE", `/channels/${channelId}/messages/${idOf("это я удалю")}`);
  petrGateway.restore();
  await expect(banner(petr.page)).toHaveText("", { timeout: 45_000 });
  await expect(petrList.getByText("пока тебя не было")).toHaveCount(1);
  await expect(petrList.getByText("снова на связи!")).toHaveCount(1);
  await expect(petrList.getByText("это я удалю")).toHaveCount(0);
});
