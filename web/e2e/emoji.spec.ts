import { composer, createGuild, expect, invite, join, messages, openChannel, signUp, test } from "./fixtures";

/** A 1×1 red PNG: the smallest picture the server takes as an emoji. */
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

test("custom emoji: uploaded in settings, typed as :name:, used as a reaction, renamed", async ({
  browser,
  people,
}) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId, channelName } = await createGuild(anna, "Мемы");
  await join(petr, await invite(anna, channelId));

  // Anna, the owner, uploads one; its name comes from the file.
  await anna.page.goto(`/guilds/${guildId}/settings/emoji`);
  await anna.page
    .locator('input[type="file"]')
    .setInputFiles({ name: "vasya.png", mimeType: "image/png", buffer: PNG });
  const field = anna.page.getByRole("textbox", { name: "Имя эмодзи :vasya:" });
  await expect(field).toBeVisible();

  // Petr types it and sends it; both see the picture.
  await openChannel(petr, guildId, channelId, channelName);
  await composer(petr.page, channelName).fill("смотри :vas");
  await petr.page.getByRole("option", { name: ":vasya:" }).click();
  await composer(petr.page, channelName).press("Enter");
  const petrMessage = messages(petr.page, channelName).getByRole("listitem").filter({ hasText: "смотри" });
  await expect(petrMessage.getByRole("img", { name: ":vasya:" })).toBeVisible();
  await expect(petrMessage.getByRole("img", { name: ":vasya:" })).toHaveJSProperty("naturalWidth", 1);

  // He reacts with it from the picker's search.
  await petrMessage.hover();
  await petrMessage.getByRole("button", { name: "Добавить реакцию" }).first().click();
  const picker = petr.page.getByRole("dialog", { name: "Выбор реакции" });
  await picker.getByRole("searchbox", { name: "Найти эмодзи" }).fill("vas");
  await picker.getByRole("button", { name: ":vasya:" }).click();
  await expect(petrMessage.getByRole("button", { name: ":vasya: 1" })).toHaveAttribute("aria-pressed", "true");

  // Anna renames it: the message and the reaction follow, still one group.
  await field.fill("vasily");
  await field.press("Enter");
  await openChannel(anna, guildId, channelId, channelName);
  const annaMessage = messages(anna.page, channelName).getByRole("listitem").filter({ hasText: "смотри" });
  await expect(annaMessage.getByRole("img", { name: ":vasily:" }).first()).toBeVisible();
  await annaMessage.getByRole("button", { name: ":vasily: 1" }).click();
  await expect(petrMessage.getByRole("button", { name: ":vasily: 2" })).toBeVisible();
});
