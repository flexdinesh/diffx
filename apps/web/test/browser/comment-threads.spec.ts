import { createApiClient } from "@diffx/api";
import { expect, test } from "./fixtures.ts";
import { resetFixtureState } from "./fixture-state.ts";

const PATH = "config/diffx.json";

test.beforeEach(async ({ request }) => resetFixtureState(request));

/** Stores a file comment through the API, as another writer would. */
async function seedComment(baseURL: string | undefined) {
  if (!baseURL) throw new Error("Missing test URL");
  const client = createApiClient({ baseUrl: baseURL });
  const catalog = await client.GET("/api/v2/contexts");
  const contextId = catalog.data?.contexts[0]?.id;
  if (!contextId) throw new Error("Missing fixture context");
  const { data: diff } = await client.GET(
    "/api/v2/contexts/{contextId}/diffs/current",
    { params: { path: { contextId }, query: { scope: "all" } } },
  );
  const file = diff?.files.find((entry) => entry.path === PATH);
  if (!diff || !file) throw new Error("Missing fixture file");
  const { data: comment } = await client.POST(
    "/api/v2/contexts/{contextId}/comments",
    {
      params: { path: { contextId } },
      body: {
        diffId: diff.id,
        versionId: diff.versionId,
        fileId: file.id,
        fileVersion: file.fingerprint,
        scope: "all",
        target: "file",
        side: "additions",
        start: 0,
        end: 0,
        body: "Check the config",
      },
    },
  );
  if (!comment) throw new Error("Unable to seed comment");
  return { client, contextId, commentId: comment.id };
}

test("comment threads reply, edit, delete and follow other writers live", async ({
  page,
  baseURL,
}) => {
  const { client, contextId, commentId } = await seedComment(baseURL);
  await page.goto("/");
  const card = page
    .locator('#viewer [data-comment-id] [data-slot="card"]')
    .filter({ hasText: "Check the config" });
  await expect(card.locator('[data-slot="card-title"]')).toContainText("You");

  const reply = card.getByRole("button", { name: "Reply", exact: true });
  await reply.click();
  const composer = card.getByRole("textbox", { name: /^Reply to comment/ });
  await expect(composer).toBeFocused();
  await composer.press("Escape");
  await expect(composer).toBeHidden();
  await expect(reply).toBeFocused();

  await reply.click();
  await composer.fill("Why this default?");
  await composer.press("ControlOrMeta+Enter");
  const thread = card.getByRole("list", { name: "1 reply" });
  await expect(thread).toContainText("Why this default?");
  await expect(composer).toBeHidden();
  await expect(reply).toBeFocused();

  await thread.getByRole("button", { name: "Edit reply" }).click();
  const editor = thread.getByRole("textbox", { name: "Edit reply" });
  await expect(editor).toBeFocused();
  await editor.fill("Why this default value?");
  await thread.getByRole("button", { name: "Save", exact: true }).click();
  await expect(thread).toContainText("Why this default value?");
  await expect(thread).toContainText("edited");

  // Another writer's reply arrives through the event stream, well before the
  // 3s poll, and leaves an open composer's text alone.
  await reply.click();
  await composer.fill("Half-written");
  const { error } = await client.POST(
    "/api/v2/contexts/{contextId}/comments/{commentId}/replies",
    {
      params: { path: { contextId, commentId } },
      body: { body: "From another tab" },
    },
  );
  expect(error).toBeUndefined();
  const both = card.getByRole("list", { name: "2 replies" });
  await expect(both).toContainText("From another tab", { timeout: 2_000 });
  await expect(composer).toHaveValue("Half-written");
  await composer.press("Escape");

  await both
    .getByRole("listitem")
    .first()
    .getByRole("button", { name: "Delete reply" })
    .click();
  await page
    .getByRole("dialog", { name: "Delete reply?" })
    .getByRole("button", { name: "Delete reply" })
    .click();
  await expect(card.getByRole("list", { name: "1 reply" })).toContainText(
    "From another tab",
  );

  await card.getByRole("button", { name: "Resolve", exact: true }).click();
  await expect(card).toContainText("Resolved by You");
});

