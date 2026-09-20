// Putting a movie on the board.
//
// This is the journey the whole site exists for: somebody signs in with
// Google, types a film in, and it is on the board with their name under it.
// It is driven here exactly as a person drives it - a real browser, the real
// OIDC provider, the real form - so nothing below knows anything about the
// application that a visitor could not find out by looking at a page.
//
// ---------------------------------------------------------------------------
// Why each test signs in for itself
// ---------------------------------------------------------------------------
//
// The global setup saves one session, and both Playwright projects share it.
// These journeys WRITE, and a voter's two picks are spent for good: a run that
// used them in the mobile project would leave the desktop project asserting
// against a cap that was already reached before it started, which passes and
// proves nothing. So every journey here signs in as a person of its own -
// derived from the project name in lib/people.ts, and whitelisted by the seed
// from the same list. See that file for the long version.
//
// The order inside the serial block is the order of one person's afternoon,
// and it is load-bearing: the cap test needs the two submissions above it to
// have happened, and the validation test needs them NOT to have happened yet
// (a voter at the cap gets no form at all, so there would be no boxes for the
// typed values to come back in). test.describe.serial is what says so, and
// what stops a failure at the top reporting as four unrelated failures.

import { expect, test, type Page } from "@playwright/test";

import { member, stranger } from "../lib/people";
import { signInAs } from "../lib/session";

/**
 * Titles are per-project so the two projects cannot read each other's board.
 * One application in front of one database means the mobile run's films are
 * still there when the desktop run starts, and an assertion that matched
 * either one would pass for the wrong reason.
 */
function films(project: string) {
  return {
    first: { title: `Predator [${project}]`, year: "1987", line: `Predator [${project}] (1987)` },
    second: { title: `The Thing [${project}]`, year: "1982", line: `The Thing [${project}] (1982)` },
    third: { title: `Aliens [${project}]`, year: "1986", line: `Aliens [${project}] (1986)` },
  };
}

const TRAILER = "https://example.test/trailer";

/** The card for one film on whatever board is on screen. */
function card(page: Page, titleLine: string) {
  return page
    .getByRole("listitem")
    .filter({ has: page.getByRole("heading", { level: 3, name: titleLine, exact: true }) });
}

/** Fill the form in and send it. Returns the POST's response. */
async function submitFilm(page: Page, film: { title: string; year: string }) {
  await page.goto("/submit");
  await page.getByLabel("Title").fill(film.title);
  await page.getByLabel("Year").fill(film.year);
  await page.getByLabel("Trailer link").fill(TRAILER);

  const [response] = await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "POST" && new URL(r.url()).pathname === "/submit",
    ),
    page.getByRole("button", { name: "Put it on the board" }).click(),
  ]);

  return response;
}

