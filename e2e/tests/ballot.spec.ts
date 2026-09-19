// Ranking three movies, and changing your mind.
//
// This is the furthest-out of the placeholder specs: there is no ballot epic
// in the tracker yet, only the schema and the instant-runoff tally behind it.
// The journeys are written down here anyway because they are the reason the
// site exists, and because the shape of the harness should be settled before
// somebody is also inventing the page.
//
// Needs: a ballot page and handler. There is still no ticket for either.
//
// nap-dph (G4 E2E journeys) did NOT fill these in, and the titles below used
// to say it would. It wrote the submit and slate journeys, which exist; there
// is nothing on /ballot to drive, so these three stayed skipped and the ticket
// they name was corrected rather than left to look done. A skip that points at
// a closed ticket is how an unwritten test gets believed to be a written one.

import { expect, test } from "@playwright/test";
import { assertionsNotWrittenYet } from "../lib/unwritten";

const NEEDS_BALLOT = "no ticket yet: there is no ballot page, handler or tally endpoint";

test.describe("the ballot", () => {
  test("a voter ranks a first, second and third choice [skipped: no ballot page yet]", async ({
    page,
  }) => {
    test.skip(true, NEEDS_BALLOT);

    await page.goto("/ballot");
    // Ranking is the one interaction on this site that is genuinely awkward on
    // a phone, so this test matters most in the mobile project.
    assertionsNotWrittenYet(NEEDS_BALLOT);
  });

  test("a voter changes a ballot already cast [skipped: no ballot page yet]", async () => {
    test.skip(true, NEEDS_BALLOT);

    // Changing a ballot is the case that catches an INSERT where an UPSERT was
    // meant: the second ballot must replace the first, not add to it.
    assertionsNotWrittenYet(NEEDS_BALLOT);
  });

  test("the same movie cannot be ranked twice [skipped: no ballot page yet]", async () => {
    test.skip(true, NEEDS_BALLOT);
    assertionsNotWrittenYet(NEEDS_BALLOT);
  });
});
