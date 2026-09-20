// Command seed loads development data: a season, the test people, and the
// whitelist rows that let three of them in.
//
// It is for development only, and it says so at the top of every run. It is
// also strictly additive and idempotent: every statement is an upsert keyed on
// a natural key, so running it twice changes nothing and running it against a
// database that already has real data adds these rows beside that data rather
// than replacing it. There is deliberately no --reset: `make compose-nuke`
// already deletes the volume, and a seed that can wipe a database is a seed
// somebody eventually runs against the wrong DATABASE_URL.
//
// The people here are the other half of devtools/mockoidcd's identity list. That
// process signs ID tokens for these addresses; this one writes the whitelist
// rows those tokens are checked against. If the two lists disagree, a
// "whitelisted" test user is told they are not on the list -- so the addresses
// are asserted against each other by TestSeedUsersMatchProviderUsers.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ERaith/nightofathousandpixels/internal/devusers"
	"github.com/ERaith/nightofathousandpixels/internal/signin"
)

// seedYear is the season the development stack runs in.
const seedYear = 2026

// seedState is the phase that makes the app usable: submissions are open, so
// the submit form has something to do. A 'draft' season would be invisible to
// GetCurrentSeason and every signed-in person would land on "no season is
// open", which is a confusing first run.
const seedState = "submitting"

func main() {
	if err := run(); err != nil {
		log.Fatalf("seed: %v", err)
	}
}

func run() error {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL is not set - run this through `make seed-dev`, which derives it from AGENT_SLOT")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	// Everything in one transaction: a seed that half-applies leaves a season
	// with no whitelist, which looks exactly like the bug it would be mistaken
	// for.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	seasonID, err := upsertSeason(ctx, tx)
	if err != nil {
		return err
	}

	cast, err := seedCast()
	if err != nil {
		return err
	}

	for _, p := range cast {
		personID, err := upsertPerson(ctx, tx, p)
		if err != nil {
			return err
		}
		if !p.Member {
			log.Printf("  %-24s not whitelisted (this is deliberate)", p.Email)
			continue
		}
		if err := upsertMember(ctx, tx, seasonID, personID, p.Admin); err != nil {
			return err
		}
		role := "voter"
		if p.Admin {
			role = "admin"
		}
		log.Printf("  %-24s %s for %d", p.Email, role, seedYear)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	log.Printf("seeded the %d season (%s) and %d people", seedYear, seedState, len(cast))
	log.Printf("sign in at /auth/login and pick one of them")
	return nil
}

// extraMembersEnv names additional whitelisted people to seed, on top of the
// development cast.
//
// It exists for the browser suite. The four people in internal/devusers are
// the cast a HUMAN picks from on the provider's consent screen, and that list
// should stay short enough to read. The e2e suite needs something different:
// one identity per journey per Playwright project, so that a journey which
// spends both of somebody's picks cannot leave the next journey - or the same
// journey in the other project - testing a quota that is already used up. Six
// more names on the dev picker would be six names nobody developing locally
// wants to scroll past.
//
// The format is the same as the provider's own MOCKOIDC_USERS, deliberately:
// comma-separated `email|Display Name`. Everybody listed here is a MEMBER of
// the season - an address that is meant to be refused does not need a row at
// all, because sign-in creates the person and the whitelist gate turns them
// away, which is exactly what the refusal journeys assert.
//
// e2e/lib/people.ts holds the list and passes it in, so the suite and the
// whitelist cannot drift: the same file that decides who signs in decides who
// is on the list.
const extraMembersEnv = "SEED_EXTRA_MEMBERS"

// seedCast is the development cast plus whatever extraMembersEnv adds.
func seedCast() ([]devusers.User, error) {
	extra, err := extraMembers()
	if err != nil {
		return nil, err
	}

	cast := make([]devusers.User, 0, len(devusers.All)+len(extra))
	cast = append(cast, devusers.All...)

	// A duplicate address would be harmless here - every write is an upsert on
	// a natural key - but it is always a mistake in the list that produced it,
	// so it is reported rather than absorbed.
	seen := make(map[string]bool, len(cast))
	for _, u := range cast {
		seen[signin.NormalizeEmail(u.Email)] = true
	}
	for _, u := range extra {
		key := signin.NormalizeEmail(u.Email)
		if seen[key] {
			return nil, fmt.Errorf("%s lists %s, which is already in the development cast", extraMembersEnv, u.Email)
		}
		seen[key] = true
		cast = append(cast, u)
	}

	return cast, nil
}

// extraMembers parses extraMembersEnv.
//
// An entry that does not parse is an error rather than a skipped line. A seed
// that quietly dropped a name would put the suite in front of a whitelist it
// believes it is on, and the symptom - "You're not on the list yet" on a page
// the test expected a form on - looks like a bug in the whitelist gate.
func extraMembers() ([]devusers.User, error) {
	raw := strings.TrimSpace(os.Getenv(extraMembersEnv))
	if raw == "" {
		return nil, nil
	}

	var users []devusers.User
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		parts := strings.Split(entry, "|")
		email := strings.TrimSpace(parts[0])
		if email == "" || !strings.Contains(email, "@") {
			return nil, fmt.Errorf("%s entry %q has no email address", extraMembersEnv, entry)
		}

		u := devusers.User{Email: email, Member: true, Note: "added by " + extraMembersEnv}
		if len(parts) > 1 {
			u.DisplayName = strings.TrimSpace(parts[1])
		}
		users = append(users, u)
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("%s is set to %q but lists no addresses", extraMembersEnv, raw)
	}

	return users, nil
}

