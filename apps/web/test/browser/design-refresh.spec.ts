import { type Page } from "@playwright/test";
import { expect, fileToggle, reviewedToggle, test } from "./fixtures.ts";
import { resetFixtureState } from "./fixture-state.ts";
import { THEMES, themePalette } from "@diffx/review-app/theme";

test.beforeEach(async ({ request }) => resetFixtureState(request));

/** The file sidebar sits beside the review column on desktop. */
async function showDiff(page: Page) {
  await expect(page.locator("#sidebar")).toBeVisible();
}

async function seedReviewComments(page: Page) {
  await page.evaluate(async () => {
    const catalog: unknown = await (await fetch("/api/v2/contexts")).json();
    if (
      typeof catalog !== "object" ||
      catalog === null ||
      !("contexts" in catalog) ||
      !Array.isArray(catalog.contexts)
    )
      throw new Error("Missing catalog");
    const context: unknown = catalog.contexts[0];
    if (
      typeof context !== "object" ||
      context === null ||
      !("id" in context) ||
      typeof context.id !== "string"
    )
      throw new Error("Missing context");
    const contextId = context.id;
    const base = "/api/v2/contexts/" + encodeURIComponent(contextId);
    const response = await fetch(base + "/diffs/current?scope=all");
    const data: unknown = await response.json();
    if (typeof data !== "object" || data === null)
      throw new Error("Bad fixture");
    const diffId = Reflect.get(data, "id");
    const versionId = Reflect.get(data, "versionId");
    const files = Reflect.get(data, "files");
    if (
      typeof diffId !== "string" ||
      typeof versionId !== "string" ||
      !Array.isArray(files)
    )
      throw new Error("Missing fixture metadata");
    const file = files.find(
      (entry) =>
        typeof entry === "object" &&
        entry !== null &&
        Reflect.get(entry, "path") === "src/value.ts",
    );
    const fingerprint =
      typeof file === "object" && file !== null
        ? Reflect.get(file, "fingerprint")
        : undefined;
    if (typeof fingerprint !== "string")
      throw new Error("Missing fixture fingerprint");
    const imported = await fetch(base + "/comments/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        comments: [
          {
            id: "open-review-comment",
            diffId,
            versionId,
            path: "src/value.ts",
            scope: "all",
            fingerprint,
            side: "additions",
            start: 3,
            end: 3,
            code: '+ export const label = "diffx";',
            body: "Open feedback",
            status: "open",
            createdAt: 1,
          },
          {
            id: "resolved-review-comment",
            diffId,
            versionId,
            path: "src/value.ts",
            scope: "all",
            fingerprint,
            side: "additions",
            start: 3,
            end: 3,
            code: '+ export const label = "diffx";',
            body: "Resolved feedback",
            status: "resolved",
            createdAt: 2,
          },
        ],
      }),
    });
    if (!imported.ok) throw new Error("Could not seed review comments");
  });
  await page.reload();
}

test("narrow screens use unified layout without replacing the desktop preference", async ({
  page,
}) => {
  await page.setViewportSize({ width: 767, height: 844 });
  await page.addInitScript(() => localStorage.setItem("layout", "split"));
  await page.goto("/");

  await page.getByRole("button", { name: "View options" }).click();
  await expect(
    page.getByRole("menuitemradio", { name: "Unified", exact: true }),
  ).toHaveAttribute("aria-checked", "true");
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem("layout")))
    .toBe("split");
  await page.keyboard.press("Escape");

  await page.setViewportSize({ width: 1280, height: 844 });
  await expect(
    page.getByRole("button", { name: "Split", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
});

test("view options expose secondary display settings and persist them", async ({
  page,
}) => {
  await page.goto("/");

  await page.getByRole("button", { name: "View options" }).click();
  const menu = page.getByRole("menu", { name: "View options" });
  const wrap = menu.getByRole("menuitemcheckbox", { name: "Wrap lines" });
  await expect(wrap).toHaveAttribute("aria-checked", "false");
  await expect(
    menu.getByRole("menuitemradio", { name: "Words", exact: true }),
  ).toHaveAttribute("aria-checked", "true");
  await expect(
    menu.getByRole("menuitemradio", { name: "Match app theme" }),
  ).toHaveAttribute("aria-checked", "true");

  await wrap.click();
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem("wrap")))
    .toBe("true");
});

