// Who signs in, and who is on the season's whitelist.
//
// These are two halves of one fact and they are written here once. The suite
// tells the provider which address to sign a token for (`queueUser`); the seed
// writes the `season_member` row that address is checked against. When those
// two lists disagree the failure is loud but misleading: sign-in succeeds, the
// gate says "You're not on the list yet", and the page a test expected a form
// on is a friendly refusal instead. internal/devusers exists for exactly this
// reason on the Go side; this file is the same idea for the browser suite, and
// `SEED_EXTRA_MEMBERS` (below) is how the two are joined.
//
// ---------------------------------------------------------------------------
// Why there is an identity per journey per project
// ---------------------------------------------------------------------------
//
// One application, one database, two Playwright projects, and journeys that
// WRITE. A voter has two picks; a journey that spends both of them and then
// asserts the third is refused has permanently changed the world for anybody
// who runs after it - including the very same journey in the other project,
// which would find the cap already reached and "pass" without ever having
// submitted anything.
//
// The fix is not to reset the database between tests (the state under test IS
// the accumulated one) and not to order the files carefully (Playwright orders
// them alphabetically, which puts slate.spec before submit.spec whatever the
// journeys need). It is to give every journey that writes its own person. A
// person is free: one row, seeded before the run.
//
// So an identity is named for the role it plays and the project it plays it
// in, and `seedExtraMembers()` derives the whitelist from the same function
// the tests sign in with. Adding a Playwright project adds its identities.

import { createHash } from "node:crypto";

import type { TestUser } from "./oidc";

/**
 * The Playwright projects the suite runs.
 *
 * playwright.config.ts checks its own project list against this one and
 * refuses to load if they disagree, because a project missing from here is a
 * project whose people are never seeded - and the symptom of that is the
 * whitelist refusal page, which looks like a bug in the gate.
 */
export const projectNames = ["mobile", "desktop"] as const;

export type ProjectName = (typeof projectNames)[number];

/**
 * The roles a journey can need. Each one is a separate person per project.
 *
 * - `submit` spends both picks: the submit journeys end at the cap.
 * - `slate`  spends one: the board has to have something on it to list.
 * - `form`   spends none, and exists so that a test which only LOOKS at the
 *            form (the 320px layout check) does not have to care whether the
 *            journeys above it have run, or run at all.
 */
export const roles = ["submit", "slate", "form"] as const;

export type Role = (typeof roles)[number];

/**
 * The whitelisted person who plays `role` in `project`.
 *
 * The address is derived rather than listed so that it cannot disagree with
 * the seeded one: `seedExtraMembers()` below walks the same two loops.
 *
 * `.test` is reserved by RFC 6761 and can never be registered, so none of
 * these can reach a real inbox even if one escaped into a real database.
 */
export function member(role: Role, project: string): TestUser {
  if (!(projectNames as readonly string[]).includes(project)) {
    // Not a guard against typos - `project` comes from testInfo. It is a guard
    // against a NEW project, whose people nothing has seeded. Failing here
    // names the cause; letting it through would produce a whitelist refusal in
    // the middle of an unrelated journey.
    throw new Error(
      `e2e: no seeded people for Playwright project "${project}". ` +
        `Add it to projectNames in lib/people.ts so the seed creates its identities.`,
    );
  }

  const email = `e2e-${role}-${project}@example.test`;
  return { subject: subjectFor(email), email, name: displayName(role, project) };
}

/**
 * Somebody who signs in successfully and is on nobody's whitelist.
 *
 * This is internal/devusers' `stranger`, and it deliberately needs no seeded
 * row of its own: sign-in creates the person, and the whitelist gate turns
 * them away. That is the thing the refusal journeys assert, so inventing a
 * fixture for it would be inventing the state under test.
 */
export const stranger: TestUser = {
  subject: subjectFor("stranger@example.test"),
  email: "stranger@example.test",
  name: "Sam Stranger",
};

/**
 * The value of `SEED_EXTRA_MEMBERS` for cmd/seed: every `member()` above, as
 * `email|Display Name` separated by commas.
 *
 * The global setup puts this in the environment before it brings the stack up.
 * `stranger` is not here on purpose - see above.
 */
export function seedExtraMembers(): string {
  const entries: string[] = [];
  for (const project of projectNames) {
    for (const role of roles) {
      const p = member(role, project);
      entries.push(`${p.email}|${p.name}`);
    }
  }
  return entries.join(",");
}

function displayName(role: Role, project: string): string {
  // What the board credits a submission to, so it reads as a name rather than
  // as an address: "Submitted by E2E Submit (mobile)".
  return `E2E ${role[0]!.toUpperCase()}${role.slice(1)} (${project})`;
}

/**
 * The OIDC `sub` claim for an address.
 *
 * Same derivation as internal/devusers.User.Subject: sha256 of
 * "nap-dev-oidc|<lowercased address>", first eight bytes, hex, "dev-" prefix.
 * It is copied rather than imported because this directory shares no code with
 * the application - and it is DERIVED rather than random because sub is what
 * person.google_sub holds after a first sign-in. A subject that changed
 * between runs, or between the two projects, would make the second sign-in
 * look like a brand new Google account arriving with an address somebody else
 * already claimed: the identity-collision refusal, for no reason.
 *
 * If the Go derivation ever changes, this does not fail silently. The seed
 * leaves google_sub NULL, so the first sign-in claims the row whatever the
 * subject is; a drift only shows up if something else has already written a
 * subject, and then it shows up as "That address is already claimed", which
 * names itself.
 */
export function subjectFor(email: string): string {
  const digest = createHash("sha256").update(`nap-dev-oidc|${email.toLowerCase()}`).digest();
  return `dev-${digest.subarray(0, 8).toString("hex")}`;
}