// querier is the subset of pgx both a pool and a transaction satisfy.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// upsertSeason creates the development season, or leaves an existing one
// alone apart from its state.
//
// Raw SQL rather than a sqlc query because there is no CreateSeason: the
// application never creates a season outside the admin screens, which do not
// exist yet. Adding a query to queries/season.sql for the benefit of a seed
// would put a write the application does not perform into the generated
// surface every handler can reach.
func upsertSeason(ctx context.Context, q querier) (uuid.UUID, error) {
	const stmt = `
INSERT INTO season (year, name, state, default_submit_limit, submit_opens_at, vote_opens_at)
VALUES ($1, $2, $3, 2, now() - interval '1 day', now() + interval '14 days')
ON CONFLICT (year) DO UPDATE
SET state = EXCLUDED.state,
    name  = EXCLUDED.name
RETURNING id`

	var id uuid.UUID
	name := fmt.Sprintf("Night of a Thousand Pixels %d", seedYear)
	if err := q.QueryRow(ctx, stmt, seedYear, name, seedState).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("upsert season %d: %w", seedYear, err)
	}
	return id, nil
}

// upsertPerson writes the whitelist row, leaving google_sub untouched.
//
// The DO UPDATE deliberately does not write google_sub. A person who has
// already signed in has a subject on their row, and re-running the seed must
// not clear it -- that would unlink them from their own Google account and
// hand the row to whoever signed in next.
func upsertPerson(ctx context.Context, q querier, p devusers.User) (uuid.UUID, error) {
	const stmt = `
INSERT INTO person (email, email_normalized, display_name)
VALUES ($1, $2, $3)
ON CONFLICT (email_normalized) DO UPDATE
SET email        = EXCLUDED.email,
    display_name = EXCLUDED.display_name
RETURNING id`

	var id uuid.UUID
	// signin.NormalizeEmail, not strings.ToLower: this column is what sign-in
	// matches on, and the two must apply the same policy or a seeded person is
	// told they are not on the list.
	normalized := signin.NormalizeEmail(p.Email)
	if err := q.QueryRow(ctx, stmt, p.Email, normalized, p.DisplayName).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("upsert person %s: %w", p.Email, err)
	}
	return id, nil
}

func upsertMember(ctx context.Context, q querier, seasonID, personID uuid.UUID, admin bool) error {
	const stmt = `
INSERT INTO season_member (season_id, person_id, is_admin)
VALUES ($1, $2, $3)
ON CONFLICT (season_id, person_id) DO UPDATE
SET is_admin = EXCLUDED.is_admin`

	if _, err := q.Exec(ctx, stmt, seasonID, personID, admin); err != nil {
		return fmt.Errorf("upsert membership for %s: %w", personID, err)
	}
	return nil
}
