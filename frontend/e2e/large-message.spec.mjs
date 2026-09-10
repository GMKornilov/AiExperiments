import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";

test("14 MB prompt: paste, send intact, bounded display and context error", async ({ page }) => {
  const text = readFileSync(new URL("../../docs/deepseek-overflow-prompt.txt", import.meta.url), "utf8").trim();
  const dialog = { id: "large", title: "Большой промпт", title_status: "success", created_at: "2026-09-11T00:00:00Z", updated_at: "2026-09-11T00:00:00Z", messages: [], accounted_tokens: 0 };
  let received = "";
  await page.route("**/api/**", async (route) => {
    if (route.request().url().endsWith("/messages")) {
      received = route.request().postDataJSON().text;
      dialog.messages = [{ id: "m1", role: "user", text: received, status: "error", error_category: "context_limit", created_at: dialog.created_at }];
      return route.fulfill({ json: dialog });
    }
    return route.fulfill({ json: { dialogs: [dialog], selected_dialog_id: dialog.id } });
  });
  await page.goto("/");
  await expect(page.getByLabel("Ваш вопрос")).toBeEnabled();
  const started = Date.now();
  await page.getByLabel("Ваш вопрос").evaluate((element, text) => {
    const clipboardData = new DataTransfer();
    clipboardData.setData("text/plain", text);
    element.dispatchEvent(new ClipboardEvent("paste", { bubbles: true, cancelable: true, clipboardData }));
  }, text);
  await expect(page.getByRole("button", { name: "Очистить промпт" })).toBeVisible();
  expect((await page.getByLabel("Ваш вопрос").inputValue()).length).toBeLessThanOrEqual(1000);
  await page.getByRole("button", { name: "Отправить", exact: true }).click();
  await expect(page.getByText("Контекст диалога превышает лимит модели. Начните новый диалог.", { exact: true })).toBeVisible();
  expect(received === text).toBe(true);
  expect(await page.getByRole("region", { name: "Переписка" }).evaluate((element) => element.textContent.length)).toBeLessThan(5000);
  await page.getByRole("button", { name: "Развернуть", exact: true }).click();
  expect(await page.getByRole("region", { name: "Переписка" }).evaluate((element) => element.textContent.length)).toBeLessThan(25000);
  await page.getByRole("button", { name: "Следующий фрагмент" }).click();
  await expect(page.getByText(/Фрагмент 2 из/)).toBeVisible();
  await page.getByRole("button", { name: "Свернуть", exact: true }).click();
  await page.getByRole("button", { name: "Копировать сообщение" }).click();
  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(copied.replace(/\r\n/g, "\n") === text.replace(/\r\n/g, "\n")).toBe(true);
  console.log(`14 MB paste/send/expand/page/collapse/copy: ${Date.now() - started} ms`);
});
