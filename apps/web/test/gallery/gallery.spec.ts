import { expect, test } from "@playwright/test";

// Visual goldens for every primitive and composite variant. Baselines are
// Chromium on Linux; other platforms regenerate with --update-snapshots.
const themes = ["light", "dark"] as const;
const sections = [
  "tokens",
  "button",
  "toggle",
  "tabs",
  "badge",
  "fields",
  "card",
  "rows",
  "choices",
  "states",
] as const;

async function open(page: import("@playwright/test").Page, query: string) {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto(`/design.html?${query}`);
  await page.evaluate(() => document.fonts.ready);
}

for (const theme of themes) {
  test(`components in ${theme}`, async ({ page }) => {
    await open(page, `theme=${theme}`);
    for (const section of sections)
      await expect(
        page.locator(`[data-gallery="${section}"]`),
      ).toHaveScreenshot(`${section}-${theme}.png`);
  });

  test(`overlays in ${theme}`, async ({ page }) => {
    await open(page, `theme=${theme}&page=overlays`);
    await expect(
      page.getByRole("menu", { name: "View options" }),
    ).toBeVisible();
    await expect(
      page.getByRole("dialog", { name: "Server healthy" }),
    ).toBeVisible();
    await expect(page.getByText("Sync all checkouts")).toBeVisible();
    await expect(page).toHaveScreenshot(`overlays-${theme}.png`);
  });

  test(`confirm dialog in ${theme}`, async ({ page }) => {
    await open(page, `theme=${theme}&page=dialog`);
    const dialog = page.getByRole("dialog", { name: "Delete comment?" });
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeFocused();
    await expect(page).toHaveScreenshot(`dialog-${theme}.png`);
  });
}

test("touch sizing on narrow screens", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await open(page, "theme=light");
  await expect(page.locator('[data-gallery="button"]')).toHaveScreenshot(
    "button-touch.png",
  );
  const touch = page.getByRole("button", { name: "Touch", exact: true });
  expect((await touch.boundingBox())?.height).toBe(44);
});
