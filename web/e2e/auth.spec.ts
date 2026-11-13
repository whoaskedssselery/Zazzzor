import { randomUUID } from "node:crypto";
import { expect, newVisitor, test, uniqueLogin } from "./fixtures";

test("signs up, signs out and signs back in", async ({ browser }) => {
  const context = await newVisitor(browser);
  const page = await context.newPage();
  const login = uniqueLogin("auth");
  const password = randomUUID();

  await page.goto("/register");
  await page.getByLabel("Отображаемое имя").fill("Аня");
  await page.getByLabel("Логин").fill(login);
  await page.getByLabel("Пароль").fill(password);
  await page.getByRole("button", { name: "Зарегистрироваться" }).click();
  await expect(page).toHaveURL(/\/channels\/@me$/);
  const panel = page.getByRole("region", { name: "Ваш профиль" });
  await expect(panel.getByText("Аня")).toBeVisible();

  // Signing out lives in the user settings, as in Discord, and asks first.
  await panel.getByRole("button", { name: "Настройки пользователя" }).click();
  await page.getByRole("button", { name: "Выйти" }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Выйти" }).click();
  await expect(page).toHaveURL(/\/login$/);

  await page.getByLabel("Логин").fill(login);
  await page.getByLabel("Пароль").fill(password);
  await page.getByRole("button", { name: "Войти" }).click();
  await expect(page).toHaveURL(/\/channels\/@me$/);
  await expect(panel.getByText("Аня")).toBeVisible();
  await context.close();
});

test("says the password is wrong", async ({ browser }) => {
  const context = await newVisitor(browser);
  const page = await context.newPage();
  await page.goto("/login");
  await page.getByLabel("Логин").fill(uniqueLogin("nobody"));
  await page.getByLabel("Пароль").fill("not-the-password");
  await page.getByRole("button", { name: "Войти" }).click();
  await expect(page.getByRole("alert")).toHaveText("Неверный логин или пароль.");
  await expect(page).toHaveURL(/\/login$/);
  await context.close();
});
