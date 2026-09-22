import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  // Legacy strategy/summary/branching scenarios stay in e2e as references,
  // but the active product contract is the project-memory flow.
  testMatch: ["**/memory.spec.mjs", "**/profiles.spec.mjs", "**/tasks.spec.mjs", "**/mcp.spec.mjs"],
  timeout: 30_000,
  fullyParallel: false,
  use: {
    baseURL: process.env.BARISTA_E2E_URL ?? "http://localhost:13000",
    browserName: "chromium",
    channel: "chrome",
    permissions: ["clipboard-read", "clipboard-write"],
    viewport: { width: 1440, height: 900 },
  },
  reporter: "list",
});
