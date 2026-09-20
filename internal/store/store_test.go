//go:build integration

// Live-database tests. They are behind the `integration` build tag, which is
// the repository's one contract for "this test needs Postgres":
//
//     go test ./...                  compiles nothing in this file
//     make test                      the same, and says so
//     make test-integration          starts a database, migrates it, runs this
//
// The tag exists because the alternative did not work. This file used to be
// untagged and to skip when TEST_DATABASE_URL was empty, so a bare
// `go test ./...` printed `ok  internal/store` having run no database test at
// all -- for months, including the runs used to justify merges (nap-gn1). A
// skip that fires by default is indistinguishable from a pass in every summary
// anyone actually reads.
//
// With the tag, the untagged run does not compile these tests, `make test`
// prints what it left out, and `make test-integration` fails outright if the
// tag selects nothing. A missing TEST_DATABASE_URL is therefore a FATAL here
// rather than a skip: under this tag the database is the point, so its absence
// is a broken invocation, not a reason to go quietly green.

package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ERaith/nightofathousandpixels/internal/store"
)

// The structural property this whole package rests on: the generated query set
// runs against a pool in production and against a single transaction in tests,
// because both satisfy DBTX. If either assertion stops compiling, the
// BEGIN/ROLLBACK test harness stops being possible.
var (
	_ store.DBTX = (pgx.Tx)(nil)
	_ store.DBTX = (*pgxpool.Pool)(nil)
	_ store.DBTX = (*pgx.Conn)(nil)
)

// ptr is a local helper for the nullable parameters, which are honestly typed
// as pointers -- a nil submit_limit means "use the season default" and a
// pointer to 0 means "may not submit". Those are different, and this is the
// file that proves the difference survives the round trip.
func ptr[T any](v T) *T { return &v }

// show renders a nullable value the way the database sees it, so the test log
// distinguishes NULL from 0 instead of printing a pointer address.
func show[T any](v *T) string {
	if v == nil {
		return "NULL"
	}
	return fmt.Sprintf("%v", *v)
}

// containsSeason reports whether a listing contains the season with this id.
// Tests assert on the rows they seeded rather than on the length of a global
// list, so that an unrelated row in the database cannot turn them red.
func containsSeason(seasons []store.Season, id uuid.UUID) bool {
	for _, s := range seasons {
		if s.ID == id {
			return true
		}
	}
	return false
}

// poolCloseTimeout bounds how long a pool may take to close. Generous for an
// honest close, which returns as soon as the last connection is back.
const poolCloseTimeout = 10 * time.Second

// connect opens a pool against TEST_DATABASE_URL. Under the integration tag a
// missing DSN is fatal: see the note at the top of this file for why it is not
// a skip.
func connect(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is not set.\n" +
			"These tests are built with -tags=integration, which means they need a database.\n" +
			"Run them with `make test-integration`, which starts and migrates one.")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	// Closing the pool is bounded, because this is where a leaked transaction
	// strands the whole package.
	//
	// pool.Close waits for every connection to be returned. A test that never
	// rolls back still holds one, so Close blocks forever: the package runs to
	// Go's test timeout and dies with a full goroutine dump that names
	// pgxpool, not the test that leaked. Measured, not assumed -- the stack
	// bottoms out in puddle.Pool.Close on a sync.WaitGroup.
	//
	// Bounding it turns that into one clear line naming the real cause. The
	// leak is still a failure; it is just a legible one.
	t.Cleanup(func() {
		closed := make(chan struct{})
		go func() {
			pool.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(poolCloseTimeout):
			t.Errorf("pool did not close within %s: a test left a transaction open, "+
				"which holds its connection and would otherwise hang the package until the test timeout",
				poolCloseTimeout)
		}
	})
	return pool
}

// begin wraps the scenario in a transaction that is always rolled back, so the
// test leaves the database exactly as it found it.
func begin(t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", err)
		}
	})
	return tx
}

func seedSeason(t *testing.T, tx pgx.Tx, year int32, defaultLimit int32) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		`INSERT INTO season (year, name, default_submit_limit, state)
		 VALUES ($1, $2, $3, 'submitting') RETURNING id`,
		year, "Test Season", defaultLimit).Scan(&id)
	if err != nil {
		t.Fatalf("seed season: %v", err)
	}
	return id
}

func signIn(t *testing.T, q *store.Queries, email, sub, name string) store.Person {
	t.Helper()
	p, err := q.UpsertPersonOnSignIn(context.Background(), store.UpsertPersonOnSignInParams{
		Email:           email,
		EmailNormalized: email,
		GoogleSub:       sub,
		DisplayName:     name,
	})
	if err != nil {
		t.Fatalf("sign in %s: %v", email, err)
	}
	return p
}

