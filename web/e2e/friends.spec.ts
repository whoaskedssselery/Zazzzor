import { createGuild, expect, invite, join, openChannel, signUp, test } from "./fixtures";

test("friends: a request from the member list, accepted at home, seen online on both sides", async ({
  browser,
  people,
}) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId, channelName } = await createGuild(anna, "Вечерний сеанс");
  await join(petr, await invite(anna, channelId));

  // Anna asks from Petr's card in the member list.
  await openChannel(anna, guildId, channelId, channelName);
  await anna.page.getByRole("complementary", { name: "Участники" }).getByRole("button", { name: /Пётр/ }).click();
  await anna.page.getByRole("menuitem", { name: "Добавить в друзья" }).click();
  await expect(anna.page.getByText("Заявка в друзья отправлена", { exact: true })).toBeVisible();

  // Petr sees it waiting at home and accepts it.
  await petr.page.goto("/channels/@me");
  await expect(petr.page.getByLabel("Входящих заявок: 1", { exact: true })).toBeVisible();
  await petr.page.getByRole("tab", { name: /^Ожидание/ }).click();
  await petr.page.getByRole("button", { name: "Принять заявку от Аня" }).click();
  await expect(petr.page.getByLabel("Входящих заявок: 1", { exact: true })).toHaveCount(0);

  // Both are friends now, and online for each other.
  await petr.page.getByRole("tab", { name: "В сети" }).click();
  await expect(petr.page.getByRole("region", { name: "В сети" }).getByText("Аня")).toBeVisible();
  await anna.page.goto("/channels/@me");
  await expect(anna.page.getByRole("region", { name: "В сети" }).getByText("Пётр")).toBeVisible();
});
