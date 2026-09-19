// Signing in to the application itself.
//
// Read tests/oidc-provider.spec.ts first: it proves the provider side works.
// What is missing is the application side. internal/auth implements the whole
// authorization-code flow, but cmd/server's newRouter mounts /healthz, / and
// /static/ and nothing else - there is no handler on /auth/login, and there is
// no session package for a callback to hand an identity to (internal/auth's
// own doc comment says so: "It deliberately does not create sessions (C3)").
//
// So the journey below is skipped, and the trip-wire above it is what makes
// that skip honest: it asserts the state of the world that justifies the skip,
// and goes red the day that state changes.

import { expect, test } from "@playwright/test";
import * as fs from "node:fs";

import { harnessStatePath, type HarnessState } from "../lib/stack";

function harnessState(): HarnessState {
  return JSON.parse(fs.readFileSync(harnessStatePath, "utf8")) as HarnessState;
}

test.describe("sign-in", () => {
  test("is not mounted yet - delete this test when nap-9jw lands", async ({ request }) => {
    // TRIP-WIRE. This asserts what IS true, not what SHOULD be. When the
    // router grows /auth/login this test fails, and that failure is the
    // instruction: delete this test, unskip the journey below, and the global
    // setup starts saving a real session instead of an empty one.
    const login = await request.get("/auth/login", { maxRedirects: 0 });
    const callback = await request.get("/auth/callback", { maxRedirects: 0 });

    expect(login.status(), "GET /auth/login").toBe(404);
    expect(callback.status(), "GET /auth/callback").toBe(404);

    // And the global setup reached the same conclusion. Without this the setup
    // could quietly decide auth was wired, skip the sign-in it could not
    // perform, and leave every session-dependent spec skipping for a reason
    // nobody had checked.
    expect(harnessState()).toMatchObject({ authWired: false, loginStatus: 404, email: null });
  });

  test("through the mock provider establishes a session [skipped until nap-9jw]", async ({
    page,
    context,
  }) => {
    test.skip(
      !harnessState().authWired,
      "nap-9jw: cmd/server does not mount /auth/login or a session store yet",
    );

    // The shape this takes once the route exists. The global setup already
    // performs exactly this and saves the result, so by the time this runs it
    // is re-proving the flow rather than establishing it.
    await page.goto("/auth/login");
    await page.waitForURL((url) => !url.pathname.startsWith("/auth/"));

    const cookies = await context.cookies(page.url());
    expect(cookies.length).toBeGreaterThan(0);
    expect(cookies.some((c) => c.httpOnly)).toBe(true);
  });

  test("is refused for an address that is not on the season whitelist [skipped until nap-l1i]", async ({
    page,
  }) => {
    test.skip(true, "nap-l1i: there is no whitelist check, and no page that reports being refused");

    // queueUser({ subject: "stranger", email: "not-invited@example.test" });
    // await page.goto("/auth/login");
    // await expect(page.getByText(/not on the list/i)).toBeVisible();
    expect(page).toBeTruthy();
  });
});
