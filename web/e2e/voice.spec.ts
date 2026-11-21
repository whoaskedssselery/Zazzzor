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
  await page
    .getByRole("region", { name: `Голосовой канал ${channel.name}` })
    .getByRole("button", { name: "Присоединиться" })
    .click();
  await expect(page.getByRole("region", { name: "Голосовая связь" })).toContainText("Голос подключён");
};

/**
 * The noise suppression setting for this person's pages, from the next load.
 * The fake microphone plays a tone (in Chromium a steady one, see
 * playwright.config.ts), which any noise suppression may take for noise:
 * scenarios that check who speaks turn it off.
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

  // The tile's own buttons take the click, not the tile behind them that zooms.
  await petr.page.getByRole("listitem").filter({ has: tile }).hover();
  await tile.getByRole("button", { name: "Перестать смотреть" }).click();
  await expect(tile.getByRole("button", { name: "Смотреть" })).toBeVisible();
  const own = anna.page.getByRole("article", { name: "Ваша демонстрация экрана" });
  await anna.page.getByRole("listitem").filter({ has: own }).hover();
  await own.getByRole("button", { name: "Остановить" }).click();
  await expect(row(petr.page, voice.name, "Аня").getByText("В ЭФИРЕ")).toHaveCount(0, { timeout: 20_000 });
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

test("a camera shows in its owner's tile", async ({ browser, browserName, people }) => {
  test.skip(browserName === "firefox", "Firefox asks for the camera, Chromium's fake UI allows it");
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId } = await createGuild(anna, "Посиделки");
  await join(petr, await invite(anna, channelId));
  const voice = await voiceChannel(anna, guildId);
  await enter(anna.page, guildId, voice);
  await enter(petr.page, guildId, voice);

  await anna.page
    .getByRole("toolbar", { name: "Управление звонком" })
    .getByRole("button", { name: "Включить камеру" })
    .click();
  // Anna sees herself at once (a local preview); Petr receives her camera for her tile.
  const own = anna.page.getByRole("listitem", { name: "Аня" }).locator("video");
  await expect.poll(() => own.evaluate((v: HTMLVideoElement) => v.videoWidth), { timeout: 20_000 }).toBeGreaterThan(0);
  const hers = petr.page.getByRole("listitem", { name: "Аня" }).locator("video");
  await expect.poll(() => hers.evaluate((v: HTMLVideoElement) => v.videoWidth), { timeout: 20_000 }).toBeGreaterThan(0);

  await anna.page
    .getByRole("toolbar", { name: "Управление звонком" })
    .getByRole("button", { name: "Выключить камеру" })
    .click();
  await expect(petr.page.getByRole("listitem", { name: "Аня" }).locator("video")).toHaveCount(0, { timeout: 20_000 });
});

test("someone's volume goes from 0 to 200 % and is kept", async ({ browser, people }) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId } = await createGuild(anna, "Громкий вечер");
  await join(petr, await invite(anna, channelId));
  const voice = await voiceChannel(anna, guildId);
  await noiseMode(anna, "off");
  await noiseMode(petr, "off");
  const errors: string[] = [];
  petr.page.on("pageerror", (e) => errors.push(e.message));

  await enter(anna.page, guildId, voice);
  await enter(petr.page, guildId, voice);
  const stage = petr.page.getByRole("region", { name: `Голосовой канал ${voice.name}` });
  const slider = petr.page.getByRole("slider", { name: "Громкость пользователя" });

  // Two people fit the stage side by side, with no scroll bar.
  const annaTile = stage.getByRole("listitem", { name: "Аня" });
  const petrTile = stage.getByRole("listitem", { name: "Пётр" });
  await expect(petrTile).toBeVisible();
  const [a, p] = [await annaTile.boundingBox(), await petrTile.boundingBox()];
  expect(Math.abs((a?.y ?? 0) - (p?.y ?? 1))).toBeLessThan(1);
  const scrolls = await stage.locator(".overflow-y-auto").evaluate((el) => el.scrollHeight > el.clientHeight);
  expect(scrolls).toBe(false);

  // Petr hears Anna (her fake microphone plays a tone), then turns her up past 100 %: the sound
  // moves to a gain node and she is still heard speaking.
  await expect(row(petr.page, voice.name, "Аня").locator("[data-speaking]")).toBeVisible({ timeout: 20_000 });
  await stage.getByRole("listitem", { name: "Аня" }).click({ button: "right" });
  await expect(slider).toHaveValue("100");
  await slider.press("End");
  await expect(slider).toHaveValue("200");
  await expect(petr.page.getByText("200%")).toBeVisible();
  await petr.page.keyboard.press("Escape");
  await expect(row(petr.page, voice.name, "Аня").locator("[data-speaking]")).toBeVisible({ timeout: 20_000 });

  // His own tile offers no volume; a left click on hers still brings it to the front.
  await stage.getByRole("listitem", { name: "Пётр" }).click({ button: "right" });
  await expect(petr.page.getByRole("menu")).toBeVisible();
  await expect(slider).toHaveCount(0);
  await petr.page.keyboard.press("Escape");
  await stage.getByRole("button", { name: "Развернуть: Аня" }).click();
  await expect(stage.getByRole("list", { name: "Остальные в звонке" })).toBeVisible();
  await stage.getByRole("button", { name: "Вернуть в сетку: Аня" }).click();

  // The sidebar offers no volume: it is set in the call only.
  await row(petr.page, voice.name, "Аня").click({ button: "right" });
  await expect(petr.page.getByRole("menu")).toBeVisible();
  await expect(slider).toHaveCount(0);
  await petr.page.keyboard.press("Escape");

  // Down to silence, and it is kept on this device across a reload.
  await stage.getByRole("listitem", { name: "Аня" }).click({ button: "right" });
  await slider.press("Home");
  await expect(slider).toHaveValue("0");
  await petr.page.keyboard.press("Escape");
  await enter(petr.page, guildId, voice);
  await petr.page
    .getByRole("region", { name: `Голосовой канал ${voice.name}` })
    .getByRole("listitem", { name: "Аня" })
    .click({ button: "right" });
  await expect(petr.page.getByRole("slider", { name: "Громкость пользователя" })).toHaveValue("0");
  expect(errors).toEqual([]);
});
