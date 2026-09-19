# End-to-end browser tests

Black-box browser tests for Night of a Thousand Pixels, driving the **shipped
container image** over HTTP. This directory shares no code with the Go
application; it knows the site only through its URLs and its HTML.

```
make e2e            # install, type-check, bring up a stack, run, tear down
make e2e-ui         # the same, in Playwright's UI mode, leaving the stack up
make e2e-report     # open the HTML report from the last run
AGENT_SLOT=3 make e2e
```

`npx playwright test` from this directory does the same thing — its global
setup calls `make e2e-up` and its teardown calls `make e2e-down`, so there is
only one way the stack comes up.

## Why Node rather than playwright-go

The Go bindings shell out to Playwright's Node driver anyway, so choosing them
would cost the trace viewer, UI mode and `codegen` and buy nothing back. These
tests deliberately share no code with the application, so a second language
costs nothing either.

## How sign-in works, and why there is no bypass

Playwright cannot sign in to Google: headless browsers get flagged, and
scripting a real account would be fragile even if they were not. The common
workaround is a test-only bypass route in the application — a handler that
mints a session for whoever asks. That is a hole in production that happens to
be switched off, and the failure mode of leaving it switched on is silent and
total.

Instead the stack runs a **real OIDC provider**, `e2e/mockoidcd`, built on
[`oauth2-proxy/mockoidc`](https://github.com/oauth2-proxy/mockoidc).
`OAUTH_ISSUER_URL` is already configuration rather than a constant, so pointing
it at that process changes the issuer and nothing else. The application still
runs discovery, still fetches the JWKS, still sends a PKCE S256 challenge,
still exchanges the code and still verifies an RS256 signature over the issuer,
audience, expiry and nonce — **the same authentication code that runs in
production**.

There is no `skipAuth()` anywhere to leave enabled. The worst case of a
misconfigured issuer in production is that sign-in stops working, not that it
is bypassed. `tests/shipped-image.spec.ts` reads the module list back out of
the binary inside the running container and fails if `mockoidc` appears in it.

### The one genuinely awkward part

An OIDC issuer is **one string**: the discovery document, the `iss` claim and
the client's configured issuer are compared byte for byte, and every endpoint
hangs off the same base. But two parties must reach that URL — the app from
inside its container, and the **browser** from the host — and on Docker Desktop
they share no hostname. `mockoidc` resolves only inside the network,
`localhost` means different machines to each, and `host.docker.internal` does
not resolve on the host at all.

So the app is placed in the provider's network namespace
(`network_mode: "service:mockoidc"`). `localhost:<oidc port>` is then the same
socket for the app and, through the published port, for the browser. The
provider owns the namespace rather than the app because a service in another's
namespace cannot publish ports, and because once C3 wires `auth.New` into
startup the app must find the provider already listening. The long version is
in `docker-compose.e2e.yml`.

### Choosing who signs in

```ts
import { queueUser } from "../lib/oidc";

await queueUser({ subject: "abc", email: "someone@example.test", name: "Someone" });
await page.goto("/auth/login");
```

`queueUser` posts to a control endpoint **on the provider**. It grants nothing
and touches nothing in the application: it only decides which address the
provider will sign a token for, the way clicking an account on Google's chooser
does. The application still verifies that token, and will still have to decide
whether the address is on the season's whitelist. The provider pops one queued
user per authorize call, so call it immediately before the sign-in it applies
to.

## What runs today, and what is skipped

Most of the site does not exist yet. The suite reflects that honestly:

| Spec | State |
| --- | --- |
| `smoke.spec.ts` | Runs. `/`, `/healthz`, the site's 404, `base.css`, and two phone-width checks. |
| `oidc-provider.spec.ts` | Runs. A full authorization-code flow in a real browser, plus two checks that the provider refuses bad input. |
| `shipped-image.spec.ts` | Runs. `mockoidc` is absent from `go list -deps ./cmd/server` **and** from the binary in the container. |
| `sign-in.spec.ts` | One trip-wire runs; the journeys are skipped (nap-9jw, nap-l1i). |
| `submit.spec.ts` | Skipped (nap-4l9, nap-ibx, nap-623). |
| `slate.spec.ts` | Skipped (nap-nws, nap-1s9). |
| `ballot.spec.ts` | Skipped — no ticket yet; nap-dph fills these in. |

Every skip is **unconditional and visible**: the reason names the ticket, the
title carries it too, and the list reporter prints a `-` for it. None of them
assert nothing and call that green. A conditional skip ("skip if the page is
missing") would quietly start running the day something answered on that path,
whatever it answered.

The one exception is the trip-wire in `sign-in.spec.ts`, which asserts that
`/auth/login` is currently a 404. It is the thing that makes the skips beneath
it honest: when nap-9jw mounts the route, that test goes red, and the failure
is the instruction — delete it, unskip the journey, and the global setup starts
saving a real session instead of an empty one.

### This was verified, not assumed

The sign-in path above could not run against `beads-setup`, so it was run
against `agent/builder-2`, which carries C3 and C4, on a throwaway merge. No
code changed; the branch behaved exactly as designed:

```
e2e: /auth/login returned 302 - signing in for real
e2e: signed in as e2e-voter@example.test, 1 cookie(s) saved
✘ sign-in › is not mounted yet - delete this test when nap-9jw lands
    Expected: 404
    Received: 302
✓ sign-in › through the mock provider establishes a session   (both projects)
```

The trip-wire went red with the right reason, the global setup drove the real
flow and saved a session, and the journey beneath it unskipped itself and
passed. What is written here for a world that does not exist yet has been run
in one that does.

### The dev provider's consent screen

nap-bml adds a click-through identity picker to the provider for `make dev`.
It intercepts the authorize endpoint, so two things follow for tests:

- **Queue an identity before driving a flow.** A queued user tells the picker
  the caller is a test rather than a browser waiting to choose, and it
  delegates straight through. `oidc-provider.spec.ts`'s PKCE test queues one
  for exactly this reason even though it does not care who signs in.
- **`rejects an authorize request from an unknown client` is a specification,
  not a convenience.** It deliberately queues nobody. A picker that renders its
  list before mockoidc has validated `client_id`, `response_type`, `scope` and
  the PKCE method turns a misconfigured `OAUTH_CLIENT_ID` into a friendly page
  with a list of people on it; the developer clicks one and the failure
  surfaces later and somewhere else. Google refuses an unknown client, so the
  mock must too. This test currently fails against `agent/builder-2`
  (`Expected: >= 400 / Received: 200`) and that failure is the finding.

## Adding a test for a page that has just landed

1. Delete the `test.skip(true, ...)` line.
2. Write the assertions. The session, when there is one, arrives through the
   project's `storageState` — there is no sign-in step to write.
3. For a test that must be signed out:
   `test.use({ storageState: { cookies: [], origins: [] } })`.
4. For a specific identity, `queueUser(...)` then go to `/auth/login`.

## Ports, and other agents

Everything is derived from `AGENT_SLOT` so that several agents can run their
own stack at once. That arithmetic lives in the **Makefile** and is read from
it by `lib/stack.ts` (via `make e2e-env`) rather than duplicated here: two
copies would agree right up until someone changed one, and the symptom would be
a suite quietly testing a neighbour's application.

| | slot 0 | slot *n* |
| --- | --- | --- |
| app | 8085 | `8085 + n*10` |
| postgres | 5434 | `5434 + n*10` |
| OIDC provider | 9085 | `9085 + n*10` |

The database is a tmpfs and `make e2e-down` is `compose down -v`, so no run can
inherit anything from the one before it.

## Deliberate choices that look like omissions

- **`retries: 0`.** A retry turns a flake into a pass and files the evidence in
  a report nobody opens. If something here is flaky, that is the finding.
- **`workers: 1`, `fullyParallel: false`.** One application in front of one
  database. Journeys that submit a movie or cast a ballot write shared state,
  and parallelism would buy seconds at the cost of trusting a red run.
- **No `webServer`.** The stack is compose's, not Playwright's, so that the
  suite tests the image that ships rather than a `go run`.

## Proving the harness can fail

This project has a history of tests that were green while proving nothing, so
these were checked rather than assumed. Each was reverted afterwards.

- **Break the expected title.** Changing `SITE_TITLE` to `"Night of a Thousand
  Pixels!"` fails with `Expected: "Night of a Thousand Pixels!" / Received:
  "Night of a Thousand Pixels"` — a content mismatch against real fetched HTML,
  while `/healthz` and `base.css` kept passing in the same run.
- **Stop the application.** With the app container stopped, the global setup
  throws `timed out after 60000ms waiting for the application to answer on
  http://localhost:8175/healthz`, dumps the container logs, runs zero tests and
  exits 1. It does not report "0 passed".
- **Weaken the image assertion.** Changing `not.toContain("mockoidc")` to
  `not.toContain("pgx")` in `shipped-image.spec.ts` fails and prints the
  binary's real module list, so the negative assertion is searching actual
  content rather than an empty string.
- **Edit a template without regenerating.** Changing a heading in `home.templ`
  and leaving `home_templ.go` alone makes `make e2e` fail in `e2e-templ-fresh`
  before a browser starts, naming the stale file.

### Stale templates, which is the failure mode this nearly had

The image is built from the **committed** `*_templ.go` — the Dockerfile only
runs `go build`. So a `.templ` edited without regenerating produces a suite
that passes happily against markup nobody is serving.

Inside an agent worktree that is not hypothetical. `make templ-generate`
generates *nothing at all* there while printing a tick and exiting 0
(**nap-hil**): `TEMPL_IGNORE` is unanchored and templ matches it against
absolute paths, so `/worktrees/` in the path makes the pattern match every file
in the tree. Reproduced here — `updates=0`, and a deliberately corrupted
generated file survived untouched.

`make e2e` therefore runs `e2e-templ-fresh` first, which regenerates with a
pattern anchored at this checkout's root, with the root regex-escaped (see the
note on `TEMPL_IGNORE_E2E` in the Makefile), and fails if anything changed.

Three earlier patterns were tried and the first two each reintroduced the bug
by another route — dropping the exclusion made templ rewrite every other
agent's worktree from the main clone; leaving the pattern unanchored meant a
checkout under an ambient `tmp`, `bin` or `archive` directory matched every
file. That is the shape of a silent-failure bug: every fix for it is itself
hard to verify.

Delete the pattern when nap-hil lands. **Keep the guard.** A fix without a
check just relocates the next occurrence; the pattern is the bug, but the
absence of a check is why eight of these survived.

## Debugging a failure

`trace`, `screenshot` and `video` are all kept on failure.

```sh
make e2e-report                                  # the HTML report
npx playwright show-trace test-results/<dir>/trace.zip
E2E_KEEP_STACK=1 npx playwright test             # leave the stack up afterwards
E2E_SKIP_STACK=1 npx playwright test             # reuse a stack already running
make e2e-logs                                    # follow the containers
```
