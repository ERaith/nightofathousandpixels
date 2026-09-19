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

import { stranger } from "../lib/people";
import { signInAs } from "../lib/session";
import { harnessStatePath, type HarnessState } from "../lib/stack";

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

  test("succeeds for an address that is not on the season whitelist, and gets no further", async ({
    page,
    context,
  }) => {
    // nap-l1i has landed. The skip that stood here is gone rather than left to
    // disable itself, for the same reason as the one above.
    //
    // The shape of this is the finding, not the refusal: sign-in SUCCEEDS.
    // The provider vouches for the address, the person row is created, the
    // session cookie is issued - and then a separate gate, in front of the
    // pages that do something, says no. That is why somebody removed from the
    // whitelist stops being able to act on their next click rather than when
    // their cookie expires, and why the refusal page can greet them by name
    // and offer them a way to sign out of the wrong Google account.
    await signInAs(page, stranger);

    const cookies = await context.cookies(page.url());
    expect(cookies.length, "a refused visitor still gets a session").toBeGreaterThan(0);

    await expect(page.getByRole("heading", { level: 1, name: "You're not on the list yet" })).toBeVisible();

    // The address they arrived with, so somebody who signed in with the wrong
    // Google account can see which one it was. It is also the address an admin
    // needs, and the page says to send exactly it.
    await expect(page.getByText(stranger.email, { exact: false }).first()).toBeVisible();

    // Not a 403, on purpose. This is a friend who was never added, not an
    // intruder, and there is something for them to do about it.
    const again = await page.goto("/me");
    expect(again?.status()).toBe(200);
    await expect(page.getByRole("button", { name: "Sign out" })).toBeVisible();
  });
});