test("every rendered corner is square, inside Pierre's shadow roots too", async ({
  page,
}) => {
  await page.goto("/");
  await showDiff(page);
  await expect(
    page.locator("#viewer [data-diffs-header]").first(),
  ).toBeAttached();

  const curved = await page.evaluate(() => {
    const found: string[] = [];
    const visit = (root: Document | ShadowRoot) => {
      for (const element of root.querySelectorAll("*")) {
        for (const pseudo of [null, "::before", "::after"]) {
          const style = getComputedStyle(element, pseudo);
          if (pseudo && style.content === "none") continue;
          if (
            parseFloat(style.borderTopLeftRadius) > 0 ||
            parseFloat(style.borderTopRightRadius) > 0 ||
            parseFloat(style.borderBottomLeftRadius) > 0 ||
            parseFloat(style.borderBottomRightRadius) > 0
          )
            found.push(`${element.tagName} ${pseudo ?? ""}`);
        }
        if (element.shadowRoot) visit(element.shadowRoot);
      }
    };
    visit(document);
    return found;
  });
  expect(curved).toEqual([]);
  // Pierre's status sprites are rounded paths; square masks replace them.
  const sprites = await page.evaluate(() =>
    [...document.querySelectorAll("#viewer *")]
      .flatMap((element) =>
        element.shadowRoot
          ? [...element.shadowRoot.querySelectorAll("[data-change-icon] use")]
          : [],
      )
      .map((sprite) => getComputedStyle(sprite).display),
  );
  expect(sprites.length).toBeGreaterThan(0);
  expect(new Set(sprites)).toEqual(new Set(["none"]));
});

test("Diffx themes put context code on the shell surface; palettes keep theirs", async ({
  page,
}) => {
  const useTheme = (theme: string) =>
    page.evaluate((id) => localStorage.setItem("theme-v2", id), theme);
  const viewer = page.locator("#viewer");
  const contextLine = viewer
    .locator('[data-line][data-line-type^="context"]')
    .first();
  // Added and deleted files lead the fixture; context lines start further down.
  await page.setViewportSize({ width: 1440, height: 4_000 });
  await page.goto("/");
  await useTheme("dark");
  await page.reload();
  await showDiff(page);
  await expect(viewer).toHaveAttribute("data-code-surface", "shell");
  await page
    .getByRole("button", { name: "src/components/Button.tsx, Modified" })
    .click();
  await expect(contextLine).toHaveCSS("background-color", "rgb(40, 44, 51)");
  await useTheme("tokyo-night");
  await page.reload();
  await showDiff(page);
  await expect(viewer).not.toHaveAttribute("data-code-surface", "shell");
  await expect
    .poll(() =>
      viewer.evaluate((element) =>
        getComputedStyle(element).getPropertyValue("--code-surface"),
      ),
    )
    .toBe("");
});

test("interactive controls are square", async ({ page }) => {
  await page.goto("/");
  await showDiff(page);

  const viewOptions = page.getByRole("button", { name: "View options" });
  const controlRadius = "0px";
  const controls = [
    page.getByRole("searchbox", { name: "Filter files" }).locator(".."),
    page.getByRole("button", { name: /^Collapse folder / }).first(),
    page.locator("#file-tree [data-path]").first(),
    page.getByRole("group", { name: "Diff layout" }),
    page.getByRole("button", { name: "Split", exact: true }),
    viewOptions,
    reviewedToggle(page).first(),
    page.getByRole("tab", { name: /Changes/ }),
  ];
  for (const control of controls)
    await expect(control).toHaveCSS("border-radius", controlRadius);

  await viewOptions.click();
  const menu = page.getByRole("menu", { name: "View options" });
  await expect(menu).toHaveCSS("border-radius", controlRadius);
  await expect(
    menu.getByRole("menuitemcheckbox", { name: "Wrap lines" }),
  ).toHaveCSS("border-radius", controlRadius);
  await expect(
    menu.getByRole("menuitemradio", { name: "Split", exact: true }),
  ).toHaveCSS("border-radius", controlRadius);
});

