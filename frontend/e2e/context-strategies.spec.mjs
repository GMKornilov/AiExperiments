import { expect, test } from "@playwright/test";

async function chooseStrategy(page, strategy) {
  await expect(page.getByTestId("strategy-picker")).toBeVisible();
  await page.getByTestId(`strategy-card-${strategy}`).click();
  await expect(page.getByTestId("context-strategy-status")).toContainText({
    sliding_window: "Sliding Window", facts: "Facts", branching: "Branching", summary: "Summary",
  }[strategy]);
  await expect(page.getByLabel("Ваш вопрос")).toBeEnabled();
}

async function send(page, text) {
  await page.getByLabel("Ваш вопрос").fill(text);
  await page.getByRole("button", { name: "Отправить", exact: true }).click();
  await expect(page.getByText("Бариста готовит ответ…")).toHaveCount(0, { timeout: 15_000 });
  await expect(page.getByRole("region", { name: "Переписка" }).getByText(text, { exact: true })).toBeVisible();
}

async function dialogID(page) {
  const id = await page.getByRole("region", { name: "Статус диалога" }).locator("code").textContent();
  if (!id) throw new Error("dialog ID is missing");
  return id;
}

test("strategy is explicit, immutable after the first message, and compact rejects Sliding Window", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("strategy-picker")).toBeVisible();
  await expect(page.getByLabel("Ваш вопрос")).toHaveCount(0);
  await chooseStrategy(page, "sliding_window");
  await send(page, "explicit strategy input");
  await expect(page.getByTestId("strategy-picker")).toHaveCount(0);

  const id = await dialogID(page);
  const strategy = await page.request.patch(`/api/dialogs/${id}/strategy`, { data: { context_strategy: "facts" } });
  expect(strategy.status()).toBe(400);
  const compact = await page.request.post(`/api/dialogs/${id}/compact`);
  expect(compact.status()).toBe(400);
  await expect(page.getByLabel("Ваш вопрос")).toBeEnabled();
});

test("Facts shows the persisted extractor snapshot and preserves a usable layout on mobile", async ({ page }) => {
  await page.goto("/");
  await chooseStrategy(page, "facts");
  await send(page, "доза теперь 17 г");
  const facts = page.getByTestId("facts-panel");
  await expect(facts).toBeVisible();
  await facts.locator("summary").click();
  await expect(facts).toContainText("dose");
  await expect(facts).toContainText("17 г");
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test("Branching creates an isolated child and switching restores its own line", async ({ page }) => {
  await page.goto("/");
  await chooseStrategy(page, "branching");
  await send(page, "common branch prefix");
  await expect(page.getByTestId("add-branch")).toBeEnabled();
  await page.getByTestId("add-branch").click();
  const branches = page.locator('[data-testid^="branch-"]');
  await expect(branches).toHaveCount(2);
  const root = branches.filter({ hasText: "Основная ветка" });
  await send(page, "child-only message");
  await root.click();
  await expect(page.getByText("child-only message", { exact: true })).toHaveCount(0);
  await expect(page.getByText("common branch prefix", { exact: true })).toBeVisible();
});

test("Summary alone exposes slash compact", async ({ page }) => {
  await page.goto("/");
  await chooseStrategy(page, "summary");
  await send(page, "summary first");
  await send(page, "summary second");
  await page.getByLabel("Ваш вопрос").fill("/");
  const compact = page.getByRole("button", { name: /compact/i });
  await expect(compact).toBeVisible();
  await compact.click();
  await expect(page.getByText(/Контекст сжат:|Summary обновлено/)).toBeVisible();
});
