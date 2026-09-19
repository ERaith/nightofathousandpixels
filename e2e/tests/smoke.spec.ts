// Does the stack actually serve the site?
//
// These are the tests that have to be true before any other test in this
// directory means anything. They assert against what exists today - the
// placeholder front page, /healthz, and the site's own 404 - so they are also
// the ones that prove the harness can tell "the page is wrong" from "nothing
// answered".

import { expect, test } from "@playwright/test";

import { stack } from "../lib/stack";

/** internal/web/templates.DefaultSiteTitle. */
const SITE_TITLE = "Night of a Thousand Pixels";

test.describe("the site is served", () => {
  test("/ renders the front page", async ({ page }) => {
    const response = await page.goto("/");

    expect(response?.status()).toBe(200);
    expect(response?.headers()["content-type"]).toContain("text/html");

    await expect(page).toHaveTitle(SITE_TITLE);
    await expect(page.getByRole("heading", { level: 1, name: SITE_TITLE })).toBeVisible();

    // Real HTML, not a framework shell that fills itself in later: the page is
    // server-rendered, so the three steps are in the response body.
    await expect(page.getByRole("heading", { level: 3, name: "Submit" })).toBeVisible();
    await expect(page.getByRole("heading", { level: 3, name: "Rank" })).toBeVisible();
    await expect(page.getByRole("heading", { level: 3, name: "Watch" })).toBeVisible();
  });

  test("/healthz reports the database is reachable", async ({ request }) => {
    const response = await request.get("/healthz");

    expect(response.status()).toBe(200);
    expect(await response.json()).toMatchObject({ status: "ok" });
  });

  test("an unknown path gets the site's own 404, not Go's", async ({ page }) => {
    const response = await page.goto("/no-such-page");

    expect(response?.status()).toBe(404);
    expect(response?.headers()["content-type"]).toContain("text/html");
    await expect(page).toHaveTitle(new RegExp(SITE_TITLE));
  });

  test("the stylesheet the layout depends on is served", async ({ request }) => {
    // base.css loading second is the whole theme contract (see layout.templ).
    // A 404 here would not break the page visibly enough to notice by eye.
    const response = await request.get("/static/css/base.css");

    expect(response.status()).toBe(200);
    expect(response.headers()["content-type"]).toContain("css");
  });
});

// Most voters will be on a phone, so this is not a bonus check. Both tests
// below run only in the mobile project, and report as an explicit skip in the
// desktop one rather than quietly not existing there.
const MOBILE_ONLY = "runs in the mobile project only";

test.describe("on a phone", () => {
  test("the front page does not scroll sideways", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "mobile", MOBILE_ONLY);

    await page.goto("/");

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(0);
  });

  test("the front page survives a 320px-wide screen", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "mobile", MOBILE_ONLY);

    // 320px is the narrowest phone still in use. base.css is written for it.
    await page.setViewportSize({ width: 320, height: 720 });
    await page.goto("/");

    await expect(page.getByRole("heading", { level: 1, name: SITE_TITLE })).toBeVisible();
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(0);
  });
});

test("the suite is pointed at this agent's own stack", async ({ request }) => {
  // Cheap, but it is the check that catches a suite quietly testing another
  // agent's application on a neighbouring port.
  const response = await request.get(`${stack().baseURL}/healthz`);
  expect(response.status()).toBe(200);
});
