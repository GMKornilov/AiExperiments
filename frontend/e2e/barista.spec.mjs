import { expect, test } from "@playwright/test";

const unique = (prefix) => `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2)}`;

async function createDialog(page) {
  await page.goto("/");
  await page.getByRole("button", { name: /новый диалог/i }).first().click();
  await expect(page.getByLabel("Ваш вопрос")).toBeVisible();
  return (await page.locator("main").getByText(/^ID:/).first().textContent()).replace("ID: ", "").trim();
}

async function sendAndSettle(page, text) {
  await page.getByLabel("Ваш вопрос").fill(text);
  await page.getByRole("button", { name: "Отправить" }).click();
  await expect(page.getByRole("region", { name: "Переписка" }).getByText(text, { exact: true })).toBeVisible();
  await expect(page.getByText("Бариста готовит ответ…")).toHaveCount(0, { timeout: 15_000 });
}

test("empty, create, send, copy, long overflow and refresh", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText("Чашка ждёт вопроса")).toBeVisible();
  await createDialog(page);
  const question = `${unique("overflow")} ${"длинный-текст ".repeat(250)}`;
  await sendAndSettle(page, question);
  await expect(page.getByRole("button", { name: "Копировать сообщение" }).first()).toBeVisible();
  await page.getByRole("button", { name: "Копировать сообщение" }).first().click();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await page.reload();
  await expect(page.getByRole("region", { name: "Переписка" }).getByText(question, { exact: true })).toBeVisible();
});

test("error is retried once without a second user bubble", async ({ page }) => {
  await createDialog(page);
  const question = unique("fail-retry");
  await page.getByLabel("Ваш вопрос").fill(question);
  await page.getByRole("button", { name: "Отправить" }).click();
  await expect(page.getByRole("button", { name: "Повторить отправку" })).toBeVisible({ timeout: 15_000 });
  await page.getByRole("button", { name: "Повторить отправку" }).click();
  await expect(page.getByText("Бариста готовит ответ…")).toHaveCount(0, { timeout: 15_000 });
  await expect(page.getByRole("region", { name: "Переписка" }).getByText(question, { exact: true })).toHaveCount(1);
});

test("two browser sessions are isolated and admin polling does not append rows", async ({ browser }) => {
  const first = await browser.newContext();
  const second = await browser.newContext();
  const page = await first.newPage();
  const dialogID = await createDialog(page);
  const question = unique("session-isolation");
  await sendAndSettle(page, question);
  const other = await second.newPage();
  await other.goto("/");
  await expect(other.getByText(question)).toHaveCount(0);
  await page.goto("/admin");
  await page.getByLabel("ID диалога").fill(dialogID);
  await page.getByRole("button", { name: "Найти" }).click();
  await expect(page.getByText("Frontend / BFF")).toBeVisible();
  await page.waitForTimeout(5500);
  const baseline = await page.locator("ol li").count();
  await page.waitForTimeout(5500);
  await expect(page.locator("ol li")).toHaveCount(baseline);
  await first.close();
  await second.close();
});

test("320px sidebar, cancel delete, then delete pending dialog without resurrection", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });
  await createDialog(page);
  await page.getByRole("button", { name: "Диалоги" }).click();
  await page.getByRole("button", { name: /удалить диалог/i }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page.getByRole("button", { name: "Отмена" }).click();
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  await page.getByLabel("Ваш вопрос").fill(unique("slow-delete"));
  await page.getByRole("button", { name: "Отправить" }).click();
  await expect(page.getByText("Бариста готовит ответ…")).toBeVisible();
  await page.getByRole("button", { name: /удалить диалог/i }).click();
  await page.getByRole("button", { name: "Удалить", exact: true }).click();
  await expect(page.getByText("Чашка ждёт вопроса")).toBeVisible();
  await page.waitForTimeout(8500);
  await expect(page.getByText("Чашка ждёт вопроса")).toBeVisible();
});
