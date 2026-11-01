import { defineConfig } from "@playwright/test";

// Opt-in README quickstart verification (WBS 5.4). Deliberately has no globalSetup:
// the spec clones the repo and runs everything, including the real suite, itself.
export default defineConfig({
  testDir: "./docs",
  testMatch: /quickstart\.spec\.ts/,
  workers: 1,
  retries: 0,
  timeout: 45 * 60_000,
  reporter: [["list"]],
  outputDir: "test-results/quickstart", // ignored by git
});