// TestQuerierIsSatisfiedByTransaction is the structural requirement spelled
// out: the full Querier interface, not just some concrete struct, has to be
// usable when the thing underneath it is a transaction. The integration
// harness wraps every scenario in BEGIN/ROLLBACK, so if this stops working the
// test strategy stops working with it.
func TestQuerierIsSatisfiedByTransaction(t *testing.T) {
	pool := connect(t)
	tx := begin(t, pool)

	var q store.Querier = store.New(tx)

	seasonID := seedSeason(t, tx, 2031, 2)
	season, err := q.GetSeason(context.Background(), seasonID)
	if err != nil {
		t.Fatalf("query through Querier backed by pgx.Tx: %v", err)
	}
	t.Logf("store.Querier backed by pgx.Tx read back season year=%d", season.Year)

	// And the same interface value works against a pool, which is what the
	// server will hand it.
	var viaPool store.Querier = store.New(pool)
	if _, err := viaPool.ListSeasons(context.Background()); err != nil {
		t.Fatalf("query through Querier backed by *pgxpool.Pool: %v", err)
	}
	t.Logf("store.Querier backed by *pgxpool.Pool also works")

	// The rollback must leave nothing behind -- and proving that requires the
	// rollback to have already happened.
	//
	// This assertion used to be registered with t.Cleanup, where it could not
	// fail. Cleanups run LIFO, so one registered here fires BEFORE begin's
	// rollback; and it reads through pool, a different connection, which under
	// MVCC cannot see this transaction's uncommitted rows in any case. It was
	// true for a reason unrelated to what it claimed to test. Worse, deleting
	// the rollback from begin did not turn it red: the open transaction holds
	// locks the next test blocks on, so the package hung to its 600s timeout
	// with no diagnostic.
	//
	// So roll back here, explicitly, and then look. begin's own cleanup still
	// runs and tolerates the already-closed transaction.
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatalf("explicit rollback: %v", err)
	}
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM season WHERE id = $1`, seasonID).Scan(&n); err != nil {
		t.Fatalf("post-rollback check: %v", err)
	}
	if n != 0 {
		t.Errorf("rollback left the seeded season behind (%d row(s) visible from the pool)", n)
	}
	t.Logf("after an explicit rollback the seeded season is not visible from the pool")
}

// TestEffectiveSubmitLimit is the query that matters most: the COALESCE that
// decides whether a person may submit at all.
func TestEffectiveSubmitLimit(t *testing.T) {
	pool := connect(t)
	tx := begin(t, pool)
	q := store.New(tx) // pgx.Tx as DBTX, at runtime this time
	ctx := context.Background()

	seasonID := seedSeason(t, tx, 2026, 2)

	cases := []struct {
		name     string
		email    string
		override *int32
		want     int32
	}{
		{"no override falls back to season default", "default@example.com", nil, 2},
		{"override above the default wins", "generous@example.com", ptr(int32(5)), 5},
		{"explicit zero override is honoured, not treated as unset", "muted@example.com", ptr(int32(0)), 0},
	}

	for _, tc := range cases {
		person := signIn(t, q, tc.email, "sub-"+tc.email, tc.email)
		if _, err := q.UpsertSeasonMember(ctx, store.UpsertSeasonMemberParams{
			SeasonID: seasonID, PersonID: person.ID, IsAdmin: false, SubmitLimit: tc.override,
		}); err != nil {
			t.Fatalf("%s: upsert member: %v", tc.name, err)
		}

		got, err := q.GetEffectiveSubmitLimit(ctx, store.GetEffectiveSubmitLimitParams{
			SeasonID: seasonID, PersonID: person.ID,
		})
		if err != nil {
			t.Fatalf("%s: effective limit: %v", tc.name, err)
		}

		t.Logf("%-52s season_member.submit_limit=%-4s season.default_submit_limit=2 -> effective=%d",
			tc.name, show(tc.override), got)
		if got != tc.want {
			t.Errorf("%s: effective limit = %d, want %d", tc.name, got, tc.want)
		}
	}

	// A non-member is not "limit 0" -- they have no membership at all, and the
	// query says so by returning no rows.
	_, err := q.GetEffectiveSubmitLimit(ctx, store.GetEffectiveSubmitLimitParams{
		SeasonID: seasonID, PersonID: uuid.New(),
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("non-member: got err %v, want pgx.ErrNoRows", err)
	}
	t.Logf("%-52s -> pgx.ErrNoRows (not a member, which is not the same as a limit of 0)", "non-member")

	// Clearing an override puts the person back on the season default.
	person := signIn(t, q, "cleared@example.com", "sub-cleared", "Cleared")
	for _, override := range []*int32{ptr(int32(7)), nil} {
		if _, err := q.UpsertSeasonMember(ctx, store.UpsertSeasonMemberParams{
			SeasonID: seasonID, PersonID: person.ID, IsAdmin: false, SubmitLimit: override,
		}); err != nil {
			t.Fatalf("re-upsert member: %v", err)
		}
		got, err := q.GetEffectiveSubmitLimit(ctx, store.GetEffectiveSubmitLimitParams{
			SeasonID: seasonID, PersonID: person.ID,
		})
		if err != nil {
			t.Fatalf("effective limit after re-upsert: %v", err)
		}
		t.Logf("%-52s override=%-4s -> effective=%d", "upsert rewrites an override", show(override), got)
	}
}

// TestPersonSignIn covers the two sign-in paths and the whitelist-before-first
// -sign-in case that the email upsert exists for.
func TestPersonSignIn(t *testing.T) {
	pool := connect(t)
	tx := begin(t, pool)
	q := store.New(tx)
	ctx := context.Background()

	// An admin whitelists an email. No Google identity yet.
	var whitelisted store.Person
	err := tx.QueryRow(ctx,
		`INSERT INTO person (email, email_normalized, display_name)
		 VALUES ($1, $1, '') RETURNING id, email, email_normalized, google_sub, display_name, created_at`,
		"newcomer@example.com").Scan(&whitelisted.ID, &whitelisted.Email,
		&whitelisted.EmailNormalized, &whitelisted.GoogleSub, &whitelisted.DisplayName, &whitelisted.CreatedAt)
	if err != nil {
		t.Fatalf("whitelist: %v", err)
	}
	t.Logf("whitelisted before sign-in: google_sub=%s display_name=%q", show(whitelisted.GoogleSub), whitelisted.DisplayName)

	// First sign-in links the Google identity to that same row.
	linked := signIn(t, q, "newcomer@example.com", "google-sub-1", "Newcomer")
	if linked.ID != whitelisted.ID {
		t.Fatalf("first sign-in created a new person: %v != %v", linked.ID, whitelisted.ID)
	}
	t.Logf("after first sign-in:        same id=%v google_sub=%q display_name=%q",
		linked.ID == whitelisted.ID, *linked.GoogleSub, linked.DisplayName)

	// A provider that returns no display name must not blank out the stored one.
	again := signIn(t, q, "newcomer@example.com", "google-sub-1", "")
	if again.DisplayName != "Newcomer" {
		t.Errorf("empty display name overwrote stored one: %q", again.DisplayName)
	}
	t.Logf("sign-in with empty name:    display_name=%q (preserved)", again.DisplayName)

	// The returning-visitor lookup.
	bySub, err := q.GetPersonByGoogleSub(ctx, "google-sub-1")
	if err != nil {
		t.Fatalf("get by google_sub: %v", err)
	}
	byEmail, err := q.GetPersonByEmailNormalized(ctx, "newcomer@example.com")
	if err != nil {
		t.Fatalf("get by email_normalized: %v", err)
	}
	t.Logf("lookup by sub == lookup by email: %v", bySub.ID == byEmail.ID)

	// Email change: matched by sub, identity updated in place.
	moved, err := q.UpdatePersonIdentity(ctx, store.UpdatePersonIdentityParams{
		ID: bySub.ID, Email: "moved@example.com", EmailNormalized: "moved@example.com", DisplayName: "",
	})
	if err != nil {
		t.Fatalf("update identity: %v", err)
	}
	t.Logf("after email change:         email=%q display_name=%q same id=%v",
		moved.Email, moved.DisplayName, moved.ID == bySub.ID)
}

// TestMovieCapAndSoftDelete covers the count that backs the 2-per-person cap
// and the hidden flag that the count has to respect.
func TestMovieCapAndSoftDelete(t *testing.T) {
	pool := connect(t)
	tx := begin(t, pool)
	q := store.New(tx)
	ctx := context.Background()

	seasonID := seedSeason(t, tx, 2027, 2)
	alice := signIn(t, q, "alice@example.com", "sub-alice", "Alice")
	bob := signIn(t, q, "bob@example.com", "sub-bob", "Bob")
	for _, p := range []store.Person{alice, bob} {
		if _, err := q.UpsertSeasonMember(ctx, store.UpsertSeasonMemberParams{
			SeasonID: seasonID, PersonID: p.ID,
		}); err != nil {
			t.Fatalf("member: %v", err)
		}
	}

	count := func(p store.Person) int64 {
		t.Helper()
		n, err := q.CountPersonMoviesInSeason(ctx, store.CountPersonMoviesInSeasonParams{
			SeasonID: seasonID, SubmittedBy: p.ID,
		})
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	first, err := q.CreateMovie(ctx, store.CreateMovieParams{
		SeasonID: seasonID, SubmittedBy: alice.ID, Title: "The Thing",
		Year: ptr(int32(1982)), TmdbID: ptr(int32(1091)),
		TrailerUrl: ptr("https://example.com/thing"), Description: "practical effects",
	})
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}
	// Nullable columns left nil must come back nil, not zero.
	sparse, err := q.CreateMovie(ctx, store.CreateMovieParams{
		SeasonID: seasonID, SubmittedBy: alice.ID, Title: "Unknown Film",
	})
	if err != nil {
		t.Fatalf("create sparse movie: %v", err)
	}
	t.Logf("sparse submission round trip: year=%s tmdb_id=%s trailer_url=%s (NULL, not 0 and not \"\")",
		show(sparse.Year), show(sparse.TmdbID), show(sparse.TrailerUrl))
	if sparse.Year != nil || sparse.TmdbID != nil || sparse.TrailerUrl != nil {
		t.Error("a NULL column came back as a non-nil pointer")
	}
	if _, err := q.CreateMovie(ctx, store.CreateMovieParams{
		SeasonID: seasonID, SubmittedBy: bob.ID, Title: "Bob's Pick",
	}); err != nil {
		t.Fatalf("bob's movie: %v", err)
	}

	t.Logf("alice count=%d bob count=%d", count(alice), count(bob))
	if got := count(alice); got != 2 {
		t.Errorf("alice count = %d, want 2", got)
	}
	if got := count(bob); got != 1 {
		t.Errorf("bob count = %d, want 1", got)
	}

	// Withdrawing a film must free the slot, or deleting and resubmitting would
	// permanently burn one.
	if _, err := q.SetMovieHidden(ctx, store.SetMovieHiddenParams{ID: first.ID, Hidden: true}); err != nil {
		t.Fatalf("hide: %v", err)
	}
	t.Logf("after hiding one of alice's two: alice count=%d", count(alice))
	if got := count(alice); got != 1 {
		t.Errorf("hidden movie still counts against the cap: %d", got)
	}

	visible, err := q.ListVisibleMoviesForSeason(ctx, seasonID)
	if err != nil {
		t.Fatalf("list visible: %v", err)
	}
	titles := make([]string, 0, len(visible))
	for _, m := range visible {
		titles = append(titles, m.Title)
	}
	t.Logf("visible slate: %v", titles)
	if len(visible) != 2 {
		t.Errorf("visible slate = %d movies, want 2", len(visible))
	}

	own, err := q.ListPersonMoviesForSeason(ctx, store.ListPersonMoviesForSeasonParams{
		SeasonID: seasonID, SubmittedBy: alice.ID,
	})
	if err != nil {
		t.Fatalf("list own: %v", err)
	}
	if len(own) != int(count(alice)) {
		t.Errorf("own list (%d) disagrees with cap count (%d)", len(own), count(alice))
	}
	t.Logf("alice's own list agrees with the cap count: %d", len(own))
}

// lockedSeasonErrCode is the SQLSTATE raised by nap_reject_season_unlock and
// nap_reject_locked_season_delete (00008_integrity_guards.sql). A project-
// defined code rather than a standard one, so "you tried to edit the archive"
// is distinguishable from every other integrity error. The same constant
// lives in internal/store/integrity, which is a separate test binary and
// cannot be imported.
const lockedSeasonErrCode = "NAPLK"

// sqlstate pulls the five-character SQLSTATE out of an error, or "" when the
// error did not come from Postgres. Asserting on the code rather than on the
// message is what makes the assertion about the guard instead of about its
// wording.
func sqlstate(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// TestSeasonStateTracksLockedAt proves the CHECK constraint cannot be violated
// through the generated query, and that `locked` is a one-way door.
//
// This test used to walk voting -> locked -> submitting -> locked -> draft and
// assert that state was freely settable in any order. Two of those steps
// unlock a locked season, which 00008's nap_reject_season_unlock refuses with
// SQLSTATE NAPLK, so the test asserted a capability the schema forbids and was
// the integration branch's standing red (nap-3wo). Nobody saw it for hours
// because the whole package skipped without TEST_DATABASE_URL (nap-gn1).
//
// Adjudicated: `locked` is terminal for accidental transitions, so the TEST
// changes and the trigger stands. The rewrite therefore asserts three things:
//
//   - forward transitions work, and locked_at tracks state in both directions;
//   - re-locking a locked season keeps the ORIGINAL lock time, which is what
//     the COALESCE in UpdateSeasonState exists for;
//   - an unlock through the generated query, without the escape hatch, is
//     rejected with NAPLK and leaves the season exactly as it was.
//
// That last one is the valuable half. internal/store/integrity pins the guard
// against hand-written SQL; this pins it against the query the application
// actually calls, which is the path a season would really be unlocked by.
func TestSeasonStateTracksLockedAt(t *testing.T) {
	pool := connect(t)
	tx := begin(t, pool)
	q := store.New(tx)
	ctx := context.Background()

	seasonID := seedSeason(t, tx, 2028, 2)

	// Windows first: once the season is locked it cannot be edited, and the
	// clearable-windows assertion is about a season still being run.
	var ts pgtype.Timestamptz
	if err := ts.Scan(nil); err != nil {
		t.Fatalf("null ts: %v", err)
	}
	w, err := q.UpdateSeasonWindows(ctx, store.UpdateSeasonWindowsParams{
		ID: seasonID, SubmitOpensAt: ts, VoteOpensAt: ts, VoteClosesAt: ts,
	})
	if err != nil {
		t.Fatalf("clear windows: %v", err)
	}
	t.Logf("cleared windows: submit=%v vote_opens=%v vote_closes=%v",
		w.SubmitOpensAt.Valid, w.VoteOpensAt.Valid, w.VoteClosesAt.Valid)

	// Forward through the season's life. seedSeason starts at 'submitting'.
	for _, state := range []string{"draft", "submitting", "voting", "locked"} {
		s, err := q.UpdateSeasonState(ctx, store.UpdateSeasonStateParams{ID: seasonID, State: state})
		if err != nil {
			t.Fatalf("set state %s: %v", state, err)
		}
		t.Logf("state=%-11s locked_at set=%v", s.State, s.LockedAt.Valid)
		if (s.State == "locked") != s.LockedAt.Valid {
			t.Errorf("state %q with locked_at.Valid=%v violates season_locked_has_timestamp", s.State, s.LockedAt.Valid)
		}
	}

	var lockedAt pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `SELECT locked_at FROM season WHERE id = $1`, seasonID).Scan(&lockedAt); err != nil {
		t.Fatalf("read locked_at: %v", err)
	}
	if !lockedAt.Valid {
		t.Fatal("a locked season has no locked_at")
	}

	// Re-locking is not an unlock, so the trigger lets it through -- and the
	// COALESCE keeps the moment the season first locked rather than stamping
	// it again. Without that, re-running the lock would silently rewrite when
	// the archive says the year closed.
	relocked, err := q.UpdateSeasonState(ctx, store.UpdateSeasonStateParams{ID: seasonID, State: "locked"})
	if err != nil {
		t.Fatalf("re-lock a locked season: %v", err)
	}
	if !relocked.LockedAt.Time.Equal(lockedAt.Time) {
		t.Errorf("re-locking moved locked_at from %v to %v; COALESCE in UpdateSeasonState should preserve it",
			lockedAt.Time, relocked.LockedAt.Time)
	}
	t.Logf("re-locking kept locked_at at %v", lockedAt.Time)

	// The guard. A locked season is the published archive, and leaving
	// 'locked' is what would lift the delete guards off its ballots, roster,
	// slate and result -- so the generated query has to be refused too.
	//
	// Inside a savepoint, because a rejected statement aborts the surrounding
	// transaction and there are assertions after this one. Rolling the
	// savepoint back is also what lets the "nothing moved" check below read
	// the row at all.
	for _, state := range []string{"draft", "submitting", "voting"} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, err = store.New(sp).UpdateSeasonState(ctx, store.UpdateSeasonStateParams{ID: seasonID, State: state})
		if err == nil {
			t.Errorf("UpdateSeasonState unlocked a locked season to %q", state)
		} else if got := sqlstate(err); got != lockedSeasonErrCode {
			t.Errorf("unlock to %q: SQLSTATE = %q, want %q: %v", state, got, lockedSeasonErrCode, err)
		} else {
			t.Logf("unlock to %-11s rejected with SQLSTATE %s", state, got)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback savepoint: %v", err)
		}
	}

	// And the refusals left the season alone, rather than half-applying.
	after, err := q.GetSeason(ctx, seasonID)
	if err != nil {
		t.Fatalf("read the season back: %v", err)
	}
	if after.State != "locked" {
		t.Errorf("season state = %q, want locked", after.State)
	}
	if !after.LockedAt.Valid || !after.LockedAt.Time.Equal(lockedAt.Time) {
		t.Errorf("locked_at = %v (valid=%v), want %v", after.LockedAt.Time, after.LockedAt.Valid, lockedAt.Time)
	}
}

// TestSeasonListingAndMembers covers the archive queries and the joined member
// list the admin whitelist page reads.
func TestSeasonListingAndMembers(t *testing.T) {
	pool := connect(t)
	tx := begin(t, pool)
	q := store.New(tx)
	ctx := context.Background()

	published := seedSeason(t, tx, 2029, 2)
	var draft uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO season (year, name, state) VALUES (2030, 'Next Year', 'draft') RETURNING id`,
	).Scan(&draft); err != nil {
		t.Fatalf("draft season: %v", err)
	}

	all, err := q.ListSeasons(ctx)
	if err != nil {
		t.Fatalf("list seasons: %v", err)
	}
	pub, err := q.ListPublishedSeasons(ctx)
	if err != nil {
		t.Fatalf("list published: %v", err)
	}
	current, err := q.GetCurrentSeason(ctx)
	if err != nil {
		t.Fatalf("current season: %v", err)
	}
	// Every assertion below is scoped to the two seasons this test seeded.
	//
	// The original versions compared global counts (len(all) == len(pub)+1)
	// and the global current year, which made the whole test pass only against
	// an empty database: a single unrelated published season with a later year
	// turns both red for reasons unconnected to the behaviour under test. The
	// suite is supposed to be isolated by BEGIN/ROLLBACK, and an assertion
	// against a global list quietly gives that up.
	t.Logf("ListSeasons=%d ListPublishedSeasons=%d current=%d (2030 is a draft and must not be current)",
		len(all), len(pub), current.Year)

	if !containsSeason(all, draft) {
		t.Errorf("ListSeasons omitted the draft season; it is the admin listing and must include drafts")
	}
	if containsSeason(pub, draft) {
		t.Errorf("draft season leaked into ListPublishedSeasons")
	}
	if !containsSeason(pub, published) {
		t.Errorf("ListPublishedSeasons omitted the published season this test seeded")
	}
	// The point of the draft rule: a half-configured later season must not
	// shadow the published one. Asserting on the id rather than on the year
	// keeps this true no matter what else is in the database.
	if current.ID == draft {
		t.Errorf("a draft season became current, shadowing the published season")
	}

	zoe := signIn(t, q, "zoe@example.com", "sub-zoe", "Zoe")
	amy := signIn(t, q, "amy@example.com", "sub-amy", "Amy")
	if _, err := q.UpsertSeasonMember(ctx, store.UpsertSeasonMemberParams{
		SeasonID: published, PersonID: zoe.ID, IsAdmin: true, SubmitLimit: ptr(int32(3)),
	}); err != nil {
		t.Fatalf("member zoe: %v", err)
	}
	if _, err := q.UpsertSeasonMember(ctx, store.UpsertSeasonMemberParams{
		SeasonID: published, PersonID: amy.ID,
	}); err != nil {
		t.Fatalf("member amy: %v", err)
	}

	members, err := q.ListSeasonMembers(ctx, published)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	for _, m := range members {
		t.Logf("member %-18s admin=%-5v raw submit_limit=%s", m.Email, m.SeasonMember.IsAdmin, show(m.SeasonMember.SubmitLimit))
	}
	if len(members) != 2 || members[0].Email != "amy@example.com" {
		t.Errorf("members not returned sorted by email: %d rows", len(members))
	}

	if err := q.DeleteSeasonMember(ctx, store.DeleteSeasonMemberParams{
		SeasonID: published, PersonID: amy.ID,
	}); err != nil {
		t.Fatalf("delete member: %v", err)
	}
	if _, err := q.GetSeasonMember(ctx, store.GetSeasonMemberParams{
		SeasonID: published, PersonID: amy.ID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("deleted member still present: %v", err)
	}
	t.Logf("after DeleteSeasonMember, GetSeasonMember returns pgx.ErrNoRows")
}

