import { composer, expect, messages, signUp, test } from "./fixtures";

/** One message of the list, by its text. */
const message = (list: ReturnType<typeof messages>, text: string | RegExp) =>
  list.getByRole("listitem").filter({ hasText: text });

test("two friends talk in real time: invite, markdown, reply, edit, delete, typing", async ({ browser, people }) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);

  // Anna creates a server, which opens on its first text channel.
  await anna.page.goto("/channels/@me");
  await anna.page.getByRole("navigation", { name: "Серверы" }).getByRole("button", { name: "Создать сервер" }).click();
  await anna.page.getByLabel("Название сервера").fill("Вечерний сеанс");
  await anna.page.getByRole("button", { name: "Создать", exact: true }).click();
  await expect(anna.page).toHaveURL(/\/channels\/\d+\/\d+$/);
  const channelName = (await anna.page.getByRole("textbox", { name: /^Сообщение в #/ }).getAttribute("aria-label"))
    ?.replace("Сообщение в #", "")
    .trim();
  if (!channelName) throw new Error("no composer");

  // …and a link for Petr, who joins through it.
  await anna.page.getByRole("button", { name: "Меню сервера Вечерний сеанс" }).click();
  await anna.page.getByRole("menuitem", { name: "Пригласить людей" }).click();
  await anna.page.getByRole("button", { name: "Создать ссылку" }).click();
  const link = await anna.page.getByLabel("Ссылка-приглашение").inputValue();
  await anna.page.getByRole("dialog").getByRole("button", { name: "Закрыть" }).click();
  await petr.page.goto(new URL(link).pathname);
  await petr.page.getByRole("button", { name: "Принять приглашение" }).click();
  await expect(composer(petr.page, channelName)).toBeVisible();

  const annaList = messages(anna.page, channelName);
  const petrList = messages(petr.page, channelName);

  // Markdown renders, HTML stays text.
  await composer(anna.page, channelName).fill("**привет** <script>x</script>");
  await composer(anna.page, channelName).press("Enter");
  const hello = message(petrList, "<script>x</script>");
  await expect(hello.locator("strong", { hasText: "привет" })).toBeVisible();
  await expect(hello).toContainText("привет <script>x</script>");
  await expect(petr.page.locator("main script")).toHaveCount(0);

  // Typing shows on the other side.
  await composer(petr.page, channelName).pressSequentially("и тебе", { delay: 30 });
  await expect(anna.page.getByText("Пётр печатает…")).toBeVisible();

  // Petr replies; Anna sees the reply with what it answers.
  await hello.hover();
  await hello.getByRole("button", { name: "Ответить" }).click();
  await composer(petr.page, channelName).fill("и тебе привет");
  await composer(petr.page, channelName).press("Enter");
  const reply = message(annaList, "и тебе привет");
  await expect(reply.getByRole("button", { name: "Перейти к исходному сообщению" })).toContainText("привет");

  // Anna edits hers; Petr sees it changed.
  // The first match: the reply below it quotes the same text.
  const mine = message(annaList, "<script>x</script>").first();
  await mine.hover();
  await mine.getByRole("button", { name: "Изменить" }).click();
  const editor = anna.page.getByRole("textbox", { name: "Изменить сообщение" });
  await editor.fill("всем привет");
  await editor.press("Enter");
  await expect(message(petrList, "всем привет").first()).toContainText("(изменено)");

  // …and deletes it; it goes from Petr's list too.
  const edited = message(annaList, "всем привет").first();
  await edited.hover();
  await edited.getByRole("button", { name: "Удалить" }).click();
  await anna.page.getByRole("alertdialog").getByRole("button", { name: "Удалить" }).click();
  await expect(message(petrList, "всем привет")).toHaveCount(0);
  await expect(message(annaList, "всем привет")).toHaveCount(0);
});
