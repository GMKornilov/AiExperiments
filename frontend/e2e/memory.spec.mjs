import { expect, test } from "@playwright/test";

test("initial profile request uses the established project session", async ({ page }) => {
  const profileRequest = page.waitForRequest(request => new URL(request.url()).pathname === "/api/profiles" && request.method() === "GET");
  await page.goto("/");
  const request = await profileRequest;
  expect(await request.headerValue("cookie")).toMatch(/barista_session=[a-f0-9-]{36}/i);
  await page.getByRole("button", { name: /новый проект/i }).first().click();
  await page.getByRole("button", { name: "Создать чат" }).click();
  await expect(page.getByLabel("Ваш вопрос")).toBeVisible();
});

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
  await expect(panel.getByText("Есть зёрна: Эфиопия")).toHaveCount(0);
});

for (const trigger of ["memory-error", "memory-invalid"]) {
  test(`task extractor ${trigger} rejects output and preserves prior snapshots`, async ({ page }) => {
    await newProjectWithChat(page);
    await sendAndWait(page, "У меня есть V60 и зёрна Эфиопия");
    await page.getByLabel("Ваш вопрос").fill(`${trigger} новая реплика`);
    await page.getByRole("button", { name: "Отправить" }).click();
    await expect(page.getByRole("button", { name: "Повторить", exact: true })).toBeVisible();
    await expect(page.getByText(`Ответ: ${trigger} новая реплика [e2e-fixture-model]`)).toHaveCount(0);
    const panel = await openMemory(page);
    await expect(panel.getByText("Оборудование: V60")).toBeVisible();
    await expect(panel.getByText("Есть зёрна: Эфиопия")).toBeVisible();
    await page.reload();
    await expect(page.getByText(`Ответ: ${trigger} новая реплика [e2e-fixture-model]`)).toHaveCount(0);
    const refreshed = await openMemory(page);
    await expect(refreshed.getByText("Оборудование: V60")).toBeVisible();
    await expect(refreshed.getByText("Есть зёрна: Эфиопия")).toBeVisible();
  });
}

test("chat deletion preserves explicitly project-scoped memory; deleting another project leaves global memory", async ({ page }) => {
  await newProjectWithChat(page);
  await sendAndWait(page, "У меня есть V60 и зёрна Эфиопия");
  await sendAndWait(page, "Только для этого проекта есть зёрна Кения");
  await page.getByRole("button", { name: /Удалить чат/i }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Удалить" }).click();
  await expect(page.getByRole("button", { name: "Создать чат" })).toBeVisible();
  await page.getByRole("button", { name: "Создать чат" }).click();
  let panel = await openMemory(page);
  await expect(panel.getByText("Есть зёрна: Кения")).toBeVisible();
  await page.getByRole("button", { name: /Новый проект/i }).first().click();
  await page.getByRole("button", { name: "Создать чат" }).click();
  panel = await openMemory(page);
  await expect(panel.getByText("Оборудование: V60")).toBeVisible();
  await expect(panel.getByText("Есть зёрна: Эфиопия")).toBeVisible();
  await expect(panel.getByText("Есть зёрна: Кения")).toHaveCount(0);
  await page.getByRole("button", { name: /Удалить проект/i }).first().click();
  await page.getByRole("dialog").getByRole("button", { name: "Удалить" }).click();
  panel = await openMemory(page);
  await expect(panel.getByText("Оборудование: V60")).toBeVisible();
});

test("панель инвариантов доступна read-only, а pre-conflict создаёт refusal и сохраняет negative global fact", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await newProjectWithChat(page);
  const toggle = page.getByRole("button", { name: "Инварианты" });
  await toggle.focus(); await page.keyboard.press("Enter");
  const panel = page.getByRole("complementary", { name: "Инварианты" });
  await expect(panel.getByRole("heading", { name: "Инварианты" })).toBeVisible();
  await expect(panel.getByRole("heading", { name: "Доступность оборудования" })).toBeVisible();
  await expect(panel.getByRole("heading", { name: "Доступность зёрен" })).toBeVisible();
  await expect(panel.getByRole("heading", { name: "Достоверность инвентаря" })).toBeVisible();
  await expect(panel.getByRole("button")).toHaveCount(0);
  expect(await toggle.evaluate((element) => element.getBoundingClientRect().height)).toBeGreaterThanOrEqual(44);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();

  await page.getByLabel("Ваш вопрос").fill("invariant-equipment-pre-conflict V60 сломан, сделай рецепт на V60");
  await page.getByRole("button", { name: "Отправить" }).click();
  await expect(page.getByText(/Доступность оборудования/).last()).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText("Ответ: invariant-equipment-pre-conflict V60 сломан, сделай рецепт на V60 [e2e-fixture-model]")).toHaveCount(0);
  const memory = await openMemory(page);
  await expect(memory.getByText("Оборудование: V60 — сломано")).toBeVisible();
  await expect(memory.getByText("Память обновлена")).toBeVisible();
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

test("copied chat ID opens admin journal; unknown and deleted chats are neutral", async ({ page }) => {
  await newProjectWithChat(page);
  await sendAndWait(page, "эспрессо admin smoke");
  const copy = page.getByRole("button", { name: "Копировать ID чата" });
  await copy.focus(); await page.keyboard.press("Enter");
  const id = await page.evaluate(() => navigator.clipboard.readText());
  expect(id).toMatch(/^[a-f0-9]+$/);
  const projects = await (await page.request.get("/api/projects")).json();
  expect(id).toBe(projects.selected_chat_id);
  const pid = projects.selected_project_id;
  await page.goto("/admin");
  await page.getByLabel("ID чата", { exact: true }).fill(id);
  await page.getByRole("button", { name: "Найти", exact: true }).click();
  await expect(page.getByText("task_step", { exact: true }).first()).toBeVisible();
  await page.getByLabel("ID чата", { exact: true }).fill("unknown-smoke-chat");
  await page.getByRole("button", { name: "Найти", exact: true }).click();
  await expect(page.getByText("Данные не найдены.", { exact: true })).toBeVisible();
  expect((await page.request.delete(`/api/projects/${pid}/chats/${id}`)).status()).toBe(204);
  await page.getByLabel("ID чата", { exact: true }).fill(id);
  await page.getByRole("button", { name: "Найти", exact: true }).click();
  await expect(page.getByText("Данные не найдены.", { exact: true })).toBeVisible();
});
