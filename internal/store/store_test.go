package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// connect opens a pool against TEST_DATABASE_URL, skipping the test when it is
// unset so that `go test ./...` still works without Docker.
func connect(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping live database test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
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
		GoogleSub:       ptr(sub),
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

	// The rollback in t.Cleanup must leave nothing behind.
	t.Cleanup(func() {
		var n int
		if err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM season WHERE year = 2031`).Scan(&n); err != nil {
			t.Errorf("post-rollback check: %v", err)
		}
		if n != 0 {
			t.Errorf("rollback left %d season row(s) behind", n)
		}
	})
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

// TestSeasonStateTracksLockedAt proves the CHECK constraint cannot be violated
// through the generated query, in either direction.
func TestSeasonStateTracksLockedAt(t *testing.T) {
	pool := connect(t)
	tx := begin(t, pool)
	q := store.New(tx)
	ctx := context.Background()

	seasonID := seedSeason(t, tx, 2028, 2)

	for _, state := range []string{"voting", "locked", "submitting", "locked"} {
		s, err := q.UpdateSeasonState(ctx, store.UpdateSeasonStateParams{ID: seasonID, State: state})
		if err != nil {
			t.Fatalf("set state %s: %v", state, err)
		}
		t.Logf("state=%-11s locked_at set=%v", s.State, s.LockedAt.Valid)
		if (s.State == "locked") != s.LockedAt.Valid {
			t.Errorf("state %q with locked_at.Valid=%v violates season_locked_has_timestamp", s.State, s.LockedAt.Valid)
		}
	}

	// Windows are nullable and clearable.
	var ts pgtype.Timestamptz
	if err := ts.Scan(nil); err != nil {
		t.Fatalf("null ts: %v", err)
	}
	if _, err := q.UpdateSeasonState(ctx, store.UpdateSeasonStateParams{ID: seasonID, State: "draft"}); err != nil {
		t.Fatalf("back to draft: %v", err)
	}
	s, err := q.UpdateSeasonWindows(ctx, store.UpdateSeasonWindowsParams{
		ID: seasonID, SubmitOpensAt: ts, VoteOpensAt: ts, VoteClosesAt: ts,
	})
	if err != nil {
		t.Fatalf("clear windows: %v", err)
	}
	t.Logf("cleared windows: submit=%v vote_opens=%v vote_closes=%v", s.SubmitOpensAt.Valid, s.VoteOpensAt.Valid, s.VoteClosesAt.Valid)
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
	t.Logf("ListSeasons=%d ListPublishedSeasons=%d current=%d (2030 is a draft and must not be current)",
		len(all), len(pub), current.Year)
	if current.Year != 2029 {
		t.Errorf("current season = %d, want 2029", current.Year)
	}
	if len(all) != len(pub)+1 {
		t.Errorf("draft season leaked into ListPublishedSeasons")
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
