import { call, createGuild, expect, invite, join, messages, openChannel, signUp, test } from "./fixtures";

test("reactions: picked by search, joined with a click, seen by everyone, kept after a reload", async ({
  browser,
  people,
}) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId, channelName } = await createGuild(anna, "Вечерний сеанс");
  await join(petr, await invite(anna, channelId));
  await call(petr, "POST", `/channels/${channelId}/messages`, { content: "в девять у меня?" });
  await openChannel(anna, guildId, channelId, channelName);
  await openChannel(petr, guildId, channelId, channelName);

  // Anna reacts from the message's toolbar, finding the emoji by its Russian name.
  const annaMessage = messages(anna.page, channelName).getByRole("listitem").filter({ hasText: "в девять у меня?" });
  await annaMessage.hover();
  await annaMessage.getByRole("button", { name: "Добавить реакцию" }).first().click();
  const picker = anna.page.getByRole("dialog", { name: "Выбор реакции" });
  await picker.getByRole("searchbox", { name: "Найти эмодзи" }).fill("лайк");
  await picker.getByRole("button", { name: "большой палец вверх" }).click();
  await expect(picker).toHaveCount(0);

  // Petr sees it at once and joins it.
  const petrMessage = messages(petr.page, channelName).getByRole("listitem").filter({ hasText: "в девять у меня?" });
  const petrChip = petrMessage.getByRole("button", { name: /^👍️? 1$/ });
  await expect(petrChip).toHaveAttribute("aria-pressed", "false");
  await petrChip.click();
  await expect(petrMessage.getByRole("button", { name: /^👍️? 2$/ })).toHaveAttribute("aria-pressed", "true");
  await expect(annaMessage.getByRole("button", { name: /^👍️? 2$/ })).toHaveAttribute("aria-pressed", "true");

  // Anna takes hers back; the server keeps the state across a reload.
  await annaMessage.getByRole("button", { name: /^👍️? 2$/ }).click();
  await expect(petrMessage.getByRole("button", { name: /^👍️? 1$/ })).toHaveAttribute("aria-pressed", "true");
  await anna.page.reload();
  const chip = messages(anna.page, channelName).getByRole("button", { name: /^👍️? 1$/ });
  await expect(chip).toHaveAttribute("aria-pressed", "false");
});
