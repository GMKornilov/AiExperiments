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
