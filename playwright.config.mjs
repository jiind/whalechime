import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "tests/ui",
  fullyParallel: false,
  workers: 1,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: process.env.WHALECHIME_TEST_BASE_URL || "http://127.0.0.1:18086",
    headless: true,
    trace: "retain-on-failure",
    launchOptions: process.env.WHALECHIME_TEST_BROWSER_PATH ? { executablePath: process.env.WHALECHIME_TEST_BROWSER_PATH } : {}
  },
  reporter: [["list"]]
});
