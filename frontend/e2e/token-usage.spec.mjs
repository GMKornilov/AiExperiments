import { expect, test } from "@playwright/test";

async function create(page) {
  await page.goto("/");
  await page.getByRole("button", { name: /новый диалог/i }).first().click();
  await expect(page.getByText("Учтено токенов: 0", { exact: true })).toBeVisible();
}

async function send(page, text) {
  await page.getByLabel("Ваш вопрос").fill(text);
  await page.getByRole("button", { name: "Отправить", exact: true }).click();
}

const total = (page, number) => expect(page.getByText(`Учтено токенов: ${number}`, { exact: true })).toBeVisible();
const row = (page, input, output) => page.getByText(`Вход: ${input} токенов · Выход: ${output} токенов`, { exact: true });

test("short and long dialog usage grows, title excluded, refresh preserves totals", async ({ page }) => {
  await create(page);
  for (let step = 1; step <= 8; step += 1) {
    await send(page, `tokens-growth-${step}`);
    await expect(row(page, 50 + 50 * step, 10 + 10 * step)).toBeVisible();
    // First two steps: 120 then 300; eight completed steps: 2640.
    await total(page, 30 * step * (step + 3));
  }
  await page.reload();
  await total(page, 2640);
  await expect(page.getByRole("region", { name: "Переписка" }).locator("article")).toHaveCount(16);
  const originalID = (await page.locator("main").getByText(/^ID:/).first().textContent()).replace("ID: ", "").trim();
  await page.getByRole("button", { name: /новый диалог/i }).first().click();
  await total(page, 0);
  await page.getByRole("navigation").getByRole("button", { name: new RegExp(originalID) }).click();
  await total(page, 2640);
  await page.screenshot({ path: "test-results/token-desktop.png", fullPage: true });
  await send(page, "tokens-growth-9");
  await expect(page.getByRole("button", { name: "Повторить отправку" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Переписка" }).getByText(/контекст/i)).toBeVisible();
  await total(page, 2640);
  await page.setViewportSize({ width: 320, height: 720 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await page.screenshot({ path: "test-results/token-mobile.png", fullPage: true });
});

test("missing, invalid and zero usage remain distinct without warnings", async ({ page }) => {
  await create(page);
  await send(page, "tokens-missing");
  await expect(page.getByText("Токены: нет данных", { exact: true })).toHaveCount(1);
  await send(page, "tokens-invalid");
  await expect(page.getByText("Токены: нет данных", { exact: true })).toHaveCount(2);
  await send(page, "tokens-zero");
  await expect(row(page, 0, 0)).toBeVisible();
  await total(page, 0);
  await expect(page.getByText(/неполн/i)).toHaveCount(0);
});

test("failed response retains confirmed usage and manual retry adds once", async ({ page }) => {
  await create(page);
  const question = `tokens-empty-retry-${Date.now()}`;
  await send(page, question);
  await expect(page.getByRole("button", { name: "Повторить отправку" })).toBeVisible();
  await total(page, 120);
  await page.getByRole("button", { name: "Повторить отправку" }).click();
  await expect(row(page, 100, 20)).toBeVisible();
  await total(page, 240);
  await page.reload();
  await total(page, 240);
  await expect(page.getByRole("region", { name: "Переписка" }).getByText(question, { exact: true })).toHaveCount(1);
});

test("simulated context overflow keeps history and confirmed total without auto retry", async ({ page }) => {
  await create(page);
  await send(page, "tokens-short-before-overflow");
  await expect(row(page, 100, 20)).toBeVisible();
  await send(page, "tokens-overflow");
  await expect(page.getByRole("button", { name: "Повторить отправку" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Переписка" }).getByText(/контекст/i)).toBeVisible();
  await total(page, 120);
  await expect(page.getByLabel("Ваш вопрос")).toBeDisabled();
  await page.reload();
  await total(page, 120);
  await expect(page.getByRole("region", { name: "Переписка" }).locator("article")).toHaveCount(3);
  const id = (await page.locator("main").getByText(/^ID:/).first().textContent()).replace("ID: ", "").trim();
  await page.goto("/admin");
  await page.getByLabel("ID диалога").fill(id);
  await page.getByRole("button", { name: "Найти" }).click();
  await expect(page.getByText("Frontend / BFF")).toBeVisible();
  await expect(page.getByText(/context_limit/).first()).toBeVisible();
  const logResponse = await page.request.get(`/api/admin/logs?dialog_id=${id}&action=poll`);
  expect(logResponse.ok()).toBeTruthy();
  const journal = await logResponse.json();
  expect(journal.logs.filter((entry) => entry.event === "llm_start")).toHaveLength(2);
});
