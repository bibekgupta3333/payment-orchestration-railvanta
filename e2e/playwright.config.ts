import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  testMatch: /(?:api|ui)\/.*\.spec\.ts/,
  globalSetup: "./support/global-setup.ts",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: Boolean(process.env.CI),
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: [["list"], ["html", { open: "never" }]],
  outputDir: "test-results",
  use: {
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "api", testMatch: /api\/.*\.spec\.ts/ },
    {
      name: "ui",
      testMatch: /ui\/.*\.spec\.ts/,
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
