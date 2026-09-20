//go:build integration

package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ERaith/nightofathousandpixels/internal/signin"
	"github.com/ERaith/nightofathousandpixels/internal/store"
)

// Ticket E3 (nap-7hn): editing and withdrawing your own submission.
//
// These run against a real Postgres for the same reason the submit tests do.
// Two of the questions here cannot be answered by a fake at all:
//
//   - Does the schema actually stop a hard delete of a ranked film? (It does,
//     at COMMIT, because migration 00008 made the constraint deferred -- which
//     is exactly the sort of thing a fake would get wrong in the reassuring
//     direction.)
//   - Does withdrawing give the pick back? That is a property of
//     CountPersonMoviesInSeason's `WHERE NOT hidden`, not of any Go code, and
//     asserting it against the real query is the only version that stays true
//     if somebody edits the query.

// viewerFor builds the signin.Viewer the write paths take, from rows that are
// really in the database rather than from struct literals. The season in
// particular has to be the real row: the transaction re-reads it by id and
// compares its state.
func (f *fixture) viewerFor(t *testing.T, personID uuid.UUID) signin.Viewer {
	t.Helper()

	q := store.New(f.pool)
	ctx := context.Background()

	person, err := q.GetPerson(ctx, personID)
	if err != nil {
		t.Fatalf("load person: %v", err)
	}
	season, err := q.GetSeason(ctx, f.seasonID)
	if err != nil {
		t.Fatalf("load season: %v", err)
	}
	member, err := q.GetSeasonMember(ctx, store.GetSeasonMemberParams{
		SeasonID: f.seasonID,
		PersonID: personID,
	})
	if err != nil {
		// A non-member is a legitimate viewer here: the gate that would have
		// stopped them is the thing under test, so the zero membership row is
		// the honest stand-in.
		member = store.SeasonMember{SeasonID: f.seasonID, PersonID: personID}
	}

	return signin.Viewer{Person: person, Season: season, Member: member}
}

// auditRows is how many audit entries exist for one action against one target.
func (f *fixture) auditRows(t *testing.T, action string, target uuid.UUID) int {
	t.Helper()

	var n int
	err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = $1 AND target = $2`,
		action, target.String(),
	).Scan(&n)
	if err != nil {
		t.Fatalf("count audit rows: %v", err)
	}

	return n
}

// hidden reads movie.hidden straight from the table.
func (f *fixture) hidden(t *testing.T, movieID uuid.UUID) bool {
	t.Helper()

	var h bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT hidden FROM movie WHERE id = $1`, movieID).Scan(&h); err != nil {
		t.Fatalf("read hidden: %v", err)
	}

	return h
}

// TestWithdrawGivesThePickBack is rule 4, asserted end to end.
//
// It is deliberately phrased as "then submit something else and watch it be
// accepted" rather than "check a counter", because the counter is not the
// thing anybody cares about: the person wants their second film on the board.
func TestWithdrawGivesThePickBack(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 1)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)
	v := f.viewerFor(t, alice)

	first, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Second Thoughts"))
	if err != nil {
		t.Fatalf("first submission: %v", err)
	}
	if _, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Blocked")); !errors.Is(err, errAtLimit) {
		t.Fatalf("expected the cap to bite before withdrawing: %v", err)
	}

	if _, err := s.withdrawSubmission(ctx, v, first.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if !f.hidden(t, first.ID) {
		t.Error("withdrawing did not set movie.hidden")
	}

	if _, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("The Replacement")); err != nil {
		t.Fatalf("after withdrawing, the pick did not come back: %v", err)
	}
	if got := f.liveMovies(t, alice); got != 1 {
		t.Errorf("live movies = %d, want 1", got)
	}
}

