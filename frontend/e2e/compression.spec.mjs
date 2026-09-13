import { test, expect } from "@playwright/test";

test("compression switches context, persists after reload, and displays actual input", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Новый диалог", exact: true }).click();
  const toggle = page.getByRole("switch", { name: /Сжатие истории/ });
  await expect(toggle).toBeEnabled();
  await toggle.click();
  await expect(toggle).toBeChecked();
  const send = async (text) => {
    await page.getByRole("textbox").fill(text);
    const response = page.waitForResponse(r => r.url().endsWith("/messages") && r.request().method() === "POST");
    await page.getByRole("button", { name: "Отправить", exact: true }).click();
    const result = await response;
    expect(result.ok()).toBeTruthy();
    return result.json();
  };
  await send("Кофе без молока, доза 18 г. " + "Расскажи о зерне. ".repeat(40));
  await send("Какой помол подходит? " + "Объясни детали. ".repeat(40));
  const compressed = await send("Как улучшить рецепт?");
  expect(compressed.compression.covered_messages).toBe(2);
  expect(compressed.messages).toHaveLength(6);
  expect(compressed.compression.pruned_messages).toBe(2);
  expect(JSON.stringify(compressed.messages)).toContain("Расскажи о зерне.");
  await expect(page.getByText(/^Кофе без молока, доза 18 г[.]/)).toBeVisible();
  expect(compressed.compression.sent_estimate).toBeLessThan(compressed.compression.full_estimate);
  console.log("Fixture comparison:", JSON.stringify({
    full_estimate: compressed.compression.full_estimate,
    sent_estimate: compressed.compression.sent_estimate,
    summary_tokens: compressed.compression.summary_tokens,
    input_tokens: compressed.compression.last_input_tokens,
  }));
  const meter = page.getByRole("progressbar");
  await expect(meter).toHaveAttribute("aria-valuenow", String(compressed.compression.last_input_tokens));
  await expect(meter).toHaveAttribute("aria-valuemax", "1000");
  await page.reload();
  await expect(toggle).toBeChecked();
  await expect(page.getByText(/^Кофе без молока, доза 18 г[.]/)).toBeVisible();
  await page.getByText("Посмотреть summary", { exact: true }).click();
  await expect(page.getByText("Пользователь предпочитает кофе без молока; доза 18 г.", { exact: true })).toBeVisible();
  await toggle.click();
  await expect(toggle).not.toBeChecked();
  const full = await send("Повтори мои предпочтения.");
  expect(full.compression.enabled).toBe(false);
  expect(full.compression.full_estimate).toBe(full.compression.sent_estimate);
  expect(full.messages).toHaveLength(8);
  await expect(meter).toHaveAttribute("aria-valuenow", String(full.compression.last_input_tokens));
  await page.screenshot({ path: "test-results/compression-desktop.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(meter).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: "test-results/compression-mobile.png", fullPage: true });
});
