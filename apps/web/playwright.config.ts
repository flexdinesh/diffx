import { defineConfig, devices } from "@playwright/test";

// The dev-only design gallery; a dead API URL keeps Vite from starting a fixture.
const galleryURL = "http://127.0.0.1:4719";

export default defineConfig({
  // Workers own isolated fixture servers (test/browser/fixtures.ts).
  fullyParallel: true,
  // Leave headroom for Pierre rendering and concurrent Go checks.
  workers: "33%",
  globalSetup: "./test/browser/global-setup.ts",
  testDir: "./test/browser",
  outputDir: "test-results",
  use: {
    trace: "retain-on-failure",
  },
  expect: {
    // Exact goldens: on a mostly flat page, a ratio tolerance hides whole rows of text.
    toHaveScreenshot: { animations: "disabled" },
  },
  webServer: {
    command: "pnpm exec vite --host 127.0.0.1 --port 4719 --strictPort",
    url: `${galleryURL}/design.html`,
    env: { DIFFX_API_URL: "http://127.0.0.1:9" },
    reuseExistingServer: false,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
    {
      name: "gallery",
      testDir: "./test/gallery",
      snapshotPathTemplate: "{testDir}/goldens/{arg}-{platform}{ext}",
      use: {
        ...devices["Desktop Chrome"],
        baseURL: galleryURL,
        viewport: { width: 1280, height: 800 },
      },
    },
  ],
});
