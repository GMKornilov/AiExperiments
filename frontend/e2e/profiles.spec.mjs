import { expect, test } from "@playwright/test";

const unique = () => `Домашний ${Date.now()}-${Math.random().toString(16).slice(2)}`;

async function openProfiles(page) {
  await page.goto("/");
  const menu = page.getByRole("button", { name: "Проекты" });
  if (await menu.isVisible()) await menu.click();
  await page.getByRole("button", { name: "Профили" }).click();
  await expect(page.getByRole("dialog", { name: "Профили ассистента" })).toBeVisible();
}

test("built-in profiles, custom creation, selection and active deletion", async ({ page }) => {
  await openProfiles(page);
  const dialog = page.getByRole("dialog", { name: "Профили ассистента" });
  await expect(dialog.getByRole("heading", { name: "Бариста" })).toBeVisible();
  await expect(dialog.getByRole("heading", { name: "Специалист по кофейному оборудованию" })).toBeVisible();
  await expect(dialog.getByText("Активен:")).toContainText("Бариста");
  await expect(dialog.getByText("Встроенный")).toHaveCount(2);
  expect(await dialog.getByRole("button", { name: "Удалить" }).count()).toBe(0);

  const equipment = dialog.getByRole("article").filter({ hasText: "Специалист по кофейному оборудованию" });
  await equipment.getByRole("button", { name: "Выбрать" }).click();
  await expect(dialog.getByText("Активен:")).toContainText("Специалист по кофейному оборудованию");
  await page.reload();
  await page.getByRole("button", { name: "Профили" }).click();
  await expect(dialog.getByText("Активен:")).toContainText("Специалист по кофейному оборудованию");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();

  await dialog.getByLabel("Название").fill(unique());
  await dialog.getByLabel("Стиль").fill("Коротко и по шагам");
  await dialog.getByLabel("Ограничения").fill("Не предлагай молоко");
  await dialog.getByLabel("Дополнительный контекст").fill("Дома есть V60");
  await dialog.getByRole("button", { name: "Создать и выбрать" }).click();
  await expect(dialog.getByText("Активен:")).toContainText("Домашний");
  const custom = dialog.getByRole("article").filter({ hasText: "Дома есть V60" });
  await custom.getByRole("button", { name: "Удалить" }).click();
  const confirm = dialog.getByRole("alertdialog");
  await expect(confirm).toContainText("Активным сразу станет профиль «Бариста».");
  await confirm.getByRole("button", { name: "Удалить" }).click();
  await expect(dialog.getByText("Активен:")).toContainText("Бариста");
});

test("validation, keyboard controls and 390px overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openProfiles(page);
  const dialog = page.getByRole("dialog", { name: "Профили ассистента" });
  await dialog.getByRole("button", { name: "Создать и выбрать" }).focus();
  await page.keyboard.press("Enter");
  await expect(dialog.getByRole("alert")).toContainText("Введите название профиля.");
  await dialog.getByRole("button", { name: "Закрыть профили" }).focus();
  await page.keyboard.press("Enter");
  await expect(dialog).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
});
