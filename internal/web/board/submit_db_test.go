//go:build integration

package board

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// These tests run against a real Postgres and nothing else, because every
// question they ask is a question about Postgres: does the schema accept a
// submission from a non-member (it does, which is the whole of nap-90j), and
// does the cap hold when two requests arrive at once (it does not, without the
// lock).
//
// A fake would answer both the way this file's author expected, which is the
// failure mode the project has already hit eleven times. `go test ./...` with
// no TEST_DATABASE_URL skips them and prints ok, so a green run of that alone
// proves nothing about this file.

// newTestPool opens a pool against TEST_DATABASE_URL, skipping when it is
// unset so `go test ./...` still works without Docker.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is not set. Under -tags=integration the database is the point,\nso an absent DSN is a broken invocation rather than a reason to report ok.\nRun `make test-integration`, which starts and migrates a throwaway database.")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

// newDBService is a Service with a real pool and no read path.
//
// Store is nil on purpose. These tests call createSubmission directly, which
// goes through Beginner and never touches Store, so a nil there is a guarantee
// that the write path really is self-contained rather than quietly leaning on
// a read the caller did earlier.
func newDBService(pool *pgxpool.Pool) *Service {
	return &Service{opts: Options{DB: pool}}
}

// fixture is one disposable season with people in and out of it.
//
// Each test gets its own season row with a unique year, so the tests can run
// against a database that already has data in it and against each other. The
// year counter starts well above any real season and below the 2200 ceiling in
// season_year_range.
type fixture struct {
	pool     *pgxpool.Pool
	seasonID uuid.UUID
	year     int32
}

var nextFixtureYear = struct {
	sync.Mutex
	n int32
}{n: 2100}

func newFixture(t *testing.T, pool *pgxpool.Pool, state string, defaultLimit int32) *fixture {
	t.Helper()

	nextFixtureYear.Lock()
	year := nextFixtureYear.n
	nextFixtureYear.n++
	nextFixtureYear.Unlock()

	ctx := context.Background()

	var seasonID uuid.UUID
	err := pool.QueryRow(ctx, `
INSERT INTO season (year, name, state, default_submit_limit, locked_at)
VALUES ($1, $2, $3, $4, CASE WHEN $3 = 'locked' THEN now() ELSE NULL END)
RETURNING id`,
		year, fmt.Sprintf("Fixture %d", year), state, defaultLimit,
	).Scan(&seasonID)
	if err != nil {
		t.Fatalf("create season: %v", err)
	}

	f := &fixture{pool: pool, seasonID: seasonID, year: year}

	// Deleting the season cascades to its movies and its roster. The people
	// are deleted separately because movie.submitted_by is ON DELETE RESTRICT
	// against person, so they can only go once the films have.
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := pool.Exec(ctx, `DELETE FROM season WHERE id = $1`, seasonID); err != nil {
			t.Errorf("cleanup season: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`DELETE FROM person WHERE email_normalized LIKE $1`,
			fmt.Sprintf("%%@fixture-%d.test", year),
		); err != nil {
			t.Errorf("cleanup people: %v", err)
		}
	})

	return f
}

// person creates a person row, with no membership of anything.
func (f *fixture) person(t *testing.T, local string) uuid.UUID {
	t.Helper()

	email := fmt.Sprintf("%s@fixture-%d.test", local, f.year)

	var id uuid.UUID
	err := f.pool.QueryRow(context.Background(), `
INSERT INTO person (email, email_normalized, display_name)
VALUES ($1, $1, $2)
RETURNING id`, email, local).Scan(&id)
	if err != nil {
		t.Fatalf("create person %s: %v", email, err)
	}

	return id
}

// member puts a person on this season's whitelist. limit is the per-person
// override: nil leaves it NULL, which means "use the season default".
func (f *fixture) member(t *testing.T, personID uuid.UUID, limit *int32) {
	t.Helper()

	_, err := f.pool.Exec(context.Background(), `
INSERT INTO season_member (season_id, person_id, submit_limit)
VALUES ($1, $2, $3)`, f.seasonID, personID, limit)
	if err != nil {
		t.Fatalf("create membership: %v", err)
	}
}

