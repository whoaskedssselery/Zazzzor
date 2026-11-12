import { call, expect, type Person, signUp, test } from "./fixtures";

/** Makes `a` and `b` friends through the API: a request, then its acceptance. */
const befriend = async (a: Person, b: Person) => {
  await call(a, "POST", "/users/@me/relationships", { username: b.username });
  await call(b, "PUT", `/users/@me/relationships/${a.id}`, { type: 1 });
};

const voicePanel = (who: Person) => who.page.getByRole("region", { name: "Голосовая связь" });

test("a call in a DM: Anna calls, Petr accepts from another screen, both talk, Anna hangs up", async ({
  browser,
  people,
}) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  await befriend(anna, petr);
  const dm = await call<{ id: string }>(anna, "POST", "/users/@me/channels", { recipient_id: petr.id });
  await petr.page.goto("/channels/@me");

  await anna.page.goto(`/channels/@me/${dm.id}`);
  await anna.page.getByRole("button", { name: "Начать звонок" }).click();
  await expect(voicePanel(anna)).toContainText("Голос подключён");
  await expect(voicePanel(anna)).toContainText("Звонок: Пётр");

  // Petr is on his friends screen: the call rings there.
  const ringing = petr.page.getByRole("alertdialog", { name: "Входящий звонок: Аня" });
  await expect(ringing).toBeVisible();
  await ringing.getByRole("button", { name: "Принять" }).click();
  await expect(petr.page).toHaveURL(new RegExp(`/channels/@me/${dm.id}$`));
  await expect(voicePanel(petr)).toContainText("Голос подключён");
  await expect(ringing).toBeHidden();

  // Both stages show both people.
  for (const who of [anna, petr]) {
    const stage = who.page.getByRole("region", { name: "Звонок" });
    await expect(stage.getByText("Аня", { exact: true })).toBeVisible();
    await expect(stage.getByText("Пётр", { exact: true })).toBeVisible();
  }

  // Anna hangs up: Petr stays, and his DM shows the call from outside for her.
  await voicePanel(anna).getByRole("button", { name: "Отключиться" }).click();
  await expect(voicePanel(anna)).toBeHidden();
  await expect(anna.page.getByRole("region", { name: "Идёт звонок" })).toContainText("Пётр");
  await voicePanel(petr).getByRole("button", { name: "Отключиться" }).click();
  await expect(anna.page.getByRole("region", { name: "Идёт звонок" })).toBeHidden();
});

test("a declined call stops ringing", async ({ browser, people }) => {
  const anna = await signUp(browser, "Аня");
  const petr = await signUp(browser, "Пётр");
  people.push(anna, petr);
  await befriend(anna, petr);
  const dm = await call<{ id: string }>(anna, "POST", "/users/@me/channels", { recipient_id: petr.id });
  await petr.page.goto("/channels/@me");
  await anna.page.goto(`/channels/@me/${dm.id}`);
  await anna.page.getByRole("button", { name: "Начать звонок" }).click();
  const ringing = petr.page.getByRole("alertdialog", { name: "Входящий звонок: Аня" });
  await ringing.getByRole("button", { name: "Отклонить" }).click();
  await expect(ringing).toBeHidden();
  // Anna is still in her call, alone.
  await expect(voicePanel(anna)).toContainText("Голос подключён");
});