test.describe.serial("submitting a movie", () => {
  test("a bad submission is refused and every value comes back exactly as typed", async ({
    page,
  }, testInfo) => {
    await signInAs(page, member("submit", testInfo.project.name));
    await page.goto("/submit");

    // The form is here, and this person has not spent anything yet. Both
    // matter to the tests below: if either were false the rest of this file
    // would be asserting against a page with no form on it.
    await expect(page.getByRole("heading", { level: 1, name: "Put a movie on the board" })).toBeVisible();
    await expect(page.getByText("2 picks left of 2.")).toBeVisible();

    const title = page.getByLabel("Title");
    const year = page.getByLabel("Year");
    const trailer = page.getByLabel("Trailer link");

    await title.fill("   "); // blank once trimmed, and visible on the way back
    await year.fill("nineteen eighty-four");
    await trailer.fill("not-a-url");

    // The browser's own check is the first line of defence and it is on: year
    // carries pattern="[0-9]{4}" and trailer is type="url", so clicking the
    // button here would not produce a request at all. That is worth asserting
    // rather than working silently around - a form that lost its native
    // validation would still pass every assertion below.
    expect(await year.evaluate((el) => (el as HTMLInputElement).checkValidity())).toBe(false);
    expect(await trailer.evaluate((el) => (el as HTMLInputElement).checkValidity())).toBe(false);

    // The server's check is the one that counts, and it is the only one a
    // hand-built request would ever meet. Turning off novalidate is how a
    // browser gets to make that request; the values in the boxes are
    // untouched, so this posts exactly what was typed above.
    await page.locator("form[method='post']").evaluate((f) => {
      (f as HTMLFormElement).noValidate = true;
    });

    const [response] = await Promise.all([
      page.waitForResponse(
        (r) => r.request().method() === "POST" && new URL(r.url()).pathname === "/submit",
      ),
      page.getByRole("button", { name: "Put it on the board" }).click(),
    ]);

    // 422: understood, well-formed, and refused. A 200 here would mean a test
    // could not tell a rejected submission from an accepted one.
    expect(response.status()).toBe(422);

    const summary = page.locator("#submit-errors");
    await expect(summary).toBeVisible();
    await expect(summary).toContainText("3 things to fix before this goes up.");

    // Beside each control, in the control's own error element - and that
    // element is the one the input names in aria-describedby, which is what
    // makes the message reach somebody who cannot see where it is on screen.
    // Colour is never the only signal here, so the text is what is checked.
    const errors: [string, string][] = [
      ["submit-title", "A film needs a title — it is the one part we cannot guess."],
      ["submit-year", "That doesn't look like a year. Four digits, like 1982 — or leave it blank."],
      [
        "submit-trailer-url",
        "A trailer link has to be a web address starting with http:// or https://.",
      ],
    ];
    for (const [id, message] of errors) {
      await expect(page.locator(`#${id}-error`)).toHaveText(message);
      await expect(page.locator(`#${id}`)).toHaveAttribute("aria-invalid", "true");
      await expect(page.locator(`#${id}`)).toHaveAttribute(
        "aria-describedby",
        new RegExp(`\\b${id}-error\\b`),
      );
      // And the summary at the top links straight to the control, which is
      // what makes a long form recoverable without hunting for the red.
      await expect(summary.locator(`a[href="#${id}"]`)).toHaveCount(1);
    }

    // The point of the journey: nothing was tidied, parsed or dropped on the
    // way back. Somebody who mistyped one field does not retype the other two.
    await expect(title).toHaveValue("   ");
    await expect(year).toHaveValue("nineteen eighty-four");
    await expect(trailer).toHaveValue("not-a-url");

    // And a refusal is not a submission: the quota is untouched.
    await page.goto("/submit");
    await expect(page.getByText("2 picks left of 2.")).toBeVisible();
  });

  test("a signed-in voter submits a film and it is on the board under their name", async ({
    page,
  }, testInfo) => {
    const project = testInfo.project.name;
    const who = member("submit", project);
    const film = films(project).first;

    await signInAs(page, who);

    const response = await submitFilm(page, film);

    // POST-redirect-GET: the browser ends up on the slate, so a refresh
    // cannot put the same film up twice.
    expect(response.status()).toBe(303);
    await page.waitForURL(/\/slate\?added=/);

    await expect(page.getByRole("status")).toContainText(`Done: ${film.line} is on the board.`);

    const theFilm = card(page, film.line);
    await expect(theFilm).toBeVisible();
    await expect(theFilm).toContainText(`Submitted by ${who.name}`);
    await expect(theFilm.getByRole("link", { name: /watch the trailer/i })).toHaveAttribute(
      "href",
      TRAILER,
    );

    // One pick spent, on the board, counted.
    await expect(page.getByText("1 pick left of 2.")).toBeVisible();
  });

  test("a second submission is accepted and the board shows both", async ({ page }, testInfo) => {
    const project = testInfo.project.name;
    const who = member("submit", project);
    const { first, second } = films(project);

    await signInAs(page, who);

    const response = await submitFilm(page, second);
    expect(response.status()).toBe(303);
    await page.waitForURL(/\/slate\?added=/);

    // Both, not just the new one: an INSERT that replaced the previous row
    // would look identical from the confirmation alone.
    await expect(card(page, second.line)).toBeVisible();
    await expect(card(page, first.line)).toBeVisible();
    await expect(card(page, first.line)).toContainText(`Submitted by ${who.name}`);

    await expect(page.getByText("You have used all 2 picks for this year.")).toBeVisible();
  });

  test("a third submission is refused by the two-per-person cap", async ({ page }, testInfo) => {
    const project = testInfo.project.name;
    const who = member("submit", project);
    const { first, second, third } = films(project);

    await signInAs(page, who);

    // The form is gone rather than disabled, and the page says why.
    await page.goto("/submit");
    await expect(page.getByRole("heading", { level: 2, name: "That is both your picks" })).toBeVisible();
    await expect(page.getByText("You have used all 2 picks for this year.")).toBeVisible();
    await expect(page.getByRole("button", { name: "Put it on the board" })).toHaveCount(0);

    // A missing form is a UI decision, not a cap. The cap is what happens to a
    // request that arrives anyway - a double click on a slow connection, a
    // resubmitted page, or somebody with curl - so the third film is posted
    // directly, with this browser's own session.
    const posted = await page.request.post("/submit", {
      form: { title: third.title, year: third.year, trailer_url: TRAILER, description: "" },
      maxRedirects: 0,
    });

    // 409, not 403: the season is open and this person is on the list, they
    // have simply run out of picks.
    expect(posted.status()).toBe(409);
    expect(await posted.text()).toContain("That is both your picks");

    // And the refusal is a refusal all the way down: nothing was written.
    await page.goto("/slate");
    await expect(card(page, third.line)).toHaveCount(0);
    await expect(card(page, first.line)).toBeVisible();
    await expect(card(page, second.line)).toBeVisible();
  });
});

