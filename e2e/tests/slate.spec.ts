// Seeing what everyone put up.
//
// Skipped until the slate exists. See tests/submit.spec.ts for why these are
// unconditional skips with the ticket in the title.
//
// Needs: nap-1s9 (the handler and queries), nap-nws (the page).

import { expect, test } from "@playwright/test";
import { assertionsNotWrittenYet } from "../lib/unwritten";

const NEEDS_SLATE = "nap-nws + nap-1s9: there is no slate page and no handler behind it";

test.describe("the slate", () => {
  test("lists every submitted movie [skipped until nap-nws]", async ({ page }) => {
    test.skip(true, NEEDS_SLATE);

    await page.goto("/slate");
    await expect(page.getByRole("heading", { name: /slate/i })).toBeVisible();
  });

  test("does not show a movie that was hidden [skipped until nap-nws]", async () => {
    // nap-8yw is the live bug that a hidden movie is still rankable; whatever
    // fixes it needs this test.
    test.skip(true, NEEDS_SLATE);
    assertionsNotWrittenYet(NEEDS_SLATE);
  });

  test("reads as one column on a phone [skipped until nap-nws]", async () => {
    test.skip(true, NEEDS_SLATE);
    assertionsNotWrittenYet(NEEDS_SLATE);
  });
});
