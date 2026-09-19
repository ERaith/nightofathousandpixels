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
import { assertionsNotWrittenYet } from "../lib/unwritten";

function harnessState(): HarnessState {
  return JSON.parse(fs.readFileSync(harnessStatePath, "utf8")) as HarnessState;
}

test.describe("sign-in", () => {
  test("through the mock provider establishes a session", async ({ page, context }) => {
    // nap-9jw has landed, so the skip guard that used to stand here is gone
    // rather than left to disable itself. It was conditional on
    // harnessState().authWired, which is now always true - and a guard that can
    // only ever pass is indistinguishable, in a report, from one silently
    // hiding a broken sign-in. If /auth/login regresses this must go red, not
    // green with a skip. That failure mode has already cost this project eight
    // false greens (nap-gn1, nap-hil).
    expect(harnessState().authWired, "harness did not sign in").toBe(true);

    // The global setup already performs exactly this and saves the result, so
    // by the time this runs it is re-proving the flow rather than establishing
    // it.
    await page.goto("/auth/login");
    await page.waitForURL((url) => !url.pathname.startsWith("/auth/"));

    const cookies = await context.cookies(page.url());
    expect(cookies.length).toBeGreaterThan(0);
    expect(cookies.some((c) => c.httpOnly)).toBe(true);
  });

  test("is refused for an address that is not on the season whitelist [skipped until nap-l1i]", async () => {
    const reason = "nap-l1i: there is no whitelist check, and no page that reports being refused";
    test.skip(true, reason);

    // queueUser({ subject: "stranger", email: "not-invited@example.test" });
    // await page.goto("/auth/login");
    // await expect(page.getByText(/not on the list/i)).toBeVisible();
    assertionsNotWrittenYet(reason);
  });
});
