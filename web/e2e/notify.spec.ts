import { call, createGuild, expect, invite, join, openChannel, signUp, test } from "./fixtures";

test("a mention in the background: a desktop notification, the count in the tab title, cleared on reading", async ({
  browser,
  people,
}) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  const { guildId, channelId, channelName } = await createGuild(anna, "Вечерний сеанс");
  const flood = await call<{ id: string }>(anna, "POST", `/guilds/${guildId}/channels`, { name: "флуд", type: 0 });
  await join(petr, await invite(anna, channelId));

  // Anna's window is in the background, notifications are allowed; the ones shown are recorded.
  await anna.context.addInitScript(() => {
    document.hasFocus = () => false;
    const shown: { title: string; body: string }[] = [];
    (window as unknown as { __shown: typeof shown }).__shown = shown;
    class FakeNotification {
      static permission = "granted";
      static requestPermission = () => Promise.resolve("granted");
      onclick: (() => void) | null = null;
      constructor(title: string, options?: NotificationOptions) {
        shown.push({ title, body: options?.body ?? "" });
      }
      close() {}
    }
    (window as unknown as { Notification: unknown }).Notification = FakeNotification;
  });
  await openChannel(anna, guildId, channelId, channelName);
  await expect(anna.page).toHaveTitle("Zazzzor");

  await call(petr, "POST", `/channels/${flood.id}/messages`, { content: `<@${anna.id}> ты в игре?` });
  await expect
    .poll(() => anna.page.evaluate(() => (window as unknown as { __shown: unknown[] }).__shown))
    .toEqual([{ title: "Пётр (#флуд, Вечерний сеанс)", body: "@Аня ты в игре?" }]);
  await expect(anna.page).toHaveTitle("(1) Zazzzor");

  // Marking the server read clears the count (with the focus faked away, opening the channel would not).
  await anna.page.getByRole("button", { name: "Меню сервера Вечерний сеанс" }).click();
  await anna.page.getByRole("menuitem", { name: "Отметить как прочитанное" }).click();
  await expect(anna.page).toHaveTitle("Zazzzor");
});