// liveMovies is how many visible films this person has in the season. It is
// the number the cap is about, read straight from the table rather than from
// anything the handler returned.
func (f *fixture) liveMovies(t *testing.T, personID uuid.UUID) int {
	t.Helper()

	var n int
	err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM movie WHERE season_id = $1 AND submitted_by = $2 AND NOT hidden`,
		f.seasonID, personID,
	).Scan(&n)
	if err != nil {
		t.Fatalf("count movies: %v", err)
	}

	return n
}

func aDraft(title string) draft {
	return draft{title: title, description: ""}
}

// TestSchemaAcceptsWhatTheHandlerMustRefuse is nap-90j stated as a fact rather
// than as a worry.
//
// It writes a submission from a non-member straight into the table, with no
// handler involved, and it SUCCEEDS. movie has foreign keys to season and to
// person but none to season_member, so the database has no opinion about the
// whitelist. If this test ever starts failing, somebody has added the
// constraint and the handler check below has become a second line of defence
// rather than the only one -- which is a good day, and a reason to come back
// here and say so rather than to delete the check.
func TestSchemaAcceptsWhatTheHandlerMustRefuse(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	stranger := f.person(t, "stranger")

	_, err := pool.Exec(context.Background(), `
INSERT INTO movie (season_id, submitted_by, title)
VALUES ($1, $2, $3)`, f.seasonID, stranger, "Smuggled In")
	if err != nil {
		t.Fatalf("the schema refused a non-member's submission; nap-90j may have been "+
			"fixed in the schema, in which case update this test rather than deleting it: %v", err)
	}

	if got := f.liveMovies(t, stranger); got != 1 {
		t.Fatalf("live movies = %d, want 1", got)
	}
	t.Log("confirmed: the database accepts a submission from someone who is not on the season's whitelist")
}

// TestCreateSubmissionRefusesANonMember is the same person going through the
// handler instead.
func TestCreateSubmissionRefusesANonMember(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)

	stranger := f.person(t, "stranger")

	_, err := s.createSubmission(context.Background(), f.seasonID, stranger, aDraft("Smuggled In"))

	if !errors.Is(err, errNotMember) {
		t.Fatalf("err = %v, want errNotMember", err)
	}
	if got := f.liveMovies(t, stranger); got != 0 {
		t.Errorf("live movies = %d, want 0: the refusal did not roll back", got)
	}
}

// TestCreateSubmissionRefusesAfterMembershipIsRevoked is the case the
// middleware cannot cover.
//
// The whitelist gate runs at the start of the request and the write happens at
// the end of it. An admin removing somebody in between is exactly the window
// this check closes, and it is why the membership question is asked again
// inside the transaction rather than inherited from the context.
func TestCreateSubmissionRefusesAfterMembershipIsRevoked(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	// The gate would have passed this request. Now the admin acts.
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM season_member WHERE season_id = $1 AND person_id = $2`, f.seasonID, alice); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}

	_, err := s.createSubmission(context.Background(), f.seasonID, alice, aDraft("Too Late"))

	if !errors.Is(err, errNotMember) {
		t.Fatalf("err = %v, want errNotMember", err)
	}
	if got := f.liveMovies(t, alice); got != 0 {
		t.Errorf("live movies = %d, want 0", got)
	}
}

// TestCreateSubmissionHonoursAnExplicitZero: a member whose submit_limit is 0
// may vote and may not submit, and that is a different refusal from not being
// a member at all.
//
// The bug this guards against is the one the store layer's COALESCE exists to
// prevent, reintroduced one layer up: treating 0 as "unset" and falling back
// to the season default would turn a deliberate ban into two free picks.
func TestCreateSubmissionHonoursAnExplicitZero(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)

	voter := f.person(t, "voter")
	f.member(t, voter, ptrOf(int32(0)))

	_, err := s.createSubmission(context.Background(), f.seasonID, voter, aDraft("Not For You"))

	if !errors.Is(err, errBarred) {
		t.Fatalf("err = %v, want errBarred", err)
	}
	if errors.Is(err, errNotMember) {
		t.Error("a barred member was reported as a non-member; those are different answers")
	}
	if got := f.liveMovies(t, voter); got != 0 {
		t.Errorf("live movies = %d, want 0", got)
	}
}