test("mobile sidebar behaves as a modal drawer and restores focus", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");

  const trigger = page.getByRole("button", { name: "Show file sidebar" });
  await trigger.click();

  const drawer = page.getByRole("dialog", { name: "Review navigation" });
  await expect(drawer).toBeVisible();
  await expect(drawer).toHaveAttribute("aria-modal", "true");
  await expect(page.locator("main")).toHaveAttribute("inert", "");
  await expect(page.locator(".topbar")).toHaveAttribute("inert", "");
  await expect(drawer.locator(":focus")).toHaveCount(1);
  await expect(page.locator('[data-slot="scrim"]')).toBeVisible();

  await page
    .locator('[data-slot="scrim"]')
    .click({ position: { x: 380, y: 800 } });
  await expect(drawer).toBeHidden();
  await expect(trigger).toBeFocused();
  await expect(page.locator("main")).not.toHaveAttribute("inert", "");
  await expect(page.locator(".topbar")).not.toHaveAttribute("inert", "");
});

test("review actions use progressive disclosure, status filters, and confirmed deletion", async ({
  page,
}) => {
  await page.goto("/");
  await seedReviewComments(page);
  await showDiff(page);

  await expect(
    page.getByRole("button", { name: "Hide summary" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Hide review tools" }),
  ).toHaveCount(0);
  await page.getByRole("tab", { name: /Review/ }).click();
  await expect(page.getByRole("button", { name: "Hide summary" })).toHaveCount(
    0,
  );
  await expect(
    page.getByRole("button", { name: "Hide review tools" }),
  ).toBeVisible();
  const copyReview = page.getByRole("button", { name: "Copy review" });
  await expect(copyReview).toBeVisible();
  const controlRadius = "0px";
  await expect(
    page.getByRole("button", { name: "WebMCP instruction" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Copy options" }).click();
  const options = page.getByRole("menu", { name: "Copy options" });
  await expect(
    options.getByRole("menuitem", { name: "Open comments" }),
  ).toBeVisible();
  await expect(
    options.getByRole("menuitem", { name: "All comments" }),
  ).toBeVisible();
  await expect(options.getByRole("menuitem")).toHaveCount(2);
  await page.keyboard.press("Escape");

  const panel = page.locator("#comments-panel");
  await expect(panel.locator(".comment-summary")).toHaveCount(0);
  const filterTrigger = panel.getByRole("button", {
    name: /^Filter comments: /,
  });
  const count = (scope: typeof filterTrigger) =>
    scope.locator('[data-slot="badge"]');
  await expect(filterTrigger).toHaveAccessibleName("Filter comments: Open");
  await expect(filterTrigger.locator("..")).toHaveCSS(
    "justify-content",
    "flex-start",
  );
  await expect(count(filterTrigger)).toHaveText("1");
  // Comment filter counts share the CountBadge geometry used by other counts.
  await expect(count(filterTrigger)).toHaveAttribute("data-variant", "count");
  const [badgeBox, triggerBox] = await Promise.all([
    count(filterTrigger).boundingBox(),
    filterTrigger.boundingBox(),
  ]);
  expect(badgeBox?.height ?? Infinity).toBeLessThan(triggerBox?.height ?? 0);
  await filterTrigger.click();
  const filters = page.getByRole("menu", { name: "Filter comments" });
  await expect(filterTrigger).toHaveCSS("border-radius", controlRadius);
  await expect(filters).toHaveCSS("border-radius", controlRadius);
  const openFilter = filters.getByRole("menuitemradio", { name: "Open 1" });
  const resolvedFilter = filters.getByRole("menuitemradio", {
    name: "Resolved 1",
  });
  const outdatedFilter = filters.getByRole("menuitemradio", {
    name: "Outdated 0",
  });
  await expect(count(openFilter)).toHaveText("1");
  await expect(count(resolvedFilter)).toHaveText("1");
  await expect(count(outdatedFilter)).toHaveText("0");
  await expect(
    filters.getByRole("menuitemradio", { name: "All 2" }),
  ).toBeVisible();
  const filterMenuBox = await filters.boundingBox();
  expect(filterMenuBox?.width ?? Number.POSITIVE_INFINITY).toBeLessThanOrEqual(
    120,
  );
  await expect(openFilter).toHaveAttribute("aria-checked", "true");
  const selectedOptionAlignment = await openFilter.evaluate((option) => {
    const indicator = option.querySelector(
      '[data-slot="dropdown-menu-radio-item-indicator"]',
    );
    const label = Array.from(option.children).find(
      (child) => child.textContent === "Open",
    );
    if (!indicator || !label) throw new Error("Missing filter option content");
    return {
      indicatorRight: indicator.getBoundingClientRect().right,
      labelLeft: label.getBoundingClientRect().left,
    };
  });
  expect(
    selectedOptionAlignment.labelLeft - selectedOptionAlignment.indicatorRight,
  ).toBeGreaterThanOrEqual(4);
  await page.keyboard.press("Escape");
  await expect(panel.getByText("Open feedback")).toBeVisible();
  await expect(panel.getByText("Resolved feedback")).toBeHidden();

  await filterTrigger.click();
  await resolvedFilter.click();
  await expect(filterTrigger).toHaveAccessibleName("Filter comments: Resolved");
  await expect(panel.getByText("Open feedback")).toBeHidden();
  const resolvedCard = panel.locator(
    '[data-comment-id="resolved-review-comment"]',
  );
  await resolvedCard
    .getByRole("button", { name: /Expand resolved comment/ })
    .click();
  await resolvedCard
    .getByRole("button", { name: /^Delete comment at / })
    .click();

  const dialog = page.getByRole("dialog", { name: "Delete comment?" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Cancel" })).toBeFocused();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toBeHidden();
  await expect(resolvedCard).toBeVisible();
  await resolvedCard
    .getByRole("button", { name: /^Delete comment at / })
    .click();
  await page
    .getByRole("dialog", { name: "Delete comment?" })
    .getByRole("button", { name: "Delete comment" })
    .click();
  await expect(resolvedCard).toHaveCount(0);
  await expect(count(filterTrigger)).toHaveText("0");

  await filterTrigger.click();
  await openFilter.click();
  const openCard = panel.locator('[data-comment-id="open-review-comment"]');
  await openCard.getByRole("button", { name: /^Edit comment at / }).click();
  const editor = page.getByRole("textbox", { name: "Review comment" });
  await expect(page.getByText("Edit comment", { exact: true })).toBeVisible();
  await editor.fill("");
  const save = page.getByRole("button", { name: "Save", exact: true });
  await expect(save).toBeEnabled();
  await save.click();
  await expect(editor).toBeFocused();
  await expect(editor).toHaveAttribute("aria-invalid", "true");
  await expect(
    page.getByRole("alert").getByText("Enter a comment before saving."),
  ).toBeVisible();
});

test("bulk comment deletion selects a status and defaults to all", async ({
  page,
}) => {
  await page.goto("/");
  await seedReviewComments(page);
  await showDiff(page);
  await page.getByRole("tab", { name: /Review/ }).click();

  const panel = page.locator("#comments-panel");
  const filter = panel.getByRole("button", { name: /^Filter comments: / });
  const removeOptions = panel.getByRole("button", { name: "Delete options" });
  const removeGroup = panel.getByRole("group").filter({
    has: page.getByRole("button", { name: "Delete options" }),
  });
  const remove = removeGroup.getByRole("button", {
    name: "Delete",
    exact: true,
  });
  const controlRadius = "0px";
  // Bulk delete rests quiet; the confirm dialog carries the destructive fill.
  await expect(remove).toHaveAttribute("data-variant", "destructive");
  await expect(remove).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await expect(remove).toHaveCSS("height", "24px");
  await expect(remove).toHaveCSS("border-top-left-radius", controlRadius);
  await expect(remove).toHaveCSS("border-top-right-radius", "0px");
  await expect(removeOptions).toHaveCSS("border-top-left-radius", "0px");
  await expect(removeOptions).toHaveCSS(
    "border-top-right-radius",
    controlRadius,
  );
  const controls = await removeGroup.evaluate((remove) => {
    const header = remove.parentElement;
    if (!header) throw new Error("Missing review controls header");
    return {
      marginLeft: getComputedStyle(remove).marginLeft,
      rightInset:
        header.getBoundingClientRect().right -
        remove.getBoundingClientRect().right,
    };
  });
  expect(controls.marginLeft).not.toBe("0px");
  expect(Math.abs(controls.rightInset)).toBeLessThanOrEqual(0.5);

  await removeOptions.click();
  const menu = page.getByRole("menu", { name: "Delete options" });
  await expect(menu).toHaveCSS("border-radius", controlRadius);
  await expect(menu.getByRole("menuitem", { name: "All 2" })).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: "Open 1" })).toBeVisible();
  await menu.getByRole("menuitem", { name: "Resolved 1" }).click();
  const resolvedDialog = page.getByRole("dialog", {
    name: "Delete resolved comments?",
  });
  await expect(resolvedDialog).toContainText(
    "This permanently removes 1 resolved comment.",
  );
  await resolvedDialog.getByRole("button", { name: "Delete comments" }).click();
  await expect(filter.locator('[data-slot="badge"]')).toHaveText("1");

  await removeOptions.click();
  await expect(
    menu.getByRole("menuitem", { name: "Resolved 0" }),
  ).toHaveAttribute("data-disabled", "");
  await expect(
    menu.getByRole("menuitem", { name: "Outdated 0" }),
  ).toHaveAttribute("data-disabled", "");
  await page.keyboard.press("Escape");

  await remove.click();
  const allDialog = page.getByRole("dialog", {
    name: "Delete all comments?",
  });
  await expect(allDialog).toContainText("This permanently removes 1 comment.");
  await allDialog.getByRole("button", { name: "Delete comments" }).click();
  await expect(page.locator("#comment-count")).toHaveText("0");
  await expect(filter).toHaveCount(0);
  await expect(removeOptions).toHaveCount(0);
});

test("every theme retains readable text and visible control boundaries", async ({
  page,
}) => {
  await page.goto("/");

  for (const theme of THEMES.filter(({ id }) => id !== "system")) {
    await page.locator("html").evaluate(
      (element, { mode, palette }) => {
        element.dataset.theme = mode;
        if (palette) element.dataset.palette = palette;
        else delete element.dataset.palette;
      },
      { mode: theme.mode, palette: themePalette(theme.id) },
    );
    const contrast = await page.evaluate(() => {
      const rgb = (value: string) => {
        const channels = value
          .match(/[\d.]+/g)
          ?.slice(0, 3)
          .map(Number);
        if (!channels || channels.length !== 3)
          throw new Error(`Unsupported color: ${value}`);
        return channels;
      };
      const luminance = (channels: number[]) => {
        const values = channels.map((channel) => {
          const normalized = channel / 255;
          return normalized <= 0.04045
            ? normalized / 12.92
            : ((normalized + 0.055) / 1.055) ** 2.4;
        });
        const red = values[0];
        const green = values[1];
        const blue = values[2];
        if (red === undefined || green === undefined || blue === undefined)
          throw new Error("Missing RGB channel");
        return red * 0.2126 + green * 0.7152 + blue * 0.0722;
      };
      const ratio = (first: string, second: string) => {
        const values = [luminance(rgb(first)), luminance(rgb(second))].sort(
          (left, right) => right - left,
        );
        const lighter = values[0];
        const darker = values[1];
        if (lighter === undefined || darker === undefined)
          throw new Error("Missing luminance");
        return (lighter + 0.05) / (darker + 0.05);
      };
      const root = getComputedStyle(document.documentElement);
      const probe = document.createElement("span");
      document.body.append(probe);
      const token = (name: string) => {
        probe.style.color = `var(--${name})`;
        return getComputedStyle(probe).color;
      };
      const controlBorder = token("border-control");
      const hover = token("hover");
      const selected = token("accent-bg");
      const surfaces = ["bg", "panel", "surface-raised"].map(token);
      const onSurfaces = (name: string) =>
        Math.min(...surfaces.map((surface) => ratio(token(name), surface)));
      const fileHints = ["file-code", "file-config"].flatMap((name) => [
        ratio(token(name), hover),
        ratio(token(name), selected),
      ]);
      const statusBorders = [
        ratio(token("success-border"), token("success-bg")),
        ratio(token("warning-border"), token("warning-bg")),
        ratio(token("error-border"), token("error-bg")),
      ];
      const result = {
        text: ratio(root.color, root.backgroundColor),
        control: ratio(controlBorder, root.backgroundColor),
        hoverControl: ratio(controlBorder, hover),
        fileHint: Math.min(...fileHints),
        statusBorder: Math.min(...statusBorders),
        secondary: Math.min(onSurfaces("text-secondary"), onSurfaces("muted")),
        accent: Math.min(
          onSurfaces("accent"),
          ratio(token("accent"), selected),
        ),
        onAccent: ratio(token("on-accent"), token("accent")),
        status: Math.min(
          ...["success", "warning", "error"].map((name) =>
            Math.min(onSurfaces(name), ratio(token(name), token(`${name}-bg`))),
          ),
        ),
      };
      probe.remove();
      return result;
    });
    test.info().annotations.push({
      type: "contrast",
      description: `${theme.id}: ${JSON.stringify(contrast)}`,
    });
    expect(contrast.text).toBeGreaterThanOrEqual(4.5);
    expect(contrast.control).toBeGreaterThanOrEqual(3);
    expect(contrast.hoverControl).toBeGreaterThanOrEqual(3);
    expect(contrast.fileHint).toBeGreaterThanOrEqual(4.5);
    expect(contrast.statusBorder).toBeGreaterThanOrEqual(3);
    expect(contrast.secondary).toBeGreaterThanOrEqual(4.5);
    expect(contrast.accent).toBeGreaterThanOrEqual(4.5);
    expect(contrast.onAccent).toBeGreaterThanOrEqual(4.5);
    expect(contrast.status).toBeGreaterThanOrEqual(4.5);
  }
});

test("diff header actions use shared muted borders and collapse files directly", async ({
  page,
}) => {
  await page.goto("/");
  const options = page.getByRole("button", {
    name: "View options",
    exact: true,
  });
  await expect(options).toHaveText("");
  // Icon-only actions name themselves through a tooltip, not `title`.
  await expect(options).not.toHaveAttribute("title");
  await options.hover();
  await expect(page.getByText("View options", { exact: true })).toBeVisible();
  await expect(options).toHaveAttribute("data-variant", "ghost");
  await expect(
    page.getByRole("button", { name: "Collapse all files", exact: true }),
  ).toHaveAttribute("data-variant", "ghost");
  const reviewed = reviewedToggle(page).first();
  await expect(reviewed).toHaveText("Reviewed");
  const comments = page.getByRole("button", {
    name: /^Leave review comment on file /,
  });
  await expect(comments.first()).not.toHaveAttribute("title");
  await comments.first().hover();
  await expect(
    page.getByText("Leave review comment on file", { exact: true }),
  ).toBeVisible();
  const bordered = [
    page.getByRole("searchbox", { name: "Filter files" }).locator(".."),
    page.getByRole("group", { name: "Diff layout" }),
    reviewedToggle(page),
    comments,
    page.locator("#reset-reviewed"),
  ];
  for (const theme of ["light", "dark"]) {
    await page
      .locator("html")
      .evaluate(
        (element, value) => element.setAttribute("data-theme", value),
        theme,
      );
    const expected = await page.evaluate(() => {
      const probe = document.createElement("span");
      probe.style.borderColor = "var(--border-muted)";
      document.body.append(probe);
      const color = getComputedStyle(probe).borderColor;
      probe.remove();
      return color;
    });
    for (const controls of bordered) {
      const actual = await controls.evaluateAll((elements) =>
        elements.map((element) => getComputedStyle(element).borderTopColor),
      );
      expect(actual.every((color) => color === expected)).toBe(true);
    }
  }
  await options.click();
  await expect(
    page.getByRole("menuitem", { name: "Collapse all files" }),
  ).toHaveCount(0);
  await page.keyboard.press("Escape");
  await page
    .getByRole("button", { name: "Collapse all files", exact: true })
    .click();
  const viewer = page.locator("#viewer");
  await expect(fileToggle(viewer, { expanded: true })).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Expand all files", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Expand all files", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Collapse all files", exact: true }),
  ).toBeVisible();
  await expect(fileToggle(viewer, { expanded: true }).first()).toBeVisible();
});

test("default themes keep diff tints, numbers and the hatch on the shell surface", async ({
  page,
}) => {
  const colorSchemes: ("light" | "dark")[] = ["light", "dark"];
  const luminance = ([r, g, b]: number[]) => {
    const channel = (value = 0) => {
      const c = value / 255;
      return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
    };
    return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
  };
  const contrast = (a: number[], b: number[]) => {
    const [high, low] = [luminance(a), luminance(b)].sort((x, y) => y - x);
    return ((high ?? 0) + 0.05) / ((low ?? 0) + 0.05);
  };
  await page.setViewportSize({ width: 1440, height: 4_000 });
  for (const colorScheme of colorSchemes) {
    await page.emulateMedia({ colorScheme });
    await page.goto("/");
    const viewer = page.locator("#viewer");
    await expect(viewer).toHaveAttribute("data-code-surface", "shell");
    const buffer = viewer.locator("[data-content-buffer]").first();
    await expect(buffer).toBeAttached();
    for (const type of ["addition", "deletion"]) {
      await expect(
        viewer.locator(`[data-line][data-line-type="change-${type}"]`).first(),
      ).toBeVisible();
    }
    const colors = await viewer.evaluate((element) => {
      const context = document.createElement("canvas").getContext("2d", {
        willReadFrequently: true,
      });
      if (!context) throw new Error("Missing canvas");
      const rgb = (color: string) => {
        context.clearRect(0, 0, 1, 1);
        context.fillStyle = color;
        context.fillRect(0, 0, 1, 1);
        return Array.from(context.getImageData(0, 0, 1, 1).data.slice(0, 3));
      };
      const roots = Array.from(element.querySelectorAll("diffs-container"));
      const find = (selector: string) => {
        for (const root of roots) {
          const match = root.shadowRoot?.querySelector(selector);
          if (match) return getComputedStyle(match);
        }
        throw new Error(`Missing ${selector}`);
      };
      const row = (type: string) => {
        const selector = `[data-line-type="change-${type}"]`;
        const number = find(`[data-column-number]${selector}`);
        return {
          line: rgb(find(`[data-line]${selector}`).backgroundColor),
          numberBackground: rgb(number.backgroundColor),
          number: rgb(number.color),
        };
      };
      // Pierre's dimmest syntax token, its punctuation.
      const punctuation = find(
        '[data-line][data-line-type="change-addition"] span[style*="#636363"]',
      );
      const probe = document.createElement("span");
      probe.style.color = "var(--bg)";
      element.append(probe);
      const surface = getComputedStyle(probe).color;
      probe.remove();
      return {
        surface,
        surfaceRgb: rgb(surface),
        hatch: find("[data-content-buffer]").backgroundImage,
        addition: row("addition"),
        deletion: row("deletion"),
        punctuation: rgb(punctuation.color),
      };
    });
    // Dim syntax stays legible on rows lifted off Pierre's near-black.
    if (colorScheme === "dark")
      expect(
        contrast(colors.punctuation, colors.addition.line),
      ).toBeGreaterThan(2.4);
    // Stripes differ from the surface they draw over.
    expect(colors.hatch).toContain("repeating-linear-gradient");
    expect(colors.hatch).not.toContain(colors.surface);
    for (const { line, numberBackground, number } of [
      colors.addition,
      colors.deletion,
    ]) {
      // Tints mix with the surface, so dark rows lift and light rows sink.
      if (colorScheme === "dark")
        expect(luminance(line)).toBeGreaterThan(luminance(colors.surfaceRgb));
      else expect(luminance(line)).toBeLessThan(luminance(colors.surfaceRgb));
      expect(contrast(number, numberBackground)).toBeGreaterThanOrEqual(4.5);
    }
  }
});

test("a tab strip's actions share the inactive tabs' shell surface", async ({
  page,
}) => {
  await page.goto("/");
  await showDiff(page);
  const header = page.locator('#sidebar [data-slot="panel-header"]');
  const tabs = header.getByRole("tab");
  const shell = await tabs
    .last()
    .evaluate((tab) => getComputedStyle(tab).backgroundColor);
  await expect(tabs.last()).toHaveAttribute("aria-selected", "false");
  await expect(header).toHaveCSS("background-color", shell);
  await expect(header).toHaveCSS("border-bottom-width", "1px");
  await expect(tabs.first()).not.toHaveCSS("background-color", shell);
  const collapse = page.getByRole("button", {
    name: /^(?:Collapse|Expand) all folders$/,
  });
  await expect(collapse).toHaveCSS("border-top-color", "rgba(0, 0, 0, 0)");
  await expect(tabs.last()).toHaveCSS("border-right-width", "1px");
  await expect(tabs.last()).toHaveCSS("border-bottom-width", "1px");
  await tabs.last().click();
  await expect(tabs.last()).toHaveAttribute("aria-selected", "true");
  await expect(tabs.last()).toHaveCSS("border-right-width", "1px");
  await expect(tabs.last()).toHaveCSS("border-bottom-width", "0px");
  await expect(header).toHaveCSS("background-color", shell);
});

test.describe("iOS-standard phone chrome", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  async function box(page: Page, selector: string) {
    const bounds = await page.locator(selector).first().boundingBox();
    if (!bounds) throw new Error(`${selector} is not rendered`);
    return bounds;
  }

  test("header and status bar use 44px rows, targets and iOS glyph scale", async ({
    page,
  }) => {
    await page.goto("/");
    await expect(page.locator(".main-footer")).toBeVisible();

    await expect(page.locator('meta[name="viewport"]')).toHaveAttribute(
      "content",
      /viewport-fit=cover/,
    );
    expect((await box(page, ".topbar")).height).toBe(44);
    // A 44px row below the status bar's 1px top hairline.
    expect((await box(page, ".main-footer")).height).toBe(1 + 44);

    const controls = page.locator(
      ".topbar > button:visible, .header-actions button:visible, .main-footer button:visible",
    );
    expect(await controls.count()).toBeGreaterThan(3);
    for (const control of await controls.all()) {
      const bounds = await control.boundingBox();
      if (!bounds) throw new Error("Control is not rendered");
      expect(bounds.height).toBeGreaterThanOrEqual(44);
    }
    for (const selector of [
      ".topbar > button:visible svg",
      ".main-footer .footer-details button:visible svg",
    ])
      expect((await box(page, selector)).width).toBe(20);
    await expect(page.locator(".topbar .context-identity")).toHaveCSS(
      "font-size",
      "15px",
    );
    await expect(page.locator(".main-footer")).toHaveCSS("font-size", "13px");
  });

  test("chrome clears the status bar, home indicator and drawer insets", async ({
    page,
  }) => {
    await page.goto("/");
    await expect(page.locator(".main-footer")).toBeVisible();
    // Chromium has no device insets; stand in for an iPhone's.
    await page.addStyleTag({
      content: ":root { --safe-top: 47px; --safe-bottom: 34px; }",
    });

    const topbar = await box(page, ".topbar");
    expect(topbar.y).toBe(0);
    expect(topbar.height).toBe(44 + 47);
    // Safari 26 samples only fixed or sticky edge elements for its status bar
    // color; a static header gets blurred instead.
    await expect(page.locator(".topbar")).toHaveCSS("position", "sticky");
    await expect(page.locator(".topbar")).toHaveCSS("top", "0px");
    const footer = await box(page, ".main-footer");
    expect(footer.height).toBe(1 + 44 + 34);
    expect(footer.y + footer.height).toBe(844);
    expect(
      (await box(page, ".main-footer .footer-details button:visible")).y + 44,
    ).toBeLessThanOrEqual(844 - 34);

    await page.getByRole("button", { name: "Show file sidebar" }).click();
    await expect(page.locator("#sidebar")).toHaveCSS("padding-top", "47px");
    await expect(page.locator(".sidebar-footer")).toHaveCSS(
      "padding-bottom",
      "34px",
    );
  });

  test("the browser chrome color follows the active theme's canvas", async ({
    page,
  }) => {
    await page.addInitScript(() => localStorage.setItem("theme-v2", "dark"));
    await page.goto("/");
    await expect(page.locator(".topbar")).toBeVisible();
    const canvas = await page.evaluate(() =>
      getComputedStyle(document.documentElement)
        .getPropertyValue("--shell-canvas")
        .trim(),
    );
    expect(canvas).not.toBe("#e2e2e3");
    await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute(
      "content",
      canvas,
    );
  });
});