test("comment links copy a permalink that opens and pulses the comment", async ({
  page,
  baseURL,
}) => {
  const { contextId, commentId } = await seedComment(baseURL);
  await page.addInitScript(() => {
    Object.defineProperty(Navigator.prototype, "clipboard", {
      configurable: true,
      get: () => ({
        writeText: (text: string) => {
          Reflect.set(window, "copiedText", text);
          return Promise.resolve();
        },
      }),
    });
  });
  await page.goto("/");
  const card = page
    .locator('#viewer [data-comment-id] [data-slot="card"]')
    .filter({ hasText: "Check the config" });
  await card
    .getByRole("button", { name: `Copy link to comment at ${PATH}` })
    .click();
  const link = await page.evaluate(() => Reflect.get(window, "copiedText"));
  expect(link).toBe(
    `${new URL(page.url()).origin}/contexts/${contextId}?comment=${commentId}`,
  );
  if (typeof link !== "string") throw new Error("Missing copied link");

  await page.goto(link);
  await expect(
    page
      .locator("#viewer diffs-container")
      .filter({ hasText: PATH })
      .locator("[data-diffs-header][data-navigation-target]"),
  ).toBeAttached();
  await expect(card).toBeInViewport();
  // The link is consumed so scope changes and reloads do not repeat it.
  await expect(page).toHaveURL(new RegExp(`/contexts/${contextId}$`));

  await page.goto(`/contexts/${contextId}?comment=missing`);
  await expect(
    page.getByText("The linked comment is no longer in this review."),
  ).toBeAttached();
  await expect(page.locator("#viewer")).toBeVisible();
});

test("agent authors carry their reported name and the panel filters by author", async ({
  page,
  baseURL,
}) => {
  const { client, contextId, commentId } = await seedComment(baseURL);
  const { data } = await client.GET("/api/v2/contexts/{contextId}/comments", {
    params: { path: { contextId } },
  });
  const seeded = data?.comments.find((comment) => comment.id === commentId);
  if (!seeded) throw new Error("Missing seeded comment");
  // Imports stand in for an agent writer until agents write through MCP.
  const imported = await client.POST(
    "/api/v2/contexts/{contextId}/comments/import",
    {
      params: { path: { contextId } },
      body: {
        comments: [
          {
            ...seeded,
            id: "agent-comment",
            body: "Port collides with Vite",
            author: { kind: "agent", name: "Claude Code" },
          },
        ],
      },
    },
  );
  expect(imported.error).toBeUndefined();
  await page.goto("/");
  const agentCard = page
    .locator('#viewer [data-comment-id="agent-comment"] [data-slot="card"]')
    .first();
  const author = agentCard
    .locator('[data-slot="card-title"]')
    .getByTitle("Name reported by the agent");
  await expect(author).toHaveText("Claude Code (Name reported by the agent)");
  await expect(author.locator("svg")).toHaveCount(1);
  // Agent comments are append-only: replies answer them, edits do not exist.
  await expect(
    agentCard.getByRole("button", { name: /^Edit comment at / }),
  ).toHaveCount(0);
  await expect(
    agentCard.getByRole("button", { name: "Reply", exact: true }),
  ).toBeVisible();

  await page.getByRole("tab", { name: /Review/ }).click();
  const panel = page.locator("#comments-panel");
  const filter = panel.getByRole("button", { name: /^Filter comments: / });
  await expect(panel.locator("[data-comment-id]")).toHaveCount(2);
  // The author filter shares the status menu, after the status choices.
  await filter.click();
  const menu = page.getByRole("menu", { name: "Filter comments" });
  await expect(
    menu.getByRole("menuitemradio", { name: "Everyone 2" }),
  ).toHaveAttribute("aria-checked", "true");
  await menu.getByRole("menuitemradio", { name: "Agents 1" }).click();
  await expect(filter).toHaveAccessibleName("Filter comments: Open, Agents");
  await expect(filter).toContainText("Open · Agents");
  await expect(panel.locator("[data-comment-id]")).toHaveCount(1);
  await expect(panel).toContainText("Port collides with Vite");
  await filter.click();
  await menu.getByRole("menuitemradio", { name: "Me 1" }).click();
  await expect(panel.locator("[data-comment-id]")).toHaveCount(1);
  await expect(panel).toContainText("Check the config");
});