// TestCreateSubmissionUsesTheOverrideNotTheDefault: a per-person submit_limit
// of 1 wins over a season default of 2.
func TestCreateSubmissionUsesTheOverrideNotTheDefault(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)

	alice := f.person(t, "alice")
	f.member(t, alice, ptrOf(int32(1)))

	ctx := context.Background()
	if _, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("The One")); err != nil {
		t.Fatalf("first submission: %v", err)
	}
	if _, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("The Second")); !errors.Is(err, errAtLimit) {
		t.Fatalf("second submission: err = %v, want errAtLimit", err)
	}

	if got := f.liveMovies(t, alice); got != 1 {
		t.Errorf("live movies = %d, want 1", got)
	}
}

// TestCreateSubmissionEnforcesTheCap is the ordinary two-per-person rule.
func TestCreateSubmissionEnforcesTheCap(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	for i, title := range []string{"The Thing", "Alien"} {
		if _, err := s.createSubmission(ctx, f.seasonID, alice, aDraft(title)); err != nil {
			t.Fatalf("submission %d (%s): %v", i+1, title, err)
		}
	}

	if _, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("The Fly")); !errors.Is(err, errAtLimit) {
		t.Fatalf("third submission: err = %v, want errAtLimit", err)
	}

	if got := f.liveMovies(t, alice); got != 2 {
		t.Errorf("live movies = %d, want 2", got)
	}
}

// TestHidingAFilmGivesTheSlotBack: the cap counts live films, so withdrawing
// one is not a permanently burnt pick.
func TestHidingAFilmGivesTheSlotBack(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 1)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	first, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Second Thoughts"))
	if err != nil {
		t.Fatalf("first submission: %v", err)
	}
	if _, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Blocked")); !errors.Is(err, errAtLimit) {
		t.Fatalf("expected the cap to bite: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE movie SET hidden = true WHERE id = $1`, first.ID); err != nil {
		t.Fatalf("hide movie: %v", err)
	}

	if _, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("The Replacement")); err != nil {
		t.Fatalf("after withdrawing: %v", err)
	}
	if got := f.liveMovies(t, alice); got != 1 {
		t.Errorf("live movies = %d, want 1", got)
	}
}

// TestCreateSubmissionRefusesOutsideTheWindow: the season window gates writes
// server-side, not just in the UI.
func TestCreateSubmissionRefusesOutsideTheWindow(t *testing.T) {
	for _, state := range []string{"draft", "voting", "locked"} {
		t.Run(state, func(t *testing.T) {
			pool := newTestPool(t)
			f := newFixture(t, pool, state, 2)
			s := newDBService(pool)

			alice := f.person(t, "alice")
			f.member(t, alice, nil)

			_, err := s.createSubmission(context.Background(), f.seasonID, alice, aDraft("Out Of Hours"))

			if !errors.Is(err, errWindowClosed) {
				t.Fatalf("err = %v, want errWindowClosed", err)
			}
			if got := f.liveMovies(t, alice); got != 0 {
				t.Errorf("live movies = %d, want 0", got)
			}
		})
	}
}

