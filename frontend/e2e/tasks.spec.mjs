import { expect, test } from "@playwright/test";

async function setup(page, width = 1440) {
  await page.setViewportSize({ width, height: 900 });
  await page.goto("/");
  await page.getByRole("button", { name: /новый проект/i }).first().click();
  await page.getByRole("button", { name: "Создать чат" }).click();
  await page.getByRole("button", { name: /^Задачи/ }).click();
}
async function submit(page, text) {
  await page.getByLabel("Ваш вопрос").fill(text);
  await page.getByRole("button", { name: "Отправить" }).click();
  await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`, { exact: false }).last()).toBeVisible({ timeout: 15000 });
}
const panel = page => page.getByRole("region", { name: "Состояние задач" });

for (const width of [390, 1440]) {
  test(`task clarification, subject plan, feedback and keyboard at ${width}px`, async ({ page }) => {
    await setup(page, width);
    await submit(page, "Подобрать эспрессо");
    await expect(panel(page).getByText("План появится после подтверждения цели.")).toBeVisible();
    await expect(panel(page).getByText("Этап: Уточняем запрос")).toBeVisible();
    await submit(page, "same-stage Кофемолка Niche Zero, зёрна обжарены вчера");
    await expect(panel(page).getByText("Узнать информацию о кофемолке", { exact: true })).toBeVisible();
    await expect(panel(page).getByText("Задача ожидает вашу обратную связь.")).toBeVisible();
    await expect(panel(page).locator('[aria-current="step"]')).toHaveCount(0);
    await submit(page, "не подходит");
    await expect(panel(page).getByText("Скорректировать рецепт по отзыву")).toBeVisible();
    await expect(panel(page).getByText("Задача ожидает вашу обратную связь.")).toBeVisible();
    await submit(page, "спасибо");
    await expect(panel(page).getByText("Задача завершена.")).toBeVisible();
    await expect(page.getByRole("button", { name: "Продолжить", exact: true })).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
    await page.reload();
    await page.getByRole("button", { name: /^Задачи/ }).focus();
    await page.keyboard.press("Enter");
    await expect(panel(page).getByText("Задача завершена.")).toBeVisible();
  });
}

test("pause during an autonomous chain discards every uncommitted step and Resume repeats the chain", async ({ page }) => {
  await setup(page);
  await submit(page, "Подобрать эспрессо autonomous-pause");
  const text = "auto-slow Кофемолка Niche Zero, цель подтверждаю";
  await page.getByLabel("Ваш вопрос").fill(text);
  await page.getByRole("button", { name: "Отправить" }).click();
  await expect(page.getByRole("button", { name: "Остановить" })).toBeVisible();
  await page.waitForTimeout(1000);
  await page.getByRole("button", { name: "Остановить" }).click();
  await expect(page.getByRole("button", { name: "Продолжить", exact: true })).toBeEnabled({ timeout: 1000 });
  await expect(panel(page).getByText("План появится после подтверждения цели.")).toBeVisible();
  await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`, { exact: false })).toHaveCount(0);
  await page.waitForTimeout(1000);
  await expect(panel(page).getByText("Этап: Уточняем запрос")).toBeVisible();
  await page.getByRole("button", { name: "Продолжить", exact: true }).click();
  await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`, { exact: false }).last()).toBeVisible({ timeout: 15000 });
  await expect(panel(page).getByText("Задача ожидает вашу обратную связь.")).toBeVisible();
  await expect(panel(page).locator('[aria-current="step"]')).toHaveCount(0);
});

test("failed Resume stays paused across refresh and can be continued again", async ({ page }) => {
  await setup(page);
  await submit(page, "Подобрать эспрессо resume-retry");
  const text = "auto-slow resume-invalid Кофемолка Niche Zero, цель подтверждаю";
  await page.getByLabel("Ваш вопрос").fill(text);
  await page.getByRole("button", { name: "Отправить" }).click();
  await expect(page.getByRole("button", { name: "Остановить" })).toBeVisible();
  await page.waitForTimeout(1000);
  await page.getByRole("button", { name: "Остановить" }).click();
  await expect(page.getByRole("button", { name: "Продолжить", exact: true })).toBeEnabled({ timeout: 1000 });
  await page.getByRole("button", { name: "Продолжить", exact: true }).click();
  await expect(page.getByRole("button", { name: "Повторить продолжение" })).toBeVisible({ timeout: 5000 });
  await page.reload();
  await expect(page.getByRole("button", { name: "Продолжить", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: /^Задачи/ }).click();
  await expect(panel(page).getByText("На паузе", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Продолжить", exact: true }).click();
  await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`, { exact: false }).last()).toBeVisible({ timeout: 15000 });
  await expect(panel(page).getByText("Задача ожидает вашу обратную связь.")).toBeVisible();
});

