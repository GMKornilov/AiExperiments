import { expect, test } from "@playwright/test";

async function create(page) {
  await page.goto("/");
  await page.getByRole("button", { name: /новый диалог/i }).first().click();
  await page.getByTestId("strategy-card-sliding_window").click();
  await total(page, 0);
}

async function send(page, text) {
  await page.getByLabel("Ваш вопрос").fill(text);
  const response = page.waitForResponse((candidate) => candidate.url().endsWith("/messages") && candidate.request().method() === "POST");
  await page.getByRole("button", { name: "Отправить", exact: true }).click();
  const result = await response;
  expect(result.ok()).toBeTruthy();
  return result.json();
}

const total = (page, number) => expect(page.getByText(new RegExp(`Chat: ${number.toLocaleString("ru-RU")} токенов`))).toBeVisible();
const row = (page, input, output) => page.getByText(`Вход: ${input} токенов · Выход: ${output} токенов`, { exact: true }).last();

test("short and long dialog usage grows, title excluded, refresh preserves totals", async ({ page }) => {
  await create(page);
  let accounted = 0;
  const inputs = [];
  for (let step = 1; step <= 8; step += 1) {
    const dialog = await send(page, `tokens-growth-${step}`);
    const usage = dialog.messages.at(-1)?.usage;
    expect(usage).toEqual({ prompt_tokens: expect.any(Number), completion_tokens: expect.any(Number) });
    expect(usage.prompt_tokens).toBeGreaterThan(0);
    expect(usage.completion_tokens).toBeGreaterThan(0);
    inputs.push(usage.prompt_tokens);
    accounted += usage.prompt_tokens + usage.completion_tokens;
    await expect(row(page, usage.prompt_tokens, usage.completion_tokens)).toBeVisible();
    expect(dialog.accounted_tokens).toBe(accounted);
    await total(page, accounted);
  }
  expect(inputs.slice(0, 3)).toEqual([100, 150, 200]);
  expect(Math.max(...inputs.slice(3))).toBeLessThanOrEqual(300);
  expect(inputs.at(-1)).toBe(300);
  await page.reload();
  await total(page, accounted);
  await expect(page.getByRole("region", { name: "Переписка" }).locator("article")).toHaveCount(16);
  const originalID = (await page.getByRole("region", { name: "Статус диалога" }).locator("code").textContent()).trim();
  await page.getByRole("button", { name: /новый диалог/i }).first().click();
  await page.getByTestId("strategy-card-sliding_window").click();
  await total(page, 0);
  await page.getByRole("navigation").getByRole("button", { name: new RegExp(originalID) }).click();
  await total(page, accounted);
  await page.screenshot({ path: "test-results/token-desktop.png", fullPage: true });
  await send(page, "tokens-overflow");
  await expect(page.getByRole("button", { name: "Повторить отправку" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Переписка" }).getByText(/контекст/i)).toBeVisible();
  await total(page, accounted);
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
  const id = (await page.getByRole("region", { name: "Статус диалога" }).locator("code").textContent()).trim();
  await page.goto("/admin");
  await page.getByLabel("ID диалога").fill(id);
  await page.getByRole("button", { name: "Найти" }).click();
  await page.getByRole("checkbox", { name: "Только LLM" }).uncheck();
  await expect(page.locator("ol li").first()).toBeVisible();
  await expect(page.getByText(/context_limit/).first()).toBeVisible();
  const logResponse = await page.request.get(`/api/admin/logs?dialog_id=${id}&action=poll`);
  expect(logResponse.ok()).toBeTruthy();
  const journal = await logResponse.json();
  expect(journal.logs.filter((entry) => entry.event === "llm_start")).toHaveLength(2);
});