// TestUUIDMapping pins down the type every id in this schema is expressed as.
// The mapping is a config choice, so it is worth a test that fails loudly if
// someone changes it: uuid.UUID is comparable with ==, prints as a uuid, and
// needs no conversion at the HTTP boundary, none of which is true of
// pgtype.UUID.
func TestUUIDMapping(t *testing.T) {
	pool := connect(t)
	tx := begin(t, pool)
	q := store.New(tx)
	ctx := context.Background()

	seasonID := seedSeason(t, tx, 2032, 2)
	person := signIn(t, q, "uuid@example.com", "sub-uuid", "Uuid")

	// A uuid that arrived as text on a request, as a handler will see it.
	parsed, err := uuid.Parse(seasonID.String())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed != seasonID {
		t.Error("uuid does not survive String/Parse round trip")
	}
	t.Logf("season id round trips through text: %s (comparable with ==)", seasonID)

	movie, err := q.CreateMovie(ctx, store.CreateMovieParams{
		SeasonID: seasonID, SubmittedBy: person.ID, Title: "Id Test",
	})
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}
	if movie.SeasonID != seasonID || movie.SubmittedBy != person.ID {
		t.Error("ids did not round trip through the database")
	}
	t.Logf("movie.season_id == season id: %v, movie.submitted_by == person id: %v",
		movie.SeasonID == seasonID, movie.SubmittedBy == person.ID)

	// result.winner_movie_id is the one nullable uuid in the schema: a tie in
	// the final round has no winner. It must decode as nil, not as the
	// all-zeroes uuid, or "nobody won" and "the nil uuid won" become the same
	// value. No query returns it yet, so this checks the mapping directly.
	if _, err := tx.Exec(ctx,
		`INSERT INTO result (season_id, winner_movie_id) VALUES ($1, NULL)`, seasonID); err != nil {
		t.Fatalf("insert tied result: %v", err)
	}
	var winner *uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT winner_movie_id FROM result WHERE season_id = $1`, seasonID).Scan(&winner); err != nil {
		t.Fatalf("read tied result: %v", err)
	}
	t.Logf("tied season winner_movie_id decodes as %v (nil, not the zero uuid)", winner)
	if winner != nil {
		t.Errorf("NULL winner decoded as %v", *winner)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE result SET winner_movie_id = $2 WHERE season_id = $1`, seasonID, movie.ID); err != nil {
		t.Fatalf("set winner: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT winner_movie_id FROM result WHERE season_id = $1`, seasonID).Scan(&winner); err != nil {
		t.Fatalf("read decided result: %v", err)
	}
	if winner == nil || *winner != movie.ID {
		t.Errorf("decided winner = %v, want %v", winner, movie.ID)
	}
	t.Logf("decided season winner_movie_id decodes as %v", winner)
}