// TestCreateSubmissionWritesEveryColumn walks one good submission all the way
// into the row, including the two nullable columns that a pointer bug would
// turn into empty strings.
func TestCreateSubmissionWritesEveryColumn(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	d := draft{
		title:       "The Thing",
		year:        ptrOf(int32(1982)),
		trailerURL:  ptrOf("https://www.youtube.com/watch?v=4rgXFbZjTU8"),
		description: "Practical effects, no CGI.",
	}

	movie, err := s.createSubmission(context.Background(), f.seasonID, alice, d)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	q := store.New(pool)
	saved, err := q.GetMovie(context.Background(), movie.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	switch {
	case saved.Title != d.title:
		t.Errorf("title = %q, want %q", saved.Title, d.title)
	case saved.Year == nil || *saved.Year != 1982:
		t.Errorf("year = %v, want 1982", saved.Year)
	case saved.TrailerUrl == nil || *saved.TrailerUrl != *d.trailerURL:
		t.Errorf("trailer_url = %v", saved.TrailerUrl)
	case saved.Description != d.description:
		t.Errorf("description = %q", saved.Description)
	case saved.SeasonID != f.seasonID || saved.SubmittedBy != alice:
		t.Errorf("wrong season or submitter: %v / %v", saved.SeasonID, saved.SubmittedBy)
	case saved.Hidden:
		t.Error("a new submission is hidden")
	case saved.TmdbID != nil:
		// A manual submission: nobody picked a film, so there is no id to
		// store. Since nap-eie a draft CAN carry one — see
		// TestCreateSubmissionWritesTheTMDBID — but a draft that does not must
		// still write NULL, because a fabricated id here would make an
		// unrelated film collide with it in movie_season_tmdb_unique_idx.
		t.Errorf("tmdb_id = %v, want NULL for a submission with no film picked", saved.TmdbID)
	}
}

// ---------------------------------------------------------------------------
// tmdb_id, and the constraint that has been waiting for it (nap-eie).
// ---------------------------------------------------------------------------

// TestCreateSubmissionWritesTheTMDBID: the column the schema has had since
// migration 00004 and that nothing has ever written to.
//
// It is what makes "have we watched this before" answerable across years,
// which was the original argument for the column, and it is what the
// duplicate check below is keyed on.
func TestCreateSubmissionWritesTheTMDBID(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	d := draft{
		title:       "Blade Runner",
		year:        ptrOf(int32(1982)),
		tmdbID:      ptrOf(int32(78)),
		trailerURL:  ptrOf("https://www.youtube.com/watch?v=eogpIG53Cis"),
		description: "In the smog-choked dystopian Los Angeles of 2019.",
	}

	movie, err := s.createSubmission(context.Background(), f.seasonID, alice, d)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	saved, err := store.New(pool).GetMovie(context.Background(), movie.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	switch {
	case saved.TmdbID == nil:
		t.Fatal("tmdb_id is NULL after a submission that carried one")
	case *saved.TmdbID != 78:
		t.Errorf("tmdb_id = %d, want 78", *saved.TmdbID)
	// The three things a picked film is supposed to arrive with, and the
	// reason the ticket exists: nobody typed any of them.
	case saved.Year == nil || *saved.Year != 1982:
		t.Errorf("year = %v, want 1982", saved.Year)
	case saved.TrailerUrl == nil || *saved.TrailerUrl != *d.trailerURL:
		t.Errorf("trailer_url = %v, want TMDB's", saved.TrailerUrl)
	case saved.Description == "":
		t.Error("description is empty; the synopsis did not make it to the row")
	}
}

// TestSameFilmTwiceInOneSeasonIsRefused exercises
// movie_season_tmdb_unique_idx, which has been in the schema since migration
// 00004 and has NEVER FIRED — because tmdb_id was written as NULL on every row
// and the index is partial on tmdb_id IS NOT NULL.
//
// Writing real ids switches it on. Two people picking the same film in one
// season is an ordinary Tuesday, and two rows for one film would split the
// ranked-choice vote between them, so the refusal is the feature. What this
// test proves is that it arrives as errAlreadyUp and not as a 500: the
// difference between "somebody already put that up, pick another" and an
// error page is the whole of whether this is usable.
func TestSameFilmTwiceInOneSeasonIsRefused(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 3)
	s := newDBService(pool)

	alice := f.person(t, "alice")
	bob := f.person(t, "bob")
	f.member(t, alice, nil)
	f.member(t, bob, nil)

	film := func() draft {
		return draft{title: "Blade Runner", year: ptrOf(int32(1982)), tmdbID: ptrOf(int32(78))}
	}

	if _, err := s.createSubmission(context.Background(), f.seasonID, alice, film()); err != nil {
		t.Fatalf("alice's submission: %v", err)
	}

	// Somebody else, same film.
	_, err := s.createSubmission(context.Background(), f.seasonID, bob, film())
	if !errors.Is(err, errAlreadyUp) {
		t.Fatalf("bob's duplicate = %v, want errAlreadyUp", err)
	}

	// And the same person, same film: the quota is not what stopped it.
	if _, err := s.createSubmission(context.Background(), f.seasonID, alice, film()); !errors.Is(err, errAlreadyUp) {
		t.Errorf("alice's own duplicate = %v, want errAlreadyUp", err)
	}
}

// The index is partial on NOT hidden, so withdrawing a film must hand the
// title back to the season. Without that, one person withdrawing their pick
// would permanently bar everybody from submitting that film for the year.
func TestWithdrawingAFilmFreesItsTMDBID(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 3)
	s := newDBService(pool)
	q := store.New(pool)

	alice := f.person(t, "alice")
	bob := f.person(t, "bob")
	f.member(t, alice, nil)
	f.member(t, bob, nil)

	film := func() draft {
		return draft{title: "Blade Runner", year: ptrOf(int32(1982)), tmdbID: ptrOf(int32(78))}
	}

	first, err := s.createSubmission(context.Background(), f.seasonID, alice, film())
	if err != nil {
		t.Fatalf("alice's submission: %v", err)
	}

	if _, err := q.SetMovieHidden(context.Background(), store.SetMovieHiddenParams{
		ID:     first.ID,
		Hidden: true,
	}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	if _, err := s.createSubmission(context.Background(), f.seasonID, bob, film()); err != nil {
		t.Errorf("after a withdrawal the film is still blocked: %v", err)
	}
}

// Two people submitting two DIFFERENT films must not collide, and a manual
// submission must never collide with anything: NULL is not equal to NULL in a
// unique index, so any number of films with no tmdb_id can be up at once.
func TestTMDBIDUniquenessDoesNotOverreach(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 4)
	s := newDBService(pool)

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	drafts := []draft{
		{title: "Blade Runner", tmdbID: ptrOf(int32(78))},
		{title: "Blade Runner 2049", tmdbID: ptrOf(int32(335984))},
		// Two manual submissions, both with no id at all.
		{title: "Hausu"},
		{title: "Begotten"},
	}

	for _, d := range drafts {
		if _, err := s.createSubmission(context.Background(), f.seasonID, alice, d); err != nil {
			t.Fatalf("submitting %q: %v", d.title, err)
		}
	}

	if n := f.liveMovies(t, alice); n != len(drafts) {
		t.Errorf("%d films on the board, want %d", n, len(drafts))
	}
}

