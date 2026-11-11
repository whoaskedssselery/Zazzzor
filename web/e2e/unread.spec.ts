import { call, composer, createGuild, expect, invite, join, messages, openChannel, signUp, test } from "./fixtures";

test("mentions and unread: counts in the list and the rail, the «Новые» line, read on open", async ({
  browser,
  people,
}) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);

  const { guildId, channelId, channelName } = await createGuild(anna, "Вечерний сеанс");
  const flood = await call<{ id: string; name: string }>(anna, "POST", `/guilds/${guildId}/channels`, {
    name: "флуд",
    type: 0,
  });
  await join(petr, await invite(anna, channelId));
  await openChannel(petr, guildId, channelId, channelName);

  const channels = petr.page.getByRole("navigation", { name: "Каналы" });
  const rail = petr.page.getByRole("navigation", { name: "Серверы" });

  // Anna writes in #флуд while Petr is elsewhere: once to everyone, once to him.
  await call(anna, "POST", `/channels/${flood.id}/messages`, { content: "кто на сеанс?" });
  await call(anna, "POST", `/channels/${flood.id}/messages`, { content: `<@${petr.id}> ты идёшь?` });
  await expect(channels.getByRole("link", { name: "флуд, упоминаний: 1" })).toBeVisible();
  await expect(rail.getByRole("link", { name: "Вечерний сеанс, упоминаний: 1" })).toBeVisible();

  // He opens it: the line sits above what he has not read, the mention is marked.
  await channels.getByRole("link", { name: /^флуд/ }).click();
  const list = messages(petr.page, "флуд");
  await expect(list.getByText("Новые сообщения")).toHaveCount(1);
  await expect(list.locator("[data-mention=user]")).toHaveText("@Пётр");
  // Reading it to the end clears both counts, and the server keeps it read.
  await expect(channels.getByRole("link", { name: "флуд" })).toBeVisible();
  await expect(rail.getByRole("link", { name: "Вечерний сеанс" })).toBeVisible();
  await petr.page.reload();
  await expect(composer(petr.page, "флуд")).toBeVisible();
  await expect(channels.getByRole("link", { name: "флуд" })).toBeVisible();

  // Petr answers through the @ list; Anna, in #общий, gets the mention.
  await openChannel(anna, guildId, channelId, channelName);
  await composer(petr.page, "флуд").pressSequentially("@Ан", { delay: 30 });
  await petr.page.getByRole("option", { name: /Аня/ }).click();
  await composer(petr.page, "флуд").pressSequentially("иду", { delay: 10 });
  await composer(petr.page, "флуд").press("Enter");
  await expect(list.locator("[data-mention=user]", { hasText: "@Аня" })).toBeVisible();
  await expect(
    anna.page.getByRole("navigation", { name: "Каналы" }).getByRole("link", { name: "флуд, упоминаний: 1" }),
  ).toBeVisible();
});
