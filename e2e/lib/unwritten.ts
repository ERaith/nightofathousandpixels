import { expect } from "@playwright/test";

/**
 * Fails, on purpose, in place of a test body nobody has written yet.
 *
 * Every placeholder spec in tests/ is skipped unconditionally and names in its
 * title the ticket that unskips it. The trap is what happens next: whoever
 * follows that instruction deletes the `test.skip()` and gets a GREEN test
 * that asserts nothing, because the body was only ever a sketch. That is the
 * precise failure this suite exists to prevent, sitting armed and waiting for
 * the person most likely to trust it.
 *
 * So an unwritten body calls this instead of standing in a tautology. Removing
 * the skip turns the spec red, with the ticket in the message, and it stays
 * red until somebody writes real assertions. That is the correct amount of
 * friction: unskipping is meant to be the start of the work, not the end of
 * it.
 *
 * Found in review of nap-560 by verifier-4, who deleted the skips and watched
 * the stubs pass. Tracked as nap-qry.
 */
export function assertionsNotWrittenYet(reason: string): void {
  expect(
    false,
    `${reason}\n\n` +
      `This test has no assertions yet, so it was failed deliberately rather than ` +
      `left to pass. Write the assertions before removing the test.skip() above: a ` +
      `spec that runs and asserts nothing is worse than one that is visibly skipped.`,
  ).toBe(true);
}
