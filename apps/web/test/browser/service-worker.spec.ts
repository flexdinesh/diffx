import { expect, test } from "./fixtures.ts";

// The headless shell denies notification permission; full Chromium grants
// it. Chromium's own notification center avoids waiting on the desktop's.
test.use({
  channel: "chromium",
  launchOptions: { args: ["--disable-features=NativeNotifications"] },
});

test("the service worker shows pushed pull requests and a fallback for unreadable pushes", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["notifications"]);
  // A signed-out remote server still installs the app shell.
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "remote",
        publicPullRequests: false,
        googleEnabled: false,
        signupEnabled: false,
        passwordSignupEnabled: false,
        user: null,
        csrfToken: null,
      },
    }),
  );
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await page.evaluate(() => navigator.serviceWorker.ready);
  const cdp = await context.newCDPSession(page);
  const registered = new Promise<string>((resolve) => {
    cdp.on("ServiceWorker.workerRegistrationUpdated", ({ registrations }) => {
      const registration = registrations.find((item) => !item.isDeleted);
      if (registration) resolve(registration.registrationId);
    });
  });
  await cdp.send("ServiceWorker.enable");
  const registrationId = await registered;
  const origin = new URL(page.url()).origin;
  const notifications = () =>
    page.evaluate(async () => {
      const registration = await navigator.serviceWorker.ready;
      return (await registration.getNotifications()).map((notification) => ({
        title: notification.title,
        body: notification.body,
        tag: notification.tag,
        data: notification.data,
      }));
    });
  await cdp.send("ServiceWorker.deliverPushMessage", {
    origin,
    registrationId,
    data: JSON.stringify({
      kind: "pull_request",
      title: "octo/app #12",
      body: "Add login",
      url: "/streams/s1",
      tag: "pr-12",
      timestamp: 1_790_000_000_000,
    }),
  });
  await expect.poll(notifications).toEqual([
    {
      title: "octo/app #12",
      body: "Add login",
      tag: "pr-12",
      data: { url: "/streams/s1" },
    },
  ]);
  await cdp.send("ServiceWorker.deliverPushMessage", {
    origin,
    registrationId,
    data: "not json",
  });
  await expect
    .poll(async () => (await notifications()).map((item) => item.title))
    .toContain("New pull request diff");
});