// TestWithdrawIsASoftDeleteEvenWhenRanked is rule 3, and it is the one that
// justifies the whole design.
//
// A film that a ballot ranked cannot be hard-deleted: ballot_entry's composite
// foreign key to movie(season_id, id) refuses it. Migration 00008 made that
// constraint DEFERRABLE INITIALLY DEFERRED, so the refusal arrives at COMMIT
// rather than at the DELETE -- which this test demonstrates rather than
// assumes, because "the statement succeeded" is exactly the observation that
// would mislead somebody into writing a hard delete.
func TestWithdrawIsASoftDeleteEvenWhenRanked(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)
	bob := f.person(t, "bob")
	f.member(t, bob, nil)

	movie, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Ranked By Somebody"))
	if err != nil {
		t.Fatalf("submission: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO ballot_entry (season_id, person_id, movie_id, rank) VALUES ($1, $2, $3, 1)`,
		f.seasonID, bob, movie.ID,
	); err != nil {
		t.Fatalf("rank the film: %v", err)
	}

	// The hard delete, attempted honestly, in its own transaction.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM movie WHERE id = $1`, movie.ID); err != nil {
		// Fine either way; the point is the commit below.
		t.Logf("the DELETE statement itself failed: %v", err)
	}
	if err := tx.Commit(ctx); err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("a ranked film was hard-deleted. The composite FK in migration 00005 is the reason withdrawing is a soft delete; if it is gone, this feature's design has lost its foundation")
	}
	_ = tx.Rollback(ctx)

	// The soft delete, which is the operation the constraint exists to force.
	if _, err := s.withdrawSubmission(ctx, f.viewerFor(t, alice), movie.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if !f.hidden(t, movie.ID) {
		t.Error("withdrawing did not set movie.hidden")
	}

	var ballots int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM ballot_entry WHERE movie_id = $1`, movie.ID).Scan(&ballots); err != nil {
		t.Fatalf("count ballots: %v", err)
	}
	if ballots != 1 {
		t.Errorf("ballot rows after withdrawing = %d, want 1 — withdrawing must not rewrite anybody's ballot", ballots)
	}
}

// TestEditAndWithdrawRefuseSomebodyElsesFilm is rule 1, checked on the server
// rather than by not rendering a button.
func TestEditAndWithdrawRefuseSomebodyElsesFilm(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)
	bob := f.person(t, "bob")
	f.member(t, bob, nil)

	movie, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Alice's Film"))
	if err != nil {
		t.Fatalf("submission: %v", err)
	}

	bobsView := f.viewerFor(t, bob)

	if _, err := s.updateSubmission(ctx, bobsView, movie.ID, aDraft("Bob's Film Now")); !errors.Is(err, errNotYours) {
		t.Errorf("edit by a non-owner: got %v, want errNotYours", err)
	}
	if _, err := s.withdrawSubmission(ctx, bobsView, movie.ID); !errors.Is(err, errNotYours) {
		t.Errorf("withdraw by a non-owner: got %v, want errNotYours", err)
	}

	// And nothing moved.
	q := store.New(pool)
	after, err := q.GetMovie(ctx, movie.ID)
	if err != nil {
		t.Fatalf("reload movie: %v", err)
	}
	if after.Title != "Alice's Film" || after.Hidden {
		t.Errorf("the film changed under a refused request: title=%q hidden=%v", after.Title, after.Hidden)
	}
}

// TestEditAndWithdrawRefuseAFilmFromAnotherSeason: movie.id is unique across
// every year, so being the submitter is not enough. Last year's film is the
// archive.
func TestEditAndWithdrawRefuseAFilmFromAnotherSeason(t *testing.T) {
	pool := newTestPool(t)
	last := newFixture(t, pool, "submitting", 2)
	this := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := last.person(t, "alice")
	last.member(t, alice, nil)
	this.member(t, alice, nil)

	old, err := s.createSubmission(ctx, last.seasonID, alice, aDraft("Last Year"))
	if err != nil {
		t.Fatalf("submission: %v", err)
	}

	// The viewer is in THIS season, reaching for a film in the other one.
	v := this.viewerFor(t, alice)

	if _, err := s.updateSubmission(ctx, v, old.ID, aDraft("Rewritten")); !errors.Is(err, errWrongSeason) {
		t.Errorf("edit across seasons: got %v, want errWrongSeason", err)
	}
	if _, err := s.withdrawSubmission(ctx, v, old.ID); !errors.Is(err, errWrongSeason) {
		t.Errorf("withdraw across seasons: got %v, want errWrongSeason", err)
	}
}

// TestEditAndWithdrawRefuseOutsideTheWindow is rule 2: the window gates the
// WRITE. Every state but 'submitting' refuses, including 'draft' -- a season
// that has not opened has nothing on it to change.
func TestEditAndWithdrawRefuseOutsideTheWindow(t *testing.T) {
	for _, state := range []string{"draft", "voting", "locked"} {
		t.Run(state, func(t *testing.T) {
			pool := newTestPool(t)
			f := newFixture(t, pool, "submitting", 2)
			s := newDBService(pool)
			ctx := context.Background()

			alice := f.person(t, "alice")
			f.member(t, alice, nil)

			movie, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Put Up In Time"))
			if err != nil {
				t.Fatalf("submission: %v", err)
			}

			// Move the season on AFTER the film is up, which is the real
			// sequence: the window closes under somebody with the form open.
			if _, err := pool.Exec(ctx,
				`UPDATE season SET state = $2, locked_at = CASE WHEN $2 = 'locked' THEN now() ELSE NULL END WHERE id = $1`,
				f.seasonID, state,
			); err != nil {
				t.Fatalf("move the season to %s: %v", state, err)
			}

			v := f.viewerFor(t, alice)

			if _, err := s.updateSubmission(ctx, v, movie.ID, aDraft("Too Late")); !errors.Is(err, errWindowClosed) {
				t.Errorf("edit in %s: got %v, want errWindowClosed", state, err)
			}
			if _, err := s.withdrawSubmission(ctx, v, movie.ID); !errors.Is(err, errWindowClosed) {
				t.Errorf("withdraw in %s: got %v, want errWindowClosed", state, err)
			}
			if f.hidden(t, movie.ID) {
				t.Error("a refused withdrawal hid the film anyway")
			}
		})
	}
}

// TestEditAndWithdrawRefuseANonMember is the check the schema does not have:
// movie references season and person, never season_member.
func TestEditAndWithdrawRefuseANonMember(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	movie, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Hers While She Was In"))
	if err != nil {
		t.Fatalf("submission: %v", err)
	}

	v := f.viewerFor(t, alice)

	// Removed from the whitelist mid-season. Her film stays on the slate --
	// see DeleteSeasonMember -- but she can no longer act on it.
	if _, err := pool.Exec(ctx,
		`DELETE FROM season_member WHERE season_id = $1 AND person_id = $2`, f.seasonID, alice,
	); err != nil {
		t.Fatalf("remove membership: %v", err)
	}

	if _, err := s.updateSubmission(ctx, v, movie.ID, aDraft("Still Mine?")); !errors.Is(err, errNotMember) {
		t.Errorf("edit after removal: got %v, want errNotMember", err)
	}
	if _, err := s.withdrawSubmission(ctx, v, movie.ID); !errors.Is(err, errNotMember) {
		t.Errorf("withdraw after removal: got %v, want errNotMember", err)
	}
}

// TestEditAndWithdrawRefuseAMemberBarredFromSubmitting: an explicit
// submit_limit of 0 is "may vote, may not submit", and somebody who may not
// put a film up has no business rewriting one that is already there.
func TestEditAndWithdrawRefuseAMemberBarredFromSubmitting(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	movie, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Before The Override"))
	if err != nil {
		t.Fatalf("submission: %v", err)
	}

	v := f.viewerFor(t, alice)

	if _, err := pool.Exec(ctx,
		`UPDATE season_member SET submit_limit = 0 WHERE season_id = $1 AND person_id = $2`,
		f.seasonID, alice,
	); err != nil {
		t.Fatalf("set the override: %v", err)
	}

	if _, err := s.updateSubmission(ctx, v, movie.ID, aDraft("Rewritten")); !errors.Is(err, errBarred) {
		t.Errorf("edit while barred: got %v, want errBarred", err)
	}
	if _, err := s.withdrawSubmission(ctx, v, movie.ID); !errors.Is(err, errBarred) {
		t.Errorf("withdraw while barred: got %v, want errBarred", err)
	}
}

// TestEditWritesEveryColumnAndAnAuditRow is rule 6, plus the columns.
//
// tmdb_id is asserted to SURVIVE the edit. The form does not carry it -- there
// is no TMDB lookup yet -- and UpdateMovie sets it unconditionally, so a
// handler that passed nil would silently drop it and, worse, free up the
// movie_season_tmdb_unique_idx slot that stops two people submitting the same
// film.
func TestEditWritesEveryColumnAndAnAuditRow(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)

	movie, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Before"))
	if err != nil {
		t.Fatalf("submission: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE movie SET tmdb_id = 12345 WHERE id = $1`, movie.ID); err != nil {
		t.Fatalf("set tmdb_id: %v", err)
	}

	year := int32(1982)
	trailer := "https://example.test/after"
	updated, err := s.updateSubmission(ctx, f.viewerFor(t, alice), movie.ID, draft{
		title:       "After",
		year:        &year,
		trailerURL:  &trailer,
		description: "A different sentence entirely.",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	switch {
	case updated.Title != "After":
		t.Errorf("title = %q, want After", updated.Title)
	case updated.Year == nil || *updated.Year != 1982:
		t.Errorf("year = %v, want 1982", updated.Year)
	case updated.TrailerUrl == nil || *updated.TrailerUrl != trailer:
		t.Errorf("trailer = %v, want %q", updated.TrailerUrl, trailer)
	case updated.Description != "A different sentence entirely.":
		t.Errorf("description = %q", updated.Description)
	case updated.TmdbID == nil || *updated.TmdbID != 12345:
		t.Errorf("tmdb_id = %v, want it preserved as 12345", updated.TmdbID)
	}

	if got := f.auditRows(t, "movie.edited", movie.ID); got != 1 {
		t.Errorf("movie.edited audit rows = %d, want 1", got)
	}
}

// TestWithdrawWritesAnAuditRowAndIsIdempotent: two taps on a slow connection
// is the ordinary way a second withdrawal happens, and an error page for "the
// thing you asked for is already true" helps nobody.
func TestWithdrawWritesAnAuditRowAndIsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)
	v := f.viewerFor(t, alice)

	movie, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Gone"))
	if err != nil {
		t.Fatalf("submission: %v", err)
	}

	if _, err := s.withdrawSubmission(ctx, v, movie.ID); err != nil {
		t.Fatalf("first withdraw: %v", err)
	}
	if _, err := s.withdrawSubmission(ctx, v, movie.ID); err != nil {
		t.Fatalf("second withdraw should be a no-op, got: %v", err)
	}

	if got := f.auditRows(t, "movie.withdrawn", movie.ID); got != 1 {
		t.Errorf("movie.withdrawn audit rows = %d, want 1 — the second call changed nothing and should log nothing", got)
	}
}

