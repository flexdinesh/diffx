import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  // Workers own isolated fixture servers (test/browser/fixtures.ts).
  fullyParallel: true,
  workers: 1,
  globalSetup: "./test/browser/global-setup.ts",
  testDir: "./test/browser",
  outputDir: "test-results",
  use: {
    trace: "retain-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
