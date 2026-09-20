# Night of a Thousand Pixels

An annual movie-voting site for a group of friends. Each October everybody puts up
to two films on the board, then ranks their top three, and an instant-runoff count
decides what gets watched.

2025 ran on a Next.js app, now archived under `archive/2025/`. This is the 2026
rewrite: Go, chi, templ, sqlc, PostgreSQL 16, Google sign-in.

---

## Run it locally

You need **Go 1.26+** and **Docker**. Nothing else — every tool (templ, sqlc, goose,
air, golangci-lint) is pinned in `go.mod` and fetched on first use.

```bash
git clone https://github.com/ERaith/nightofathousandpixels.git
cd nightofathousandpixels

make dev      # migrates, seeds, then starts the three watchers
```

Then open **http://localhost:7331** — the templ proxy, not `:8080`. The proxy is what
gives you live reload; the app port works but won't refresh itself.

`make dev` is enough on its own: `dev-up` migrates and seeds before the
watchers start, so a clean clone gets a real season and a whitelist rather
than a site that serves "Nobody has gone first yet" and refuses every
submission. Both steps are safe to repeat — see **Test users** below.

Several people (or agents) can run this on one machine at the same time:
every port, container name, network and volume is derived from `AGENT_SLOT`,
which defaults to 0.

### Sign in

There is **no password and no bypass route**. Local dev runs a real OIDC provider
(`devtools/mockoidcd`) and the app performs a real authorization-code flow against it,
so development exercises exactly the same authentication code as production.

Go to **http://localhost:7331/auth/login** and pick one:

| user | what they are | use them to see |
|---|---|---|
| `alice@example.test` | whitelisted voter | the normal path |
| `bob@example.test` | whitelisted voter | a second person on the board |
| `admin@example.test` | season admin | anything admin-gated |
| `stranger@example.test` | **not whitelisted** | the polite refusal |

`make setup` seeds a 2026 season in `submitting` with a 2-film limit per person.

---

## Work through it

1. **Load `/slate` signed out.** The public board. With nothing on it you get the
   empty state, which is deliberately an invitation rather than a "no results" message.
2. **Sign in as Bob, submit a film.** Title, year, trailer URL, a line about why.
   You land back on the slate with a confirmation and the film credited to you.
3. **Submit a second.** Fine.
4. **Try a third.** `409` — that's his cap. The cap is enforced in a transaction with
   a row lock, not by hiding the button, so posting directly doesn't get around it.
5. **Sign in as Stranger.** Reads the board fine. `/submit` explains they're not on
   this year's list. Deliberately a 200 with a human page, not a 403.
6. **Submit rubbish.** Blank title, `nineteen eighty-four` as the year, a bare
   `youtube.com/...` trailer. `422`, three errors wired to their own fields, and every
   value you typed comes back exactly as typed.
7. **Refresh after submitting.** Nothing duplicates — the POST redirects.

### Look at the themes

`make seed-dev` creates a **2026 season in `submitting` state** and these four
people. All four exist in `person`; only three are on the 2026 whitelist.

It runs on its own as part of `make setup`, `make dev`, `make compose-up` and
`make e2e`, so in practice you rarely call it. It is **idempotent** — every
statement is an upsert on a natural key — and **additive**: it never writes
`google_sub`, so once you have signed in as one of these people, re-seeding
leaves your identity, your submissions and your quota exactly where they were.
(It does reset `display_name` to the value below; your next sign-in sets it
again from the ID token.)

The Go integration tests are the one thing that deliberately runs against an
**unseeded** database: they build their own fixtures per test, and a shared
season underneath them would give a test state it did not create. See the note
above `test-db-up` in the Makefile.

| Address | Name | On the 2026 list? | What it shows |
|---|---|---|---|
| `admin@example.test` | Ada Admin | yes, `is_admin` | the admin path — `season_member.is_admin`, per season |
| `alice@example.test` | Alice Voter | yes | the ordinary member path |
| `bob@example.test` | Bob Voter | yes | a second member, for anything involving two people |
| `stranger@example.test` | Sam Stranger | **no** | the "you're not on the list" page — a friendly page, not a 403 |

`stranger@` is not an oversight. Without somebody who is genuinely not on the
list, the refusal path is something you have to take on trust instead of
something you can click.

The seeded people have **no `google_sub`** until they first sign in, which is
the state an admin's whitelist entry is actually in. So the first sign-in as
each of them exercises the real row-claiming path rather than skipping it.

The seed is additive and idempotent: every statement is an upsert on a natural
key, re-running it changes nothing, and it never clears a `google_sub` that a
sign-in has already written. To start over, delete the volume with
`make compose-nuke`.

### Signing in as somebody else

Sign out (the button on `/me`), then sign in again and pick a different name.
The session cookie is cleared with attributes matching the ones it was set
with, so the browser actually drops it.

Tests that drive the browser can skip the picker entirely by queueing an
identity on the provider first:

