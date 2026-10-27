import {
  expect,
  test,
  withReviewNavigation,
  withSecondReview,
} from "./fixtures.ts";
import { resetFixtureState } from "./fixture-state.ts";

test.use({ launchOptions: { ignoreDefaultArgs: ["--hide-scrollbars"] } });
test.beforeEach(async ({ request }) => resetFixtureState(request));

test("pane scrollbars keep a constant thin gutter as overflow changes", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 700 });
  await page.goto("/");
  await expect(page.locator("#viewer diffs-container").first()).toBeVisible();
  const gutter = (selector: string) =>
    page
      .locator(selector)
      .evaluate((element) =>
        element instanceof HTMLElement
          ? element.offsetWidth - element.clientWidth
          : NaN,
      );
  const size = await page
    .locator("html")
    .evaluate((element) =>
      parseFloat(
        getComputedStyle(element).getPropertyValue("--scrollbar-size"),
      ),
    );
  expect(size).toBe(10);

  // Filtering the tree to one file must not widen its rows.
  const tree = page.locator("#file-tree");
  const row = page.locator('#file-tree [data-path="src/components/Badge.tsx"]');
  const rowRight = async () => {
    const box = await row.boundingBox();
    return box ? box.x + box.width : undefined;
  };
  const before = await rowRight();
  await page.getByRole("searchbox", { name: "Filter files" }).fill("Badge");
  await expect(page.locator("#file-tree [data-path]")).toHaveCount(1);
  await expect
    .poll(() =>
      tree.evaluate((element) => element.scrollHeight > element.clientHeight),
    )
    .toBe(false);
  expect(await gutter("#file-tree")).toBe(size);
  expect(await rowRight()).toBe(before);

  // The diff canvas reserves its track without overflow too, so the
  // toolbar inset and trailing actions stay put.
  const scroller = page.locator(".diff-code-view");
  const toggle = page.locator("#toggle-all-files");
  const toggleRight = async () => {
    const box = await toggle.boundingBox();
    return box ? box.x + box.width : undefined;
  };
  await expect.poll(() => gutter(".diff-code-view")).toBe(size);
  const overflowing = await toggleRight();
  await scroller.evaluate((element) => {
    for (const child of Array.from(element.children))
      if (child instanceof HTMLElement) child.style.display = "none";
  });
  await expect
    .poll(() =>
      scroller.evaluate(
        (element) => element.scrollHeight > element.clientHeight,
      ),
    )
    .toBe(false);
  expect(await gutter(".diff-code-view")).toBe(size);
  expect(await toggleRight()).toBe(overflowing);
});

test("diff tracks fill frames and headers align with their gutters", async ({
  page,
}) => {
  // Header geometry is measured against the compact "Show reviews" toggle.
  await withReviewNavigation(page);
  await withSecondReview(page);
  const colorSchemes: ("light" | "dark")[] = ["light", "dark"];
  for (const colorScheme of colorSchemes) {
    await page.emulateMedia({ colorScheme });
    await page.setViewportSize({ width: 1440, height: 4_000 });
    await page.goto("/");
    const files = page.locator("#viewer diffs-container");
    await expect(files).toHaveCount(12);
    await expect
      .poll(() =>
        files.evaluateAll((elements) =>
          elements.slice(0, 4).every((element) => {
            const code =
              element.shadowRoot?.querySelector<HTMLElement>("[data-code]");
            const last =
              code?.querySelector("[data-content]")?.lastElementChild;
            if (!code || !last) return false;
            const trackHeight = code.offsetHeight - code.clientHeight;
            return (
              trackHeight > 0 &&
              getComputedStyle(code).backgroundColor ===
                getComputedStyle(last).backgroundColor &&
              Math.abs(
                element.getBoundingClientRect().bottom -
                  last.getBoundingClientRect().bottom -
                  trackHeight,
              ) <= 0.5
            );
          }),
        ),
      )
      .toBe(true);
    await expect
      .poll(() =>
        files.evaluateAll((elements) => {
          const first = elements[0];
          const next = elements[1];
          const scaffold = first?.parentElement?.parentElement;
          if (!scaffold || !first || !next)
            throw new Error("Missing virtual scroll scaffold");
          const gap =
            next.getBoundingClientRect().top -
            first.getBoundingClientRect().bottom;
          const measured = elements.reduce(
            (height, element) =>
              height + element.getBoundingClientRect().height,
            (elements.length - 1) * gap,
          );
          return Math.abs(scaffold.getBoundingClientRect().height - measured);
        }),
      )
      .toBeLessThanOrEqual(0.5);

    const longDiff = files.filter({ hasText: "src/components/Button.tsx" });
    const code = longDiff.locator("[data-code][data-additions]");
    await code.evaluate((element) => {
      element.scrollLeft = 50;
    });
    await expect
      .poll(() => code.evaluate((element) => element.scrollLeft))
      .toBeGreaterThan(0);

    for (const viewport of [
      { width: 1440, height: 1000 },
      { width: 390, height: 844 },
    ]) {
      await page.setViewportSize(viewport);
      const scroller = page.locator(".diff-code-view");
      await scroller.evaluate((element) => {
        element.scrollTop = 0;
      });
      await expect
        .poll(() =>
          files.first().evaluate((element) => {
            // Trailing header actions anchor to the shell; diff actions
            // retain their measured gutter.
            const trailing = Array.from(
              document.querySelectorAll(".header-actions > *"),
            )
              .filter((action) => action.getClientRects().length > 0)
              .at(-1);
            const toggle = document.querySelector("#toggle-all-files");
            const header = document.querySelector(".topbar");
            if (!toggle || !header) throw new Error("Missing shell actions");
            const edge = element.getBoundingClientRect().right;
            return Math.max(
              trailing
                ? Math.abs(
                    trailing.getBoundingClientRect().right -
                      header.getBoundingClientRect().right,
                  )
                : 0,
              Math.abs(toggle.getBoundingClientRect().right - edge),
            );
          }),
        )
        .toBeLessThanOrEqual(0.5);
      await expect
        .poll(() =>
          files.first().evaluate((element) => {
            const scroller = document.querySelector(".diff-code-view");
            if (!scroller) throw new Error("Missing diff canvas");
            const file = element.getBoundingClientRect();
            const canvas = scroller.getBoundingClientRect();
            return {
              top: file.top - canvas.top,
              left: file.left - canvas.left,
              right: canvas.left + scroller.clientWidth - file.right,
            };
          }),
        )
        .toEqual({ top: 12, left: 12, right: 12 });
      await scroller.evaluate((element) => {
        element.scrollTop = element.scrollHeight;
      });
      await expect
        .poll(() =>
          files.last().evaluate((element) => {
            // Trailing scroll space follows the gutter below the last file.
            const end = document.querySelector(".diff-scroll-end");
            if (!end) throw new Error("Missing diff scroll end");
            return (
              end.getBoundingClientRect().top -
              element.getBoundingClientRect().bottom
            );
          }),
        )
        .toBe(12);
    }
    const header = await page.locator(".topbar").boundingBox();
    const toolbar = await page.locator(".toolbar").boundingBox();
    const action = await page
      .locator(".topbar")
      .getByRole("button", { name: "Show reviews" })
      .boundingBox();
    if (!header) throw new Error("Missing header");
    // The wrapping toolbar keeps a 44px control row plus its bottom border.
    expect(toolbar?.height).toBe(header.height + 1);
    for (const id of ["#view-options", "#toggle-all-files"]) {
      const button = await page.locator(id).boundingBox();
      expect(button?.width).toBe(action?.width);
      expect(button?.height).toBe(action?.height);
    }
  }
});
