// Signing in inside a test, through the real flow.
//
// The global setup signs in once and saves the result, which is right for a
// suite where every test is the same person. This one is not: the journeys
// that write need a person each (see lib/people.ts), and a saved state is one
// file shared by both projects.
//
// So a journey that cares who it is drives the flow itself. That is not a
// second, weaker way in - it is the same three lines the global setup runs,
// against the same provider, exercising the same discovery, PKCE, code
// exchange and RS256 verification. There is no bypass here either.

import { expect, type Page } from "@playwright/test";

import { queueUser, type TestUser } from "./oidc";

/** Storage state for a test that must be signed out. */
export const signedOut = { cookies: [], origins: [] };

/**
 * Sign `page` in as `user` and return once the application has taken over.
 *
 * `queueUser` decides which address the provider will sign a token for - the
 * equivalent of clicking an account on Google's chooser. It grants nothing:
 * the application still verifies the token, and still decides for itself
 * whether that address is on the season's whitelist. So this is usable for
 * somebody who is about to be refused, and the refusal journeys use it.
 *
 * The wait is for any path outside /auth/, because where a sign-in lands
 * depends on who signed in: a member reaches /me, and somebody who is not on
 * the list reaches the same URL and is shown the refusal. Waiting for a
 * specific page would make this helper decide the outcome it is meant to
 * observe.
 */
export async function signInAs(page: Page, user: TestUser): Promise<void> {
  await queueUser(user);
  await page.goto("/auth/login");
  await page.waitForURL((url) => !url.pathname.startsWith("/auth/"), { timeout: 15_000 });

  // A sign-in that reached a page but set no cookie is the failure mode this
  // helper must not paper over: every assertion after it would then be about
  // an anonymous visitor, and "the submit form is not here" would look like a
  // finding rather than like a broken helper.
  const cookies = await page.context().cookies(page.url());
  expect(cookies.length, `signing in as ${user.email} set no cookie`).toBeGreaterThan(0);
}
