# Night of a Thousand Pixels

Thirty-odd friends, two picks each, and one instant-runoff count that decides
what goes on the screen. Go, Postgres, templ, server-rendered HTML.

## Getting started

```sh
make setup    # clean clone -> env file, postgres, mock OIDC provider, migrations, seed data
make dev      # the watchers; open the templ proxy URL it prints
```

`make setup` is idempotent and safe to re-run. `make help` lists every target
and prints the ports your agent slot owns.

`make dev` is enough on its own: `dev-up` migrates and seeds before the
watchers start, so a clean clone gets a real season and a whitelist rather
than a site that serves "Nobody has gone first yet" and refuses every
submission. Both steps are safe to repeat — see **Test users** below.

Several people (or agents) can run this on one machine at the same time:
every port, container name, network and volume is derived from `AGENT_SLOT`,
which defaults to 0.

```sh
AGENT_SLOT=1 make dev
```

| | slot 0 | formula |
|---|---|---|
| app | 8080 | `8080 + SLOT*10` |
| templ proxy (open this one) | 7331 | `7331 + SLOT*10` |
| postgres | 5433 | `5433 + SLOT*10` |
| mock OIDC provider | 9000 | `9000 + SLOT*10` |

## Signing in locally

There is **no test login route and no `skipAuth`**. Local development signs in
through a real OpenID Connect provider — `oauth2-proxy/mockoidc`, running as a
container from `docker-compose.dev.yml` — so the application runs exactly the
authentication code it runs in production: discovery, JWKS, PKCE S256, code
exchange, RS256 signature verification, and the issuer, audience, expiry and
nonce checks.

`OAUTH_ISSUER_URL` is the only thing that differs, and it is configuration. The
consequence is worth being explicit about: **a misconfigured issuer in
production means sign-in is broken, not bypassed.** There is no hole to leave
open, because there is no hole.

Start at `/auth/login` (the header's **Sign in** link). Instead of Google's
account chooser you get the mock provider's, listing the seeded users. Picking
one decides which address the provider will sign a token for — it grants
nothing on its own, and the application still verifies the token and still
checks the season's whitelist.

### Test users

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

A queued identity takes precedence over the picker page.

## How authentication and authorisation fit together

They are deliberately separate, and the split is visible in the routing.

* **`internal/auth`** ends at *"this is a verified human with this verified
  email address"*. It creates no session and decides nothing about access.
* **`internal/signin`** resolves that identity to a `person` row, mints the
  session cookie, and gates the pages behind it.

Signing in therefore **succeeds for anyone Google will vouch for**. Being
allowed to *act* is a separate question, answered per request from the
`season_member` row — which is why the session cookie carries an identifier and
nothing else. Removing somebody from a season's whitelist, or taking away their
admin flag, takes effect on their **next click**, not when their cookie
expires.

Three refusals that are deliberately different from each other:

| Situation | What happens |
|---|---|
| Signed in, not on this season's list | 200 and a page naming the address and who to ask. Not a 403. |
| Signed in, no season exists yet | 200 and "no season is open" — not "ask to be added" to something that does not exist. |
| Address is on the list but belongs to a **different** Google account | 409, no session, and a page saying an admin has to sort it out. |

That last one matters more than it looks. `UpsertPersonOnSignIn` reports it by
returning **no rows**, which is the same signal Postgres gives for "nothing
matched". Read as "person not found", the natural next step is to create the
person — and creating the person *is* the account takeover the query's guard
exists to prevent. See the comment on the query in
`internal/store/queries/person.sql`.

## Configuration

`.env.example` documents every variable; `make setup` copies it to `.env`.

Four values belong to the agent slot and are assigned by the Makefile, so
setting them in `.env` has no effect (make prints a warning): `PORT`,
`DATABASE_URL`, `ORIGIN` and `OAUTH_ISSUER_URL`.

Two notes on cookies, both of which are silent when wrong:

* `Secure` is derived from the **`ORIGIN` scheme**, never from the request.
  Behind a TLS-terminating proxy `r.TLS` is nil on every request even though
  the browser is on HTTPS, and `X-Forwarded-Proto` is a request header, which
  is to say it is whatever the client last said it was. The server logs the
  value it derived at startup — that log line is the cheap way to catch an
  `ORIGIN` typo in a deployed environment.
* `COOKIE_SECRET` must be at least 32 characters or the server refuses to
  start. Changing it, or bumping the version suffix on either cookie label,
  signs everybody out. Nothing is lost — no session state is stored
  server-side — but it is a visible event for every user at once.

## Testing

```sh
make test              # unit tests; does NOT touch a database
make test-integration  # starts a throwaway postgres and runs against it
make lint
make no-mock-provider-in-server   # fails if the mock provider can reach the server binary
```

That last target is not ceremony. The mock OIDC provider is a separate `main`
package, so `go list -deps ./cmd/server` does not mention
`oauth2-proxy/mockoidc` — and if it ever does, the mock identity provider is
inside the shipped binary. A Dockerfile stage is a convention; an absent import
is a fact, so the fact is what gets checked.
