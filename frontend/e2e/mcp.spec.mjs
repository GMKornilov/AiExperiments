import { expect, test } from "@playwright/test";

const tools = [
  ["brewmark_list_brew_methods", "List BrewMark brew methods."],
  ["brewmark_list_brewers", "List BrewMark brewing machines and manual brewers."],
  ["brewmark_list_filters", "List BrewMark coffee filters."],
  ["brewmark_list_grinders", "List BrewMark coffee grinders."],
];

for (const width of [390, 1440]) {
  test(`MCP tab connects and lists the registry at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/mcp");

    const connect = page.getByRole("button", { name: "Подключиться и получить tools" });
    await connect.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("status")).toContainText("Получено tools: 4.");
    const list = page.getByRole("list", { name: "Доступные MCP tools" });
    await expect(list).toBeVisible();
    for (const [name, description] of tools) {
      await expect(list.getByText(name, { exact: true })).toBeVisible();
      await expect(list.getByText(description, { exact: true })).toBeVisible();
    }
    await expect(list.locator("li")).toHaveCount(4);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  });
}

test("MCP connect appears in the system journal without a browser MCP request", async ({ page }) => {
  const apiRequests = [];
  const browserRequests = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    browserRequests.push(url);
    if (url.pathname.startsWith("/api/")) apiRequests.push(url);
  });

  await page.goto("/mcp");
  await page.getByRole("button", { name: "Подключиться и получить tools" }).click();
  await expect(page.getByRole("status")).toContainText("Получено tools: 4.");

  await page.goto("/admin");
  await page.getByRole("radio", { name: "Системный MCP" }).check();
  await expect(page.getByText("MCP → BrewMark").first()).toBeVisible({ timeout: 15_000 });

  const bffCorrelation = await page.locator("article").filter({ hasText: "Источник: frontend_bff" }).first()
    .getByText(/^Источник: frontend_bff · Корреляция:/).textContent();
  const correlationID = bffCorrelation?.match(/Корреляция: (.+)$/)?.[1];
  expect(correlationID).toBeTruthy();

  for (const source of ["frontend_bff", "backend", "mcp_server"]) {
    await expect(page.getByText(`Источник: ${source} · Корреляция: ${correlationID}`, { exact: true }).first()).toBeVisible({ timeout: 15_000 });
  }
  await expect(page.getByRole("heading", { name: "mcp_tools_request" }).first()).toBeVisible();
  await expect(page.getByText("Успешно").first()).toBeVisible();
  await expect(page.getByText(/\d+ мс/).first()).toBeVisible();

  const mcpToolsRequests = apiRequests.filter((url) => url.pathname === "/api/mcp/tools");
  const mcpJournalRequests = apiRequests.filter((url) => url.pathname === "/api/admin/logs" && url.search === "?scope=mcp");
  expect(mcpToolsRequests).toHaveLength(1);
  expect(mcpJournalRequests.length).toBeGreaterThan(0);
  expect(browserRequests.every((url) => url.origin === new URL(page.url()).origin)).toBeTruthy();
});
