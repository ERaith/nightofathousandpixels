// Seeing what everyone put up.
//
// The board is the public half of this site: anybody with the link can read
// it, which is the whole reason the link gets pasted into a group chat. So the
// journeys here are about what a visitor sees, and the one that matters most
// is the one where the visitor has never signed in.
//
// These tests put their own film up rather than leaning on submit.spec.ts.
// Playwright runs files in alphabetical order, so `slate` runs BEFORE `submit`
// and a board full of that file's work is not available here - and a test that
// silently depended on it would pass locally, in file order, and fail the
// moment somebody ran one file on its own.

import { expect, test, type Page } from "@playwright/test";

import { member } from "../lib/people";
import { signedOut, signInAs } from "../lib/session";
import { assertionsNotWrittenYet } from "../lib/unwritten";

const TRAILER = "https://example.test/slate-trailer";

/** Per-project, because both projects share one board. See submit.spec.ts. */
function film(project: string) {
  return {
    title: `Near Dark [${project}]`,
    year: "1987",
    line: `Near Dark [${project}] (1987)`,
    why: "Vampires in a pickup truck.",
  };
}

function card(page: Page, titleLine: string) {
  return page
    .getByRole("listitem")
    .filter({ has: page.getByRole("heading", { level: 3, name: titleLine, exact: true }) });
}

test.describe.serial("the slate", () => {
  test("lists a film that was just submitted, with who put it up", async ({ page }, testInfo) => {
    const project = testInfo.project.name;
    const who = member("slate", project);
    const it = film(project);

    await signInAs(page, who);
    await page.goto("/submit");
    await page.getByLabel("Title").fill(it.title);
    await page.getByLabel("Year").fill(it.year);
    await page.getByLabel("Trailer link").fill(TRAILER);
    await page.getByLabel("Why this one").fill(it.why);
    await page.getByRole("button", { name: "Put it on the board" }).click();
    await page.waitForURL(/\/slate\?added=/);

    // Arriving at the board is not the same as being on it, so the assertions
    // are against the page a visitor would load cold.
    const slate = await page.goto("/slate");
    expect(slate?.status()).toBe(200);

    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await expect(page.getByText("Submissions open")).toBeVisible();

    const theFilm = card(page, it.line);
    await expect(theFilm).toBeVisible();
    await expect(theFilm).toContainText(`Submitted by ${who.name}`);
    await expect(theFilm).toContainText(it.why);
    await expect(theFilm.getByRole("link", { name: /watch the trailer/i })).toHaveAttribute(
      "href",
      TRAILER,
    );
  });

  test("reads as one column on a phone", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "mobile", "runs in the mobile project only");
    const it = film(testInfo.project.name);

    await page.setViewportSize({ width: 320, height: 720 });
    await page.goto("/slate");

    const theFilm = card(page, it.line);
    await expect(theFilm).toBeVisible();

    // One column means the card is as wide as the space it is in, give or take
    // the grid's own gutter. A two-column grid at 320px would halve it.
    const box = await theFilm.boundingBox();
    expect(box, "the card has no box").not.toBeNull();
    expect(box!.width).toBeGreaterThan(240);
    expect(box!.x + box!.width).toBeLessThanOrEqual(320);

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow, "the board scrolls sideways at 320px").toBeLessThanOrEqual(0);
  });
});

// The state most people are in: they followed a link, they have never signed
// in, and the board has to be readable anyway.
test.describe("the board, signed out", () => {
  test.use({ storageState: signedOut });

  test("shows the films to a visitor who has not signed in", async ({ page, context }, testInfo) => {
    const it = film(testInfo.project.name);

    // Assert the premise rather than trusting the fixture: a test that thinks
    // it is signed out while carrying a session proves nothing about either.
    expect(await context.cookies()).toHaveLength(0);

    const slate = await page.goto("/slate");
    expect(slate?.status()).toBe(200);

    await expect(card(page, it.line)).toBeVisible();
    await expect(card(page, it.line)).toContainText(`Submitted by ${member("slate", testInfo.project.name).name}`);

    // Public to read, not to write: the way in is offered, the form is not.
    await expect(page.getByRole("link", { name: "Sign in to add yours" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Put a movie up" })).toHaveCount(0);
    await expect(
      page.getByText("Anyone can read the board. Only this year's group can add to it."),
    ).toBeVisible();
  });
});

test.describe("withdrawn films", () => {
  // Still skipped, and the reason has changed: nap-nws and nap-1s9 have landed,
  // so the page and the handler exist, and `hidden` is already filtered out of
  // ListVisibleMoviesWithSubmitterForSeason. What does not exist is any way for
  // a BROWSER to withdraw a film - there is no admin route, no control on the
  // card, and nothing on any page that sets movie.hidden. So the only honest
  // versions of this test are one that reaches past the site into the database
  // to set up its own fixture, which is not what this directory does, and one
  // that waits for the route.
  //
  // nap-8yw is the live bug that a hidden movie is still rankable; whatever
  // fixes it needs this test, and will bring the route that makes it writable.
  const NEEDS_WITHDRAW = "nap-8yw: nothing in the site can hide a film yet, so there is no way to make one";

  test("a withdrawn film is not on the public board [skipped until nap-8yw]", async () => {
    test.skip(true, NEEDS_WITHDRAW);
    assertionsNotWrittenYet(NEEDS_WITHDRAW);
  });
});