// oldUpsertSQL is UpsertPersonOnSignIn exactly as it shipped before the
// takeover guard: a DO UPDATE with no WHERE. It is inlined here, rather than
// described, so the test can demonstrate the vulnerability on a live server
// instead of asserting that a fix fixes something nobody ever saw break.
const oldUpsertSQL = `
INSERT INTO person (email, email_normalized, google_sub, display_name)
VALUES ($1, $1, $2, $3)
ON CONFLICT (email_normalized) DO UPDATE
SET email        = EXCLUDED.email,
    google_sub   = COALESCE(EXCLUDED.google_sub, person.google_sub),
    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), person.display_name)
RETURNING id, google_sub, display_name`

// TestSignInTakeoverIsBlocked is the regression test for the account-takeover
// defect in UpsertPersonOnSignIn.
//
// The attack needs nothing exotic. Google ties sub to the account object, not
// to the address, so a recreated Google account or a recycled address produces
// a new sub for an email another person already holds. Sign-in tries the sub
// lookup first, misses, and falls through to the upsert.
//
// The test proves both halves: that the old query really was exploitable, and
// that the guard stops it while leaving every legitimate path working.
func TestSignInTakeoverIsBlocked(t *testing.T) {
	pool := connect(t)
	tx := begin(t, pool)
	q := store.New(tx)
	ctx := context.Background()

	const victimEmail = "alice@example.com"

	// Alice signs in, runs a season and submits a film. This is what a
	// takeover actually gets you.
	alice := signIn(t, q, victimEmail, "sub-alice", "Alice")
	seasonID := seedSeason(t, tx, 2031, 2)
	if _, err := q.UpsertSeasonMember(ctx, store.UpsertSeasonMemberParams{
		SeasonID: seasonID, PersonID: alice.ID, IsAdmin: true,
	}); err != nil {
		t.Fatalf("make alice admin: %v", err)
	}
	if _, err := q.CreateMovie(ctx, store.CreateMovieParams{
		SeasonID: seasonID, SubmittedBy: alice.ID, Title: "Alice's Pick",
	}); err != nil {
		t.Fatalf("alice submits: %v", err)
	}
	t.Logf("victim: id=%v sub=%q admin=true submissions=1", alice.ID, *alice.GoogleSub)

	// --- Half one: the old query was genuinely exploitable. ---
	//
	// Run it inside a savepoint so the damage is rolled back before the real
	// query is exercised.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}

	var stolenID uuid.UUID
	var stolenSub, stolenName string
	if err := sp.QueryRow(ctx, oldUpsertSQL, victimEmail, "sub-mallory", "Mallory").
		Scan(&stolenID, &stolenSub, &stolenName); err != nil {
		t.Fatalf("old upsert: %v", err)
	}

	var personCount, inheritedSubmissions int
	var inheritedAdmin bool
	if err := sp.QueryRow(ctx, `SELECT count(*) FROM person`).Scan(&personCount); err != nil {
		t.Fatalf("count person: %v", err)
	}
	if err := sp.QueryRow(ctx,
		`SELECT sm.is_admin, (SELECT count(*) FROM movie m WHERE m.submitted_by = sm.person_id)
		 FROM season_member sm WHERE sm.person_id = $1`, stolenID).
		Scan(&inheritedAdmin, &inheritedSubmissions); err != nil {
		t.Fatalf("inherited privileges: %v", err)
	}

	if stolenID != alice.ID {
		t.Fatal("setup wrong: the old query did not collide on Alice's row")
	}
	if stolenSub != "sub-mallory" || !inheritedAdmin || inheritedSubmissions != 1 || personCount != 1 {
		t.Fatalf("expected the OLD query to be exploitable, but it was not: "+
			"sub=%q admin=%v submissions=%d rows=%d",
			stolenSub, inheritedAdmin, inheritedSubmissions, personCount)
	}
	t.Logf("UNFIXED query: row count still %d, sub is now %q (%q), inherited admin=%v submissions=%d"+
		" -- takeover reproduced",
		personCount, stolenSub, stolenName, inheritedAdmin, inheritedSubmissions)

	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback savepoint: %v", err)
	}

	// --- Half two: the shipped query refuses the same attack. ---
	_, err = q.UpsertPersonOnSignIn(ctx, store.UpsertPersonOnSignInParams{
		Email:           victimEmail,
		EmailNormalized: victimEmail,
		GoogleSub:       "sub-mallory",
		DisplayName:     "Mallory",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("takeover was not refused: err = %v, want pgx.ErrNoRows", err)
	}
	t.Logf("FIXED query:   %v -- read as identity collision, NOT as person-not-found", err)

	// Alice's row must be untouched, privileges included.
	after, err := q.GetPersonByGoogleSub(ctx, "sub-alice")
	if err != nil {
		t.Fatalf("alice lost her row: %v", err)
	}
	if after.ID != alice.ID || after.DisplayName != "Alice" {
		t.Errorf("alice's row was modified: %+v", after)
	}
	if _, err := q.GetPersonByGoogleSub(ctx, "sub-mallory"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("mallory's sub reached the table: %v", err)
	}
	t.Logf("victim intact: id=%v sub=%q display_name=%q", after.ID, *after.GoogleSub, after.DisplayName)

	// --- The legitimate paths must still work. ---

	// A whitelisted row that has never signed in is unclaimed, so the first
	// Google identity to present that address takes it.
	var whitelistedID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO person (email, email_normalized, display_name)
		 VALUES ($1, $1, '') RETURNING id`, "bob@example.com").Scan(&whitelistedID); err != nil {
		t.Fatalf("whitelist bob: %v", err)
	}
	claimed := signIn(t, q, "bob@example.com", "sub-bob", "Bob")
	if claimed.ID != whitelistedID {
		t.Errorf("first sign-in did not claim the whitelisted row: %v != %v", claimed.ID, whitelistedID)
	}
	t.Logf("unclaimed row: claimed by %q on first sign-in", *claimed.GoogleSub)

	// The same person signing in again is not a collision.
	repeat := signIn(t, q, victimEmail, "sub-alice", "Alice Renamed")
	if repeat.ID != alice.ID || repeat.DisplayName != "Alice Renamed" {
		t.Errorf("repeat sign-in broke: %+v", repeat)
	}
	t.Logf("same subject:  repeat sign-in still updates (display_name=%q)", repeat.DisplayName)
}

// TestNoActionVersusRestrictIsNotCosmetic keeps the comment on
// ballot_entry_movie_fkey honest.
//
// That comment used to claim RESTRICT is checked per row while NO ACTION waits
// for end of statement. It was false, and because it read plausibly it
// convinced two reviews before anyone executed it. The remedy for a comment
// that lies is not a better-written comment; it is a test that fails when the
// claim stops being true. So this builds the trap on a live server.
func TestNoActionVersusRestrictIsNotCosmetic(t *testing.T) {
	pool := connect(t)
	tx := begin(t, pool)
	ctx := context.Background()

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec %.60q: %v", sql, err)
		}
	}
	mustExec(`CREATE TABLE fk_parent (id int PRIMARY KEY)`)
	mustExec(`CREATE TABLE fk_child_na (id int PRIMARY KEY, p int,
	            CONSTRAINT fk_na FOREIGN KEY (p) REFERENCES fk_parent(id)
	            ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED)`)
	mustExec(`CREATE TABLE fk_child_re (id int PRIMARY KEY, p int,
	            CONSTRAINT fk_re FOREIGN KEY (p) REFERENCES fk_parent(id)
	            ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED)`)

	// 1. pg_constraint reports both as deferred. This is the column a reviewer
	//    reaches for, and for RESTRICT it is not the truth.
	for _, name := range []string{"fk_na", "fk_re"} {
		var deferrable, deferred bool
		if err := tx.QueryRow(ctx,
			`SELECT condeferrable, condeferred FROM pg_constraint WHERE conname = $1`, name).
			Scan(&deferrable, &deferred); err != nil {
			t.Fatalf("pg_constraint %s: %v", name, err)
		}
		if !deferrable || !deferred {
			t.Errorf("pg_constraint.%s = (%v,%v); the premise of this test is that it "+
				"reports BOTH as deferred", name, deferrable, deferred)
		}
		t.Logf("pg_constraint %-5s condeferrable=%v condeferred=%v", name, deferrable, deferred)
	}

	// 2. pg_trigger tells the truth: RESTRICT's delete-side trigger was
	//    silently downgraded to immediate.
	countImmediateTriggers := func(constraint string) int {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM pg_constraint c JOIN pg_trigger t ON t.tgconstraint = c.oid
			 WHERE c.conname = $1 AND NOT t.tgdeferrable`, constraint).Scan(&n); err != nil {
			t.Fatalf("pg_trigger %s: %v", constraint, err)
		}
		return n
	}
	if n := countImmediateTriggers("fk_na"); n != 0 {
		t.Errorf("NO ACTION has %d non-deferrable trigger(s), want 0", n)
	}
	if n := countImmediateTriggers("fk_re"); n == 0 {
		t.Error("RESTRICT has no non-deferrable trigger: the silent downgrade this " +
			"comment warns about no longer happens, so the comment needs rewriting")
	} else {
		t.Logf("pg_trigger    fk_re has %d non-deferrable trigger(s) -- the silent downgrade", n)
	}

	// 3. The behavioural consequence, which is what actually matters.
	mustExec(`INSERT INTO fk_parent VALUES (1),(2)`)
	mustExec(`INSERT INTO fk_child_na VALUES (1,1)`)
	mustExec(`INSERT INTO fk_child_re VALUES (1,2)`)

	// NO ACTION: delete the parent while a child still points at it, then tidy
	// up. Forcing the check proves it really was deferred until now.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	if _, err := sp.Exec(ctx, `DELETE FROM fk_parent WHERE id = 1`); err != nil {
		t.Fatalf("NO ACTION: parent delete should have been deferred, got: %v", err)
	}
	if _, err := sp.Exec(ctx, `DELETE FROM fk_child_na WHERE p = 1`); err != nil {
		t.Fatalf("NO ACTION: child delete: %v", err)
	}
	if _, err := sp.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		t.Fatalf("NO ACTION: deferred check failed at the point of forcing it: %v", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	t.Logf("behaviour     NO ACTION: parent-then-child in one txn -> check deferred, passes")

	// RESTRICT: the identical first statement, with the identical DEFERRABLE
	// INITIALLY DEFERRED clause, fails on the spot.
	sp2, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	_, err = sp2.Exec(ctx, `DELETE FROM fk_parent WHERE id = 2`)
	if err == nil {
		t.Error("RESTRICT deferred its check: the silent downgrade no longer happens, " +
			"so the comment on ballot_entry_movie_fkey needs rewriting")
	} else {
		t.Logf("behaviour     RESTRICT:  same clause, same statement -> %v", err)
	}
	if err := sp2.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	// 4. The false claim itself: with neither constraint deferred, both are
	//    end-of-statement, so a CTE removing child and parent together passes
	//    under either. This is the assertion that would have caught the
	//    original comment.
	mustExec(`CREATE TABLE fk_p2 (id int PRIMARY KEY)`)
	mustExec(`CREATE TABLE fk_c2_na (id int PRIMARY KEY, p int REFERENCES fk_p2(id) ON DELETE NO ACTION)`)
	mustExec(`CREATE TABLE fk_c2_re (id int PRIMARY KEY, p int REFERENCES fk_p2(id) ON DELETE RESTRICT)`)
	for i, child := range []string{"fk_c2_na", "fk_c2_re"} {
		id := i + 1
		mustExec(`INSERT INTO fk_p2 VALUES ($1)`, id)
		mustExec(`INSERT INTO `+child+` VALUES ($1,$1)`, id)
		_, err := tx.Exec(ctx,
			`WITH gone AS (DELETE FROM `+child+` WHERE p = $1 RETURNING p)
			 DELETE FROM fk_p2 WHERE id IN (SELECT p FROM gone)`, id)
		if err != nil {
			t.Errorf("%s: single-statement delete failed (%v). If only one of these two "+
				"fails, the old per-row-vs-end-of-statement claim is true after all "+
				"and this test is what is wrong", child, err)
		}
	}
	t.Log("behaviour     non-deferrable: NO ACTION and RESTRICT both pass the same " +
		"single-statement delete -- they are NOT per-row vs end-of-statement")

	// 5. And the shipped constraints really are NO ACTION ('a'), not RESTRICT
	//    ('r'). Everything above is about a distinction that only matters if
	//    this holds.
	for _, name := range []string{"ballot_entry_movie_fkey", "result_winner_movie_fkey"} {
		var delType string
		if err := tx.QueryRow(ctx,
			`SELECT confdeltype FROM pg_constraint WHERE conname = $1`, name).Scan(&delType); err != nil {
			t.Fatalf("confdeltype %s: %v", name, err)
		}
		if delType != "a" {
			t.Errorf("%s has confdeltype %q, want \"a\" (NO ACTION)", name, delType)
		}
		t.Logf("shipped       %-24s confdeltype=%q (a = NO ACTION)", name, delType)
	}
}