// The same film in two different seasons is not a duplicate — the index is on
// (season_id, tmdb_id). This is the half of "have we watched this before" that
// has to stay ALLOWED: the answer is "yes, in 2024", not a refusal.
func TestTheSameFilmMayBeSubmittedInADifferentSeason(t *testing.T) {
	pool := newTestPool(t)
	s := newDBService(pool)

	first := newFixture(t, pool, "submitting", 2)
	second := newFixture(t, pool, "submitting", 2)

	alice := first.person(t, "alice")
	first.member(t, alice, nil)
	second.member(t, alice, nil)

	film := func() draft {
		return draft{title: "Blade Runner", tmdbID: ptrOf(int32(78))}
	}

	if _, err := s.createSubmission(context.Background(), first.seasonID, alice, film()); err != nil {
		t.Fatalf("first season: %v", err)
	}
	if _, err := s.createSubmission(context.Background(), second.seasonID, alice, film()); err != nil {
		t.Errorf("the same film in a later season was refused: %v", err)
	}
}

// The duplicate check is a caught constraint violation rather than a SELECT
// before the INSERT, precisely so it holds when two submissions arrive at
// once. This is that claim, tested the only way it can be.
func TestDuplicateFilmHoldsUnderConcurrency(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 5)
	s := newDBService(pool)

	const racers = 6

	people := make([]uuid.UUID, racers)
	for i := range people {
		people[i] = f.person(t, fmt.Sprintf("racer%d", i))
		f.member(t, people[i], nil)
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		wrote int
		other []error
	)

	start := make(chan struct{})

	for _, personID := range people {
		wg.Add(1)

		go func() {
			defer wg.Done()
			<-start

			_, err := s.createSubmission(context.Background(), f.seasonID, personID,
				draft{title: "Blade Runner", tmdbID: ptrOf(int32(78))})

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				wrote++
			case errors.Is(err, errAlreadyUp):
			default:
				other = append(other, err)
			}
		}()
	}

	close(start)
	wg.Wait()

	if wrote != 1 {
		t.Errorf("%d of %d concurrent submissions of one film were written, want exactly 1", wrote, racers)
	}
	// Every loser must get the refusal that says "pick another one", not a
	// 500. A raw constraint error here is an error page for five of six.
	if len(other) > 0 {
		t.Errorf("%d racers got an unclassified error, want all losers to see errAlreadyUp: %v", len(other), other)
	}
}