test.describe("somebody who is not on this season's list", () => {
  test("can read the board and cannot put anything on it", async ({ page }) => {
    // Signing in SUCCEEDS for them - the provider vouches for the address, the
    // session is real - and the whitelist is a separate gate in front of the
    // pages that do something. Landing on the refusal page is the proof that
    // authentication and authorisation are two decisions here.
    await signInAs(page, stranger);
    await expect(page.getByRole("heading", { level: 1, name: "You're not on the list yet" })).toBeVisible();
    // The address they arrived with, named twice on that page: once in the
    // lead and once in the note telling them what to send an admin.
    await expect(page.getByText(stranger.email).first()).toBeVisible();

    // The board is public to read. That is the whole reason the link gets
    // pasted into a group chat, so it has to work for somebody who is signed
    // in as nobody in particular.
    const slate = await page.goto("/slate");
    expect(slate?.status()).toBe(200);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await expect(page.getByRole("link", { name: "Put a movie up" })).toHaveCount(0);

    // Public to read, not to write. The form is refused...
    await page.goto("/submit");
    await expect(page.getByRole("heading", { level: 1, name: "You're not on the list yet" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Put it on the board" })).toHaveCount(0);

    // ...and so is a POST that does not bother with the form. This is ticket
    // E7 (nap-90j): `movie` has foreign keys to season and to person but not
    // to season_member, so the database on its own would take this row.
    const title = "Smuggled past the whitelist";
    const posted = await page.request.post("/submit", {
      form: { title, year: "1999", trailer_url: "", description: "" },
      maxRedirects: 0,
    });
    expect(posted.status()).toBe(200);
    expect(await posted.text()).toContain("You&#39;re not on the list yet");

    await page.goto("/slate");
    await expect(page.getByText(title)).toHaveCount(0);
  });
});

// Most people submit from a phone, and a form is where a layout that only
// works at 1280px actually stops somebody. This uses a person of its own who
// never submits anything, so it neither depends on nor disturbs the journeys
// above.
test.describe("the form on a phone", () => {
  test("survives a 320px-wide screen", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "mobile", "runs in the mobile project only");

    await signInAs(page, member("form", testInfo.project.name));

    // 320px is the narrowest phone still in use.
    await page.setViewportSize({ width: 320, height: 720 });
    await page.goto("/submit");

    for (const label of ["Title", "Year", "Trailer link", "Why this one"]) {
      await expect(page.getByLabel(label)).toBeVisible();
    }

    const button = page.getByRole("button", { name: "Put it on the board" });
    await expect(button).toBeVisible();

    const box = await button.boundingBox();
    expect(box, "the submit button has no box").not.toBeNull();
    // 44px is the tap-target floor base.css enforces. A button a thumb cannot
    // hit is the failure this width is checked for.
    expect(box!.height).toBeGreaterThanOrEqual(44);
    expect(box!.x + box!.width).toBeLessThanOrEqual(320);

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow, "the form scrolls sideways at 320px").toBeLessThanOrEqual(0);
  });
});