for (const text of ["slow Подобрать эспрессо", "memory-slow Подобрать эспрессо"]) {
  test(`pause cancels ${text.split(" ")[0]} without output or retry alert; Resume repeats`, async ({ page }) => {
    await setup(page);
    await page.getByLabel("Ваш вопрос").fill(text);
    await page.evaluate(text => {
      window.pendingLatency = undefined;
      document.querySelector("#barista-message").closest("form").addEventListener("submit", () => {
        const started = performance.now();
        const region = document.querySelector('[aria-label="Переписка"]');
        const observer = new MutationObserver(() => {
          if (region.textContent.includes(text) && region.querySelector('[role="status"]')) {
            window.pendingLatency = performance.now() - started; observer.disconnect();
          }
        });
        observer.observe(region, { childList: true, subtree: true });
      }, { once: true });
    }, text);
    await page.getByRole("button", { name: "Отправить" }).click();
    await expect(page.getByText(text, { exact: true }).first()).toBeVisible();
    const latency = await page.evaluate(() => window.pendingLatency);
    expect(latency).toBeLessThanOrEqual(100);
    console.log(`pending latency: ${latency}ms`);
    await expect(page.getByRole("button", { name: "Остановить" })).toBeVisible();
    await page.getByRole("button", { name: "Остановить" }).click();
    await expect(page.getByRole("button", { name: "Продолжить", exact: true })).toBeEnabled({ timeout: 1000 });
    await expect(page.getByRole("alert").filter({ hasText: /\S/ })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Повторить", exact: true })).toHaveCount(0);
    await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`, { exact: true })).toHaveCount(0);
    await page.getByRole("button", { name: "Продолжить", exact: true }).click();
    await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`, { exact: true })).toBeVisible({ timeout: 15000 });
    // One server pair replaces the single neutral optimistic input.
    const messages = page.locator('[data-message-id]');
    if (await messages.count()) await expect(messages).toHaveCount(2);
    await expect(page.getByRole("alert").filter({ hasText: /\S/ })).toHaveCount(0);
  });
}

test("lost response retries the same operation without a duplicate", async ({ page }) => {
  await setup(page);
  const bodies = [];
  await page.route("**/tasks/input", async route => {
    bodies.push(route.request().postDataJSON());
    if (bodies.length === 1) { await route.fetch(); await route.abort("failed"); }
    else await route.continue();
  });
  await page.getByLabel("Ваш вопрос").fill("Подобрать эспрессо lost-response");
  await page.getByRole("button", { name: "Отправить" }).click();
  await page.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(page.getByText("Ответ: Подобрать эспрессо lost-response [e2e-fixture-model]", { exact: true })).toBeVisible();
  expect(bodies).toHaveLength(2);
  expect(bodies[0].client_message_id).toBeTruthy();
  expect(bodies[1].client_message_id).toBe(bodies[0].client_message_id);
  await page.reload();
  await expect(page.getByText("Ответ: Подобрать эспрессо lost-response [e2e-fixture-model]", { exact: true })).toHaveCount(1);
});

test("ambiguous task choice waits for explicit selection", async ({ page }) => {
  await setup(page);
  await submit(page, "эспрессо помол");
  await submit(page, "новая задача эспрессо зерно");
  await page.getByLabel("Ваш вопрос").fill("эспрессо");
  await page.getByRole("button", { name: "Отправить" }).click();
  await expect(page.getByRole("heading", { name: "К какой задаче относится сообщение?" })).toBeVisible();
  await expect(page.getByText("Ответ: эспрессо [e2e-fixture-model]", { exact: true })).toHaveCount(0);
  await panel(page).getByRole("button", { name: "эспрессо помол эспрессо помол", exact: true }).click();
  await expect(page.getByText("Ответ: эспрессо [e2e-fixture-model]", { exact: true })).toBeVisible();
});

test("lost Resume response is manually retried with the same identity", async ({ page }) => {
  await setup(page);
  const text = "memory-slow эспрессо resume-retry";
  await page.getByLabel("Ваш вопрос").fill(text);
  await page.getByRole("button", { name: "Отправить" }).click();
  await page.getByRole("button", { name: "Остановить" }).click();
  const bodies = [];
  await page.route("**/resume", async route => {
    bodies.push(route.request().postDataJSON());
    if (bodies.length === 1) { await route.fetch(); await route.abort("failed"); }
    else await route.continue();
  });
  await page.getByRole("button", { name: "Продолжить", exact: true }).click();
  await page.getByRole("button", { name: "Повторить продолжение", exact: true }).click();
  await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`, { exact: true })).toHaveCount(1);
  expect(bodies).toHaveLength(2);
  expect(bodies[1].client_message_id).toBe(bodies[0].client_message_id);
  await page.reload();
  await expect(page.getByText(`Ответ: ${text} [e2e-fixture-model]`, { exact: true })).toHaveCount(1);
});