// TestCreateSubmissionOmitsBlankOptionalColumns: a form with only a title
// writes NULL rather than ” into year and trailer_url.
//
// ” in trailer_url would be rejected by movie_trailer_url_shape, so this is
// the difference between a working minimal submission and a 500.
func TestCreateSubmissionOmitsBlankOptionalColumns(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	d, errs := validate(vmForm("The Thing", "", "", ""))
	if errs.Any() {
		t.Fatalf("a title-only form was rejected: %+v", errs)
	}

	movie, err := s.createSubmission(context.Background(), f.seasonID, alice, d)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if movie.Year != nil || movie.TrailerUrl != nil {
		t.Errorf("year = %v, trailer_url = %v; both should be NULL", movie.Year, movie.TrailerUrl)
	}
}

// TestCapHoldsUnderConcurrency is the one that a transaction alone does not
// pass.
//
// Six requests arrive at the same instant from one person with two picks. At
// READ COMMITTED, six transactions each read a count of 0 and each insert, so
// a version of this handler with the transaction and without the FOR UPDATE
// puts six films on the board and reports every one of them as fine. The lock
// on the membership row is what makes the fifth one wait for the first and
// then see the film it wrote.
//
// It asserts on the table, not on the return values: what matters is how many
// rows exist afterwards.
func TestCapHoldsUnderConcurrency(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	const attempts = 6

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// One barrier, so the goroutines genuinely overlap rather than queueing up
	// behind each other's setup.
	start := make(chan struct{})
	var wg sync.WaitGroup

	results := make([]error, attempts)
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.createSubmission(ctx, f.seasonID, alice, aDraft(fmt.Sprintf("Film %d", i)))
			results[i] = err
		}()
	}

	close(start)
	wg.Wait()

	accepted := 0
	for i, err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, errAtLimit):
		default:
			t.Errorf("attempt %d failed for an unexpected reason: %v", i, err)
		}
	}

	if accepted != 2 {
		t.Errorf("accepted = %d, want 2", accepted)
	}
	if got := f.liveMovies(t, alice); got != 2 {
		t.Errorf("live movies = %d, want 2: the cap did not hold under concurrency", got)
	}
}

// TestConcurrentSubmittersDoNotBlockEachOther checks the other half of the
// lock's design: it is per person, so a busy evening does not serialise.
//
// If FOR UPDATE had been written without OF season_member it would also lock
// the joined season row, and every submission in the season would queue behind
// every other one.
func TestConcurrentSubmittersDoNotBlockEachOther(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	const people = 4

	ids := make([]uuid.UUID, people)
	for i := range people {
		ids[i] = f.person(t, fmt.Sprintf("voter%d", i))
		f.member(t, ids[i], nil)
	}

	var wg sync.WaitGroup
	errs := make([]error, people)
	for i := range people {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.createSubmission(ctx, f.seasonID, ids[i], aDraft(fmt.Sprintf("Pick %d", i)))
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("voter %d: %v", i, err)
		}
		if got := f.liveMovies(t, ids[i]); got != 1 {
			t.Errorf("voter %d: live movies = %d, want 1", i, got)
		}
	}
}

// vmForm is the four fields as a browser would post them.
func vmForm(title, year, trailer, description string) viewmodel.SubmitForm {
	return viewmodel.SubmitForm{
		Title:       title,
		Year:        year,
		TrailerURL:  trailer,
		Description: description,
	}
}

func ptrOf[T any](v T) *T { return &v }
