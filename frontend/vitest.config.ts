import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const sourceDirectory = fileURLToPath(new URL("./src", import.meta.url));
const serverOnlyStub = fileURLToPath(
  new URL("./src/test/server-only.ts", import.meta.url),
);

export default defineConfig({
  resolve: {
    alias: {
      "@": sourceDirectory,
      "server-only": serverOnlyStub,
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    restoreMocks: true,
  },
});
