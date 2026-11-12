import { call, expect, type Person, signUp, test } from "./fixtures";

/** Makes `a` and `b` friends through the API: a request, then its acceptance. */
const befriend = async (a: Person, b: Person) => {
  await call(a, "POST", "/users/@me/relationships", { username: b.username });
  await call(b, "PUT", `/users/@me/relationships/${a.id}`, { type: 1 });
};

const dmList = (who: Person) => who.page.getByRole("region", { name: "Личные сообщения" });

test("direct messages: a DM from the friends screen, unread on the other side, then a group", async ({
  browser,
  people,
}) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  const vera = await signUp(browser, "Вера");
  people.push(anna, petr, vera);
  await befriend(anna, petr);
  await befriend(anna, vera);
  await petr.page.goto("/channels/@me");
  await vera.page.goto("/channels/@me");

  // Anna writes to Petr from her friends.
  await anna.page.goto("/channels/@me");
  await anna.page.getByRole("tab", { name: "Все" }).click();
  await anna.page.getByRole("button", { name: "Написать Пётр" }).click();
  await expect(anna.page.getByRole("heading", { level: 1, name: "Пётр" })).toBeVisible();
  const field = anna.page.getByRole("textbox", { name: "Сообщение для @Пётр" });
  await field.fill("привет, это личка");
  await field.press("Enter");
  await expect(anna.page.getByRole("list", { name: "Сообщения с @Пётр" }).getByText("привет, это личка")).toBeVisible();

  // Petr gets it: the DM appears with one unread, and opening it reads it.
  const unread = dmList(petr).getByRole("link", { name: "Аня, непрочитанных: 1" });
  await expect(unread).toBeVisible();
  await unread.click();
  await expect(petr.page.getByRole("list", { name: "Сообщения с @Аня" }).getByText("привет, это личка")).toBeVisible();
  await expect(dmList(petr).getByRole("link", { name: "Аня", exact: true })).toBeVisible();

  // Anna makes a group with both; Vera sees it and can write there.
  await dmList(anna).getByRole("button", { name: "Создать личное сообщение" }).click();
  const dialog = anna.page.getByRole("dialog", { name: "Выберите друзей" });
  await dialog.getByRole("checkbox", { name: /Пётр/ }).click();
  await dialog.getByRole("checkbox", { name: /Вера/ }).click();
  await dialog.getByRole("button", { name: "Создать группу" }).click();
  await expect(anna.page.getByRole("heading", { level: 1, name: /Пётр, Вера|Вера, Пётр/ })).toBeVisible();

  const group = dmList(vera).getByRole("link", { name: /Аня, Пётр|Пётр, Аня/ });
  await expect(group).toBeVisible();
  await group.click();
  const veraField = vera.page.getByRole("textbox", { name: /^Сообщение в группу/ });
  await veraField.fill("я в деле");
  await veraField.press("Enter");
  await expect(anna.page.getByRole("list", { name: /^Сообщения в группе/ }).getByText("я в деле")).toBeVisible();
});
