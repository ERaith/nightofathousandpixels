// Putting a movie on the board.
//
// Every test here is skipped, and skipped VISIBLY: the reason names the ticket
// that will unskip it, and Playwright reports it as a skip rather than a pass.
// None of them assert nothing and call that green.
//
// The skip is unconditional on purpose. A conditional one ("skip if the page
// is missing") would quietly start running the day something answered on
// /submit, whatever it answered, and the first person to see the result would
// be whoever was debugging something else.
//
// Needs: nap-9jw (a session), nap-ibx (the submit handler), nap-4l9 (the form).

import { expect, test } from "@playwright/test";

const NEEDS_FORM = "nap-4l9: the submit form template does not exist; /submit is a 404";
const NEEDS_HANDLER = "nap-ibx + nap-623: there is no submit handler and no two-per-person cap";

test.describe("submitting a movie", () => {
  test("a signed-in voter can submit one [skipped until nap-4l9]", async ({ page }) => {
    test.skip(true, NEEDS_FORM);

    // The shape it will take. The session comes from the global setup via the
    // project's storageState, so there is no sign-in step here.
    await page.goto("/submit");
    await page.getByLabel("Title").fill("Predator");
    await page.getByLabel("Year").fill("1987");
    await page.getByLabel("Trailer").fill("https://www.youtube.com/watch?v=DdDYFaRHjdY");
    await page.getByRole("button", { name: /submit/i }).click();

    await expect(page.getByText("Predator")).toBeVisible();
  });

  test("the second submission is accepted [skipped until nap-4l9]", async ({ page }) => {
    test.skip(true, NEEDS_FORM);
    expect(page).toBeTruthy();
  });

  test("a third submission is refused by the two-per-person cap [skipped until nap-ibx]", async ({
    page,
  }) => {
    test.skip(true, NEEDS_HANDLER);

    // The cap is the interesting one: it has to hold against a form submitted
    // twice quickly, not only against a form that is politely disabled, so
    // this will eventually post directly rather than clicking.
    expect(page).toBeTruthy();
  });

  test("the form works on a 320px phone [skipped until nap-4l9]", async ({ page }) => {
    test.skip(true, NEEDS_FORM);

    // Most voters submit from a phone, and a form is where a layout that only
    // works at 1280px actually stops people.
    expect(page).toBeTruthy();
  });
});
