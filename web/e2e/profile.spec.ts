import { createGuild, expect, invite, join, openChannel, signUp, test } from "./fixtures";

test("profiles: a status, a status line and «О себе» reach the other member live", async ({ browser, people }) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId, channelName } = await createGuild(anna, "Вечерний сеанс");
  await join(petr, await invite(anna, channelId));
  await openChannel(anna, guildId, channelId, channelName);
  await openChannel(petr, guildId, channelId, channelName);
  const members = anna.page.getByRole("complementary", { name: "Участники" });
  await expect(members.getByRole("img", { name: "Пётр, в сети" })).toBeVisible();

  // Petr writes about himself in the settings.
  await petr.page.getByRole("button", { name: "Настройки пользователя" }).click();
  await petr.page.getByRole("textbox", { name: "О себе" }).fill("Люблю кино");
  await petr.page.getByRole("button", { name: "Сохранить изменения" }).click();
  await expect(petr.page.getByText("Изменения сохранены", { exact: true })).toBeVisible();
  await petr.page.keyboard.press("Escape");

  // …then picks «Не беспокоить» and a status line from his panel.
  const panel = petr.page.getByRole("region", { name: "Ваш профиль" });
  await panel.getByRole("button", { name: "Статус: В сети" }).click();
  await petr.page.getByRole("menuitemradio", { name: /Не беспокоить/ }).click();
  await panel.getByRole("button", { name: "Статус: Не беспокоить" }).click();
  await petr.page.getByRole("menuitem", { name: "Задать свой статус" }).click();
  await petr.page.getByLabel("Что у вас происходит?").fill("Смотрю фильм");
  await petr.page.getByRole("button", { name: "Сохранить" }).click();

  // Anna sees both without reloading, and «О себе» on his card.
  await expect(members.getByRole("img", { name: "Пётр, не беспокоить" })).toBeVisible();
  await expect(members.getByText("Смотрю фильм")).toBeVisible();
  await members.getByRole("button", { name: /Пётр/ }).click();
  const card = anna.page.getByRole("menu");
  await expect(card.getByText("Люблю кино")).toBeVisible();
  await anna.page.keyboard.press("Escape");

  // Invisible looks offline to her.
  await panel.getByRole("button", { name: "Статус: Не беспокоить" }).click();
  await petr.page.getByRole("menuitemradio", { name: /Невидимка/ }).click();
  await expect(members.getByRole("img", { name: "Пётр, не в сети" })).toBeVisible();
  await expect(members.getByText("Смотрю фильм")).toHaveCount(0);
});
