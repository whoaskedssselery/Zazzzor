import { execFileSync } from "node:child_process";
import type { Page } from "@playwright/test";
import { COMPOSE_FILE } from "./env";
import { call, createGuild, expect, invite, join, type Person, signUp, test } from "./fixtures";

test.setTimeout(120_000);

/** The server's first voice channel (a new server has one). */
const voiceChannel = async (owner: Person, guildId: string) => {
  const channels = await call<{ id: string; name: string; type: number }[]>(
    owner,
    "GET",
    `/guilds/${guildId}/channels`,
  );
  const voice = channels.find((c) => c.type === 2);
  if (!voice) throw new Error("a new server has no voice channel");
  return voice;
};

/** Opens the voice channel and joins it from its own screen. */
const enter = async (page: Page, guildId: string, channel: { id: string; name: string }) => {
  await page.goto(`/channels/${guildId}/${channel.id}`);
  // Firefox gathers ICE candidates on the default route only while a page neither captures nor may capture,
  // and LiveKit's first connection comes before the microphone. Here LiveKit is on 127.0.0.1, so in Firefox
  // the page holds a capture until the call is up (Playwright cannot grant Firefox the microphone). Not in
  // Chromium: a second capture of its fake device leaves the call's microphone silent. Real servers are not
  // on loopback.
  const firefox = page.context().browser()?.browserType().name() === "firefox";
  if (firefox) {
    await page.evaluate(async () => {
      Object.assign(window, { heldMicrophone: await navigator.mediaDevices.getUserMedia({ audio: true }) });
    });
  }
  await page
    .getByRole("region", { name: `Голосовой канал ${channel.name}` })
    .getByRole("button", { name: "Присоединиться" })
    .click();
  await expect(page.getByRole("region", { name: "Голосовая связь" })).toContainText("Голос подключён");
  if (firefox) {
    await page.evaluate(() => {
      const held = (window as { heldMicrophone?: MediaStream }).heldMicrophone;
      for (const t of held?.getTracks() ?? []) t.stop();
    });
  }
};

/**
 * The noise suppression setting for this person's pages, from the next load.
 * The fake microphone plays short beeps, which any noise suppression may take for
 * noise (the browser's own too, at the start of a call): scenarios that check who
 * speaks turn it off.
 */
const noiseMode = (who: Person, noise: "off" | "enhanced") =>
  who.context.addInitScript(
    ({ key, value }) => {
      localStorage.setItem(key, JSON.stringify({ noise: value }));
    },
    { key: "zazzzor.voice", value: noise },
  );

/** Someone's row under the voice channel in the sidebar. */
const row = (page: Page, channelName: string, name: string) =>
  page
    .getByRole("list", { name: `В канале ${channelName}` })
    .getByRole("listitem")
    .filter({ hasText: name });

test("two friends talk in voice: who is there, who speaks, mute, a restart", async ({ browser, people }) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId } = await createGuild(anna, "Голосовой вечер");
  await join(petr, await invite(anna, channelId));
  const voice = await voiceChannel(anna, guildId);
  await noiseMode(anna, "off");
  await noiseMode(petr, "off");

  await enter(anna.page, guildId, voice);
  await enter(petr.page, guildId, voice);
  await expect(row(petr.page, voice.name, "Аня")).toBeVisible();
  await expect(row(anna.page, voice.name, "Пётр")).toBeVisible();

  // The fake microphone plays a tone: Anna speaks, as Petr sees it.
  await expect(row(petr.page, voice.name, "Аня").locator("[data-speaking]")).toBeVisible({ timeout: 20_000 });

  // Anna mutes herself; Petr sees it.
  await anna.page
    .getByRole("region", { name: "Ваш профиль" })
    .getByRole("button", { name: "Выключить микрофон" })
    .click();
  await expect(row(petr.page, voice.name, "Аня").getByRole("img", { name: "Микрофон выключен" })).toBeVisible();

  // The API restarts and forgets the switches; the call goes on, and Anna's client tells it again.
  execFileSync("docker", ["compose", "-f", COMPOSE_FILE, "restart", "api"], { stdio: "inherit" });
  await expect(row(petr.page, voice.name, "Аня").getByRole("img", { name: "Микрофон выключен" })).toBeVisible({
    timeout: 45_000,
  });
  await expect(row(petr.page, voice.name, "Пётр")).toBeVisible();
});

test("a screen share is watched on demand", async ({ browser, browserName, people }) => {
  test.skip(browserName === "firefox", "Firefox has no fake screen to share in automation");
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId } = await createGuild(anna, "Кинозал");
  await join(petr, await invite(anna, channelId));
  const voice = await voiceChannel(anna, guildId);
  await enter(anna.page, guildId, voice);
  await enter(petr.page, guildId, voice);

  await anna.page
    .getByRole("region", { name: "Голосовая связь" })
    .getByRole("button", { name: "Демонстрация экрана" })
    .click();
  await expect(row(petr.page, voice.name, "Аня").getByText("В ЭФИРЕ")).toBeVisible({ timeout: 20_000 });
  const tile = petr.page.getByRole("article", { name: "Аня — демонстрация экрана" });
  await tile.getByRole("button", { name: "Смотреть" }).click();
  await expect
    .poll(() => tile.locator("video").evaluate((v: HTMLVideoElement) => v.videoWidth), { timeout: 20_000 })
    .toBeGreaterThan(0);
});

test("joining from a second tab takes the call over", async ({ browser, people }) => {
  const anna = await signUp(browser, "Аня");
  people.push(anna);
  const { guildId } = await createGuild(anna, "Две вкладки");
  const voice = await voiceChannel(anna, guildId);
  await enter(anna.page, guildId, voice);
  const second = await anna.context.newPage();
  await enter(second, guildId, voice);
  await expect(anna.page.getByText("Вы подключились к голосу в другом месте", { exact: true })).toBeVisible({
    timeout: 20_000,
  });
  await expect(anna.page.getByRole("region", { name: "Голосовая связь" })).toHaveCount(0);
});

test("enhanced noise suppression runs under the production CSP", async ({ browser, browserName, people }) => {
  const anna = await signUp(browser, "Аня");
  people.push(anna);
  const violations: string[] = [];
  anna.page.on("console", (m) => {
    if (/Content Security Policy|Content-Security-Policy/i.test(m.text())) violations.push(m.text());
  });
  const { guildId } = await createGuild(anna, "Тихий вечер");
  const voice = await voiceChannel(anna, guildId);
  await noiseMode(anna, "enhanced");
  await enter(anna.page, guildId, voice);

  // The call goes on in the settings, which say what runs: the model (about 23 MB) loads, then runs.
  await anna.page.getByRole("button", { name: "Настройки пользователя" }).click();
  await anna.page.getByRole("link", { name: "Голос и видео" }).click();
  const now = anna.page.getByRole("status").filter({ hasText: /^Сейчас:/ });
  if (browserName === "chromium") await expect(now).toHaveText("Сейчас: улучшенное", { timeout: 60_000 });
  // Firefox may not run a 48 kHz context on a microphone of another rate: then a fallback, said as such.
  else await expect(now).not.toHaveText(/загружается/, { timeout: 60_000 });
  expect(violations).toEqual([]);
});
