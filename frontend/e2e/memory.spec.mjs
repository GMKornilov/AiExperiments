import { expect, test } from "@playwright/test";

async function newProjectWithChat(page) {
  await page.goto("/");
  await page.getByRole("button", { name: /новый проект/i }).first().click();
  await expect(page.getByRole("button", { name: "Создать чат" })).toBeEnabled();
  await page.getByRole("button", { name: "Создать чат" }).click();
  await expect(page.getByLabel("Ваш вопрос")).toBeVisible();
}

async function sendAndWait(page, text) {
  await page.getByLabel("Ваш вопрос").fill(text);
  await page.getByRole("button", { name: "Отправить" }).click();
  await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`)).toBeVisible({ timeout: 15_000 });
}

async function openMemory(page) {
  const button = page.getByRole("button", { name: "Память" });
  if ((await button.getAttribute("aria-expanded")) !== "true") await button.click();
  return page.getByRole("complementary", { name: "Память" });
}

test("response waits for extractor; facts persist through refresh and layer clear is isolated", async ({ page }) => {
  await newProjectWithChat(page);
  const text = "memory-slow У меня есть V60 и зёрна Эфиопия";
  await page.getByLabel("Ваш вопрос").fill(text);
  await page.getByRole("button", { name: "Отправить" }).click();
  await expect(page.getByText(/обновляет память/i)).toBeVisible();
  await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`)).toHaveCount(0);
  await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`)).toBeVisible({ timeout: 15_000 });
  let panel = await openMemory(page);
  await expect(panel.getByText("Оборудование: V60")).toBeVisible();
  await expect(panel.getByText("Есть зёрна: Эфиопия")).toBeVisible();
  await page.reload();
  panel = await openMemory(page);
  await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`)).toBeVisible();
  await expect(panel.getByText("Оборудование: V60")).toBeVisible();
  await expect(panel.getByText("Есть зёрна: Эфиопия")).toBeVisible();
  await panel.getByRole("heading", { name: "Общая память" }).locator("..").getByRole("button", { name: "Очистить" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Очистить" }).click();
  await expect(panel.getByText("Фактов пока нет.").first()).toBeVisible();
  await expect(panel.getByText("Есть зёрна: Эфиопия")).toBeVisible();
});

for (const trigger of ["memory-error", "memory-invalid"]) {
  test(`extractor ${trigger} preserves pair and prior snapshots`, async ({ page }) => {
    await newProjectWithChat(page);
    await sendAndWait(page, "У меня есть V60 и зёрна Эфиопия");
    await sendAndWait(page, `${trigger} новая реплика`);
    const panel = await openMemory(page);
    await expect(panel.getByText("Не удалось обновить память; ответ сохранён.")).toBeVisible();
    await expect(panel.getByText("Оборудование: V60")).toBeVisible();
    await expect(panel.getByText("Есть зёрна: Эфиопия")).toBeVisible();
    await page.reload();
    await expect(page.getByText(`Ответ: ${trigger} новая реплика [e2e-fixture-model]`)).toBeVisible();
    const refreshed = await openMemory(page);
    await expect(refreshed.getByText("Не удалось обновить память; ответ сохранён.")).toBeVisible();
    await expect(refreshed.getByText("Оборудование: V60")).toBeVisible();
    await expect(refreshed.getByText("Есть зёрна: Эфиопия")).toBeVisible();
  });
}

test("chat deletion preserves project memory; deleting another project leaves global memory", async ({ page }) => {
  await newProjectWithChat(page);
  await sendAndWait(page, "У меня есть V60 и зёрна Эфиопия");
  await page.getByRole("button", { name: /Удалить чат/i }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Удалить" }).click();
  await expect(page.getByRole("button", { name: "Создать чат" })).toBeVisible();
  await page.getByRole("button", { name: "Создать чат" }).click();
  let panel = await openMemory(page);
  await expect(panel.getByText("Есть зёрна: Эфиопия")).toBeVisible();
  await page.getByRole("button", { name: /Новый проект/i }).first().click();
  await page.getByRole("button", { name: "Создать чат" }).click();
  panel = await openMemory(page);
  await expect(panel.getByText("Оборудование: V60")).toBeVisible();
  await expect(panel.getByText("Есть зёрна: Эфиопия")).toHaveCount(0);
  await page.getByRole("button", { name: /Удалить проект/i }).first().click();
  await page.getByRole("dialog").getByRole("button", { name: "Удалить" }).click();
  panel = await openMemory(page);
  await expect(panel.getByText("Оборудование: V60")).toBeVisible();
});

test("another browser context is isolated", async ({ browser }) => {
  const first = await browser.newContext(); const second = await browser.newContext();
  const page = await first.newPage(); await newProjectWithChat(page); await sendAndWait(page, "У меня есть V60");
  const other = await second.newPage(); await other.goto("/");
  await expect(other.getByRole("heading", { name: "Создайте проект" })).toBeVisible();
  await first.close(); await second.close();
});

test("390px keyboard smoke reaches project, memory and confirmation controls", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 }); await page.goto("/");
  const project = page.getByRole("button", { name: /новый проект/i }).first(); await project.focus(); await page.keyboard.press("Enter");
  const chat = page.getByRole("button", { name: "Создать чат" }); await expect(chat).toBeEnabled(); await chat.focus(); await page.keyboard.press("Enter");
  const memory = page.getByRole("button", { name: "Память" }); await memory.focus(); await page.keyboard.press("Enter");
  const panel = page.getByRole("complementary", { name: "Память" }); await expect(panel).toBeVisible();
  const clear = panel.getByRole("button", { name: "Очистить" }).first(); await clear.focus(); await page.keyboard.press("Enter");
  const cancel = page.getByRole("dialog").getByRole("button", { name: "Отмена" }); await cancel.focus(); await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
});

test("Enter sends, Shift+Enter keeps a newline, and project rename persists", async ({ page }) => {
  await newProjectWithChat(page);
  const input = page.getByLabel("Ваш вопрос");
  await input.fill("первая строка"); await input.press("Shift+Enter"); await input.type("вторая строка");
  await expect(input).toHaveValue("первая строка\nвторая строка"); await input.press("Enter");
  await expect(page.getByText("Ответ: первая строка\nвторая строка [e2e-fixture-model]")).toBeVisible({ timeout: 15_000 });
  await page.getByRole("button", { name: /Переименовать проект/i }).click();
  const title = page.getByLabel("Название проекта"); await title.fill("Домашняя кофейня"); await page.getByRole("button", { name: "Сохранить" }).click();
  await expect(page.getByRole("button", { name: "Домашняя кофейня", exact: true })).toBeVisible();
  await page.reload(); await expect(page.getByRole("button", { name: "Домашняя кофейня", exact: true })).toBeVisible();
});

test("title is refreshed in the background after the response", async ({ page }) => {
  await newProjectWithChat(page);
  await sendAndWait(page, "title-slow назови этот чат");
  await expect(page.getByRole("heading", { name: "Название диалога" })).toBeVisible({ timeout: 12_000 });
});
