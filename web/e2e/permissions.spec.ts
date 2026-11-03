import { call, composer, createGuild, expect, invite, join, openChannel, signUp, test } from "./fixtures";

const VIEW_CHANNEL = String(1n << 10n);

test("a member loses and regains a channel, then the server, live", async ({ browser, people }) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId, channelName } = await createGuild(anna, "Штаб");
  const secret = await call<{ id: string }>(anna, "POST", `/guilds/${guildId}/channels`, { name: "секрет", type: 0 });
  const code = await invite(anna, channelId);
  await join(petr, code);

  // Petr is in #секрет when Anna hides it from @everyone: it goes, and so does he.
  await openChannel(petr, guildId, secret.id, "секрет");
  const sidebar = petr.page.getByRole("navigation", { name: "Каналы" });
  await expect(sidebar.getByRole("link", { name: "секрет" })).toBeVisible();
  await call(anna, "PUT", `/channels/${secret.id}/permissions/${guildId}`, { type: 0, allow: "0", deny: VIEW_CHANNEL });
  await expect(petr.page.getByText("Канал больше недоступен", { exact: true })).toBeVisible();
  await expect(sidebar.getByRole("link", { name: "секрет" })).toHaveCount(0);
  await expect(petr.page).toHaveURL(new RegExp(`/channels/${guildId}/${channelId}$`));
  await expect(composer(petr.page, channelName)).toBeVisible();

  // A role that may see it brings it back.
  const role = await call<{ id: string }>(anna, "POST", `/guilds/${guildId}/roles`, { name: "Свои" });
  await call(anna, "PUT", `/channels/${secret.id}/permissions/${role.id}`, { type: 0, allow: VIEW_CHANNEL, deny: "0" });
  await call(anna, "PATCH", `/guilds/${guildId}/members/${petr.id}`, { roles: [role.id] });
  await expect(sidebar.getByRole("link", { name: "секрет" })).toBeVisible();

  // Kicked, Petr loses the server and is told so.
  await call(anna, "DELETE", `/guilds/${guildId}/members/${petr.id}`);
  await expect(petr.page.getByText("Сервер больше недоступен", { exact: true })).toBeVisible();
  await expect(petr.page.getByRole("navigation", { name: "Серверы" }).getByRole("link", { name: "Штаб" })).toHaveCount(
    0,
  );
  await expect(petr.page).toHaveURL(/\/channels\/@me$/);

  // Banned, he cannot come back through the invite.
  await call(anna, "PUT", `/guilds/${guildId}/bans/${petr.id}`, {});
  await petr.page.goto(`/invite/${code}`);
  await petr.page.getByRole("button", { name: "Принять приглашение" }).click();
  await expect(petr.page.getByRole("alert")).toContainText("Вы заблокированы на этом сервере");
});