```sh
curl -X POST http://localhost:9000/control/user \
  -d '{"subject":"sub-alice","email":"alice@example.test","name":"Alice"}'
```

Every page under the season's theme pack. `?theme=portal`, `?theme=elvira` or
`?theme=none` on any URL to compare. Elvira is the pick for 2026.

---

## The commands that matter

```bash
make help              # everything, with descriptions

make setup             # clean clone → running, seeded app
make dev               # the three watchers
make dev-down          # stop, keep the database
make seed              # reload the dev fixtures (idempotent)

make test              # unit tests
make test-integration  # against a throwaway postgres
make lint

make migrate-new name=add_votes_table
make migrate-up
make migrate-down
```

Every target derives its ports from `AGENT_SLOT` (default 0), so
`AGENT_SLOT=1 make dev` runs a second, fully separate stack — own containers,
own volumes, own ports. That's how several people (or agents) work at once.

| | slot 0 | slot 1 |
|---|---|---|
| app | 8080 | 8090 |
| templ proxy | 7331 | 7341 |
| postgres | 5433 | 5443 |

---

### How the tests are split

**A test that needs Postgres carries `//go:build integration`.** That is the
whole contract, and it is checked rather than documented, because the version
that was only documented was false for months.

`make test` does not compile those files, so it needs no Docker and it prints
what it left out. `make test-integration` is the only path that touches a
database; it starts and migrates a throwaway one, and it **refuses to run if
the tag selects no tests**. That last guard is the important one: before it,
`-tags=integration` matched no file in the repository, so the integration
suite was empty and every green it produced meant nothing — while the same
tests, untagged, skipped under a bare `go test ./...` and let the package print
`ok` having connected to nothing (nap-gn1).

Two more guards keep it true as the tree grows, both in the ordinary unit
suite:

* `TestEveryDatabaseTestCarriesTheIntegrationTag` fails if a database test
  joins the unit suite.
* `TestTheDSNCheckIsFatalNotASkip` fails if one of them goes back to skipping
  when `TEST_DATABASE_URL` is empty. Under the tag, an absent database is a
  broken invocation, not a reason to report `ok`.

That last target is not ceremony. The mock OIDC provider is a separate `main`
package, so `go list -deps ./cmd/server` does not mention
`oauth2-proxy/mockoidc` — and if it ever does, the mock identity provider is
inside the shipped binary. A Dockerfile stage is a convention; an absent import
is a fact, so the fact is what gets checked.

---

## Known broken, as of this writing

Be aware of these before you lose an hour to one. Each has an open ticket.

- **`make compose-up` crash-loops.** The base compose stack has no OIDC provider, so
  the app can't complete startup discovery and restarts forever. Use `make dev`.
  → `nap-0yg`
- **`make templ-generate` does nothing inside a git worktree.** It reports
  `updates=0` and exits 0. `make build` depends on it, so a `.templ` edit can silently
  fail to take effect. Fine in a normal clone. Fixed on this branch; verify. → `nap-hil`

---

## What exists and what doesn't

**Works:** sign-in with a per-season whitelist, submitting films, the 2-per-person cap,
the slate, validation, theme packs, the schema and its integrity guards.

**Doesn't exist yet:** ranked voting and the instant-runoff tally. That's the whole
second half. The Gherkin scenarios are written; the implementation isn't.

**Not deployed publicly.** It runs on the home server at `192.168.1.8:3005` on the LAN.
Going public needs port forwarding and a DNS cutover.

---

## How it's put together

```
cmd/server/            the binary
cmd/seed/              dev + test fixtures. Never reachable from the shipped image.
devtools/mockoidcd/    a real OIDC provider for dev and tests. Also never shipped.
internal/
  auth/                OIDC flow: PKCE, state, nonce, token verification
  signin/              sessions and the whitelist gate
  store/               sqlc queries + goose migrations
  web/                 routes, handlers, templates, view models
  web/board/           the slate and submit handlers
static/css/base.css    layout and the accessibility floor
themes/                portal/ and elvira/ — colour, type and copy only
e2e/                   Playwright, driving a real browser against a real stack
```

### Two rules worth knowing before you change things

**A theme pack owns colour, type, copy and assets — never layout, never accessibility.**
`base.css` enforces its floors (16px text, 44px tap targets, visible focus rings,
reduced motion) inside `@layer nap-enforce` using `max()` against literals, specifically
so a pack can't lower them. If you find yourself wanting a pack to override a layout
rule, that's the contract saying no.

**Templates never see database types.** `internal/web/viewmodel` holds plain Go structs;
handlers fill them, templates render them. A test parses the package's own imports and
fails if `pgx`, `sqlc` or `store` appears. That seam is what lets the frontend and
backend move independently.

---

## Tracker

Work is tracked in [beads](https://github.com/gastownhall/beads), in-repo:

```bash
bd ready --exclude-type=epic   # what's unblocked right now
bd list --status open
bd show nap-xxx                # one ticket, with its dependency tree
bd status                      # counts
```
