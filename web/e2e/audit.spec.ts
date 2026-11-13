import { call, createGuild, expect, invite, join, signUp, test } from "./fixtures";

test("audit log: a deleted message, a ban with its reason and a new role, filtered by action", async ({
  browser,
  people,
}) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId } = await createGuild(anna, "Модерация");
  await join(petr, await invite(anna, channelId));

  const message = await call<{ id: string }>(petr, "POST", `/channels/${channelId}/messages`, { content: "реклама" });
  await call(anna, "DELETE", `/channels/${channelId}/messages/${message.id}`);
  await call(anna, "PUT", `/guilds/${guildId}/bans/${petr.id}`, { reason: "спам" });
  await call(anna, "POST", `/guilds/${guildId}/roles`, { name: "модераторы" });

  await anna.page.goto(`/guilds/${guildId}/settings/audit`);
  const log = anna.page.getByRole("list", { name: "Записи журнала" });
  await expect(log.getByText("Роль создана")).toBeVisible();
  await expect(log.getByText("Участник забанен")).toBeVisible();
  await expect(log.getByText("Причина: спам")).toBeVisible();
  await expect(log.getByText("Сообщение удалено")).toBeVisible();

  await anna.page.getByRole("combobox", { name: "Действие" }).click();
  await anna.page.getByRole("option", { name: "Участник забанен" }).click();
  await expect(log.getByText("Роль создана")).toHaveCount(0);
  await expect(log.getByText("Участник забанен")).toBeVisible();
});
