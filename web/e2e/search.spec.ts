import { composer, createGuild, expect, invite, join, messages, openChannel, signUp, test } from "./fixtures";

test("search: a word in any form finds the message and opens it in its channel", async ({ browser, people }) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId, channelName } = await createGuild(anna, "Вечерний сеанс");
  await join(petr, await invite(anna, channelId));
  await openChannel(petr, guildId, channelId, channelName);

  for (const text of ["Смотрели вчера новый фильм", "Кто идёт в кино в субботу?", "Купил чай"]) {
    await composer(petr.page, channelName).fill(text);
    await composer(petr.page, channelName).press("Enter");
    await expect(messages(petr.page, channelName).getByText(text)).toBeVisible();
  }

  await openChannel(anna, guildId, channelId, channelName);
  await anna.page.getByRole("button", { name: "Поиск по сообщениям" }).click();
  await anna.page.getByRole("searchbox", { name: "Что искать" }).fill("фильмы");
  await anna.page.getByRole("searchbox", { name: "Что искать" }).press("Enter");
  const results = anna.page.getByRole("list", { name: "Результаты поиска" });
  await expect(anna.page.getByText("Найдено: 1")).toBeVisible();
  await expect(results.getByText("Смотрели вчера новый фильм")).toBeVisible();

  // The start of a word is enough.
  await anna.page.getByRole("searchbox", { name: "Что искать" }).fill("кин");
  await anna.page.getByRole("searchbox", { name: "Что искать" }).press("Enter");
  await expect(results.getByText("Кто идёт в кино в субботу?")).toBeVisible();
  await results.getByRole("button").first().click();
  await expect(messages(anna.page, channelName).getByText("Кто идёт в кино в субботу?")).toBeVisible();
});