// TestEditRefusesAWithdrawnFilm: a hidden film is not on the board, so there
// is nothing there to save.
func TestEditRefusesAWithdrawnFilm(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)
	v := f.viewerFor(t, alice)

	movie, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Withdrawn Already"))
	if err != nil {
		t.Fatalf("submission: %v", err)
	}
	if _, err := s.withdrawSubmission(ctx, v, movie.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	if _, err := s.updateSubmission(ctx, v, movie.ID, aDraft("Back From The Dead")); !errors.Is(err, errAlreadyWithdrawn) {
		t.Errorf("edit of a withdrawn film: got %v, want errAlreadyWithdrawn", err)
	}
}

// TestEditRefusesAFilmThatIsNotThere covers the id that parses and names
// nothing.
func TestEditRefusesAFilmThatIsNotThere(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)
	v := f.viewerFor(t, alice)

	if _, err := s.withdrawSubmission(ctx, v, uuid.New()); !errors.Is(err, errNotFound) {
		t.Errorf("withdraw of a film that does not exist: got %v, want errNotFound", err)
	}
}

// TestUnWithdrawingWouldCollideOnceTMDBIdsAreReal is a hazard note with a
// failing case behind it, aimed at whoever builds un-withdraw.
//
// There is no un-withdraw today: SetMovieHidden is called in exactly one place
// in this package, with Hidden: true. This test exists because the obvious
// implementation of the missing half -- flip the flag back -- is wrong, and
// wrong in a way that only appeared when builder-14's nap-eie started writing
// real tmdb_ids.
//
// movie_season_tmdb_unique_idx is UNIQUE (season_id, tmdb_id) WHERE tmdb_id IS
// NOT NULL AND NOT hidden. Withdrawing a film therefore FREES its id for
// somebody else, which is deliberate and is what migration 00004's own comment
// promises. The consequence nobody wrote down is the other direction: once
// somebody else has taken the id, the original cannot come back, and a bare
// UPDATE ... SET hidden = false is a 23505 rather than a no-op.
//
// So un-withdraw has to re-check the slot and refuse with alreadyUpMessage,
// the same sentence the submit path uses. It is not a flag flip.
func TestUnWithdrawingWouldCollideOnceTMDBIdsAreReal(t *testing.T) {
	pool := newTestPool(t)
	f := newFixture(t, pool, "submitting", 2)
	s := newDBService(pool)
	ctx := context.Background()

	alice := f.person(t, "alice")
	f.member(t, alice, nil)
	bob := f.person(t, "bob")
	f.member(t, bob, nil)

	const tmdbID = 1858 // Transformers, chosen for its unarguable awfulness.

	hers, err := s.createSubmission(ctx, f.seasonID, alice, aDraft("Hers First"))
	if err != nil {
		t.Fatalf("submission: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE movie SET tmdb_id = $2 WHERE id = $1`, hers.ID, tmdbID); err != nil {
		t.Fatalf("set tmdb_id: %v", err)
	}

	// She withdraws, which frees the id. This is the supported operation and
	// it must keep working.
	if _, err := s.withdrawSubmission(ctx, f.viewerFor(t, alice), hers.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	his, err := s.createSubmission(ctx, f.seasonID, bob, aDraft("His Now"))
	if err != nil {
		t.Fatalf("second submission: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE movie SET tmdb_id = $2 WHERE id = $1`, his.ID, tmdbID); err != nil {
		t.Fatalf("the withdrawal did not free the tmdb id: %v", err)
	}

	// And now the trap. No handler does this today; the point is what happens
	// when one does.
	_, err = pool.Exec(ctx, `UPDATE movie SET hidden = false WHERE id = $1`, hers.ID)
	if err == nil {
		t.Fatal("un-hiding a film whose tmdb id somebody else has taken succeeded. " +
			"Either movie_season_tmdb_unique_idx has changed or this hazard is gone; " +
			"check before deleting this test, because un-withdraw depends on it")
	}
	if !isAlreadyUp(err) {
		t.Fatalf("un-hiding failed with something other than a unique violation: %v", err)
	}

	// isAlreadyUp is what the edit path maps to errAlreadyUp, so un-withdraw
	// can reuse it and say the same sentence rather than inventing a second.
	t.Logf("un-withdraw must map this to alreadyUpMessage: %v", err)
}
