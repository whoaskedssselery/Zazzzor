import type { Page } from "@playwright/test";
import { call, createGuild, expect, invite, join, signUp, test } from "./fixtures";

test.setTimeout(180_000);

/**
 * Every Russian word the page shows or says: its visible text, the title,
 * and the names and hints assistive technology reads (aria-label, title,
 * placeholder, alt). The people here have English names and write English,
 * so whatever is left came from the interface.
 */
const russianOn = (page: Page): Promise<string[]> =>
  page.evaluate(() => {
    const cyrillic = /\p{Script=Cyrillic}/u;
    const found: string[] = [];
    const add = (where: string, text: string | null) => {
      if (text !== null && cyrillic.test(text)) found.push(`${where}: ${text.slice(0, 120)}`);
    };
    add("title", document.title);
    for (const line of document.body.innerText.split("\n")) add("text", line);
    for (const el of document.querySelectorAll("*")) {
      for (const attr of ["aria-label", "aria-description", "title", "placeholder", "alt"]) {
        add(`${el.tagName.toLowerCase()}[${attr}]`, el.getAttribute(attr));
      }
    }
    return found;
  });

/**
 * The screen as it is now holds no Russian (polled: it may still be drawing)
 * but `allowed`, the words that are Russian on purpose in any language.
 */
const english = async (page: Page, what: string, allowed: string[] = []) => {
  const left = async () => (await russianOn(page)).filter((f) => !allowed.some((a) => f.endsWith(`: ${a}`)));
  await expect.poll(left, { message: what, timeout: 10_000 }).toEqual([]);
};

test("an English browser gets every screen in English", async ({ browser, browserName, people }) => {
  test.skip(browserName === "firefox", "one browser is enough to read the texts");
  const anna = await signUp(browser, "Anna", undefined, "en-US");
  const peter = await signUp(browser, "Peter");
  people.push(anna, peter);
  const { guildId, channelId, channelName } = await createGuild(anna, "Movie night");
  await join(peter, await invite(anna, channelId));
  await call(peter, "POST", `/channels/${channelId}/messages`, { content: "hello there" });
  await call(peter, "POST", "/users/@me/relationships", { username: anna.username });
  const page = anna.page;

  // Home and friends, each tab.
  await page.goto("/channels/@me");
  await expect(page.locator("html")).toHaveAttribute("lang", "en");
  await english(page, "home");
  for (const tab of ["Online", "All", "Pending", "Blocked"]) {
    await page.getByRole("tab", { name: new RegExp(`^${tab}`) }).click();
    await english(page, `friends: ${tab}`);
  }
  await page.getByRole("tab", { name: "Add friend" }).click();
  await english(page, "add friend");

  // A server's channel, a message's menu and the emoji picker.
  await page.goto(`/channels/${guildId}/${channelId}`);
  await expect(page.getByRole("textbox", { name: `Message in #${channelName}` })).toBeVisible();
  await english(page, "channel");
  await page.getByText("hello there").click({ button: "right" });
  await english(page, "message menu");
  await page.getByRole("menuitem", { name: "More reactions" }).click();
  await english(page, "emoji picker");
  await page.keyboard.press("Escape");

  // The server menu and its dialogs.
  await page.getByRole("button", { name: "Server menu Movie night" }).click();
  await english(page, "server menu");
  await page.getByRole("menuitem", { name: "Invite people" }).click();
  await english(page, "invite dialog");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Create a server" }).click();
  await english(page, "create server dialog");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Join a server" }).click();
  await english(page, "join server dialog");
  await page.keyboard.press("Escape");

  // Every settings section: the server's, the channel's and the user's own.
  for (const tab of ["overview", "roles", "emoji", "members", "bans", "invites", "audit"]) {
    await page.goto(`/guilds/${guildId}/settings/${tab}`);
    await expect(page.getByRole("heading", { level: 2 }).first()).toBeVisible();
    await english(page, `server settings: ${tab}`);
  }
  for (const tab of ["overview", "permissions"]) {
    await page.goto(`/guilds/${guildId}/channels/${channelId}/settings/${tab}`);
    await expect(page.getByRole("heading", { level: 2 }).first()).toBeVisible();
    await english(page, `channel settings: ${tab}`);
  }
  for (const tab of ["profile", "account", "appearance", "language", "notifications", "voice"]) {
    await page.goto(`/settings/${tab}`);
    await expect(page.getByRole("heading", { level: 2 }).first()).toBeVisible();
    // The language picker names each language in itself, Russian included.
    await english(page, `user settings: ${tab}`, tab === "language" ? ["Русский"] : []);
  }

  // A voice channel, joined: the stage and the voice panel.
  const channels = await call<{ id: string; name: string; type: number }[]>(anna, "GET", `/guilds/${guildId}/channels`);
  const voice = channels.find((c) => c.type === 2);
  if (!voice) throw new Error("a new server has no voice channel");
  await page.goto(`/channels/${guildId}/${voice.id}`);
  await english(page, "voice channel");
  await page.getByRole("button", { name: "Join", exact: true }).click();
  await expect(page.getByRole("region", { name: "Voice connection" })).toContainText("Voice connected", {
    timeout: 20_000,
  });
  await english(page, "in a call");

  // A direct conversation.
  const dm = await call<{ id: string }>(anna, "POST", "/users/@me/channels", { recipient_id: peter.id });
  await page.goto(`/channels/@me/${dm.id}`);
  await expect(page.getByRole("textbox", { name: "Message for @Peter" })).toBeVisible();
  await english(page, "direct message");
});
