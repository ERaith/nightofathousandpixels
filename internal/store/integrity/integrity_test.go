package integrity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// lockedSeasonErrCode is the SQLSTATE raised by
// nap_reject_locked_season_delete (00008_integrity_guards.sql). It is a
// project-defined code rather than a standard one so that "you tried to edit
// the archive" is distinguishable from every other integrity error.
const lockedSeasonErrCode = "NAPLK"

const (
	lockedSeason = "aaaaaaaa-0000-0000-0000-000000000001" // 2025, state = locked
	liveSeason   = "aaaaaaaa-0000-0000-0000-000000000002" // 2026, state = voting
	bob          = "22222222-2222-2222-2222-222222222222"
	carol        = "33333333-3333-3333-3333-333333333333" // voter only, never submitted
	lockedWinner = "bbbbbbbb-0000-0000-0000-000000000001" // ranked, in the locked season
	lockedSpare  = "bbbbbbbb-0000-0000-0000-000000000009" // unranked, in the locked season
	liveRanked   = "cccccccc-0000-0000-0000-000000000001"
	liveUnranked = "cccccccc-0000-0000-0000-000000000009"
)

// seedSQL builds the smallest fixture that exercises both cascade paths out of
// season: a locked past year with a result, and a live year, sharing voters.
// Carol only ever votes, so movie.submitted_by's RESTRICT does not
// accidentally stand in for the guard we are actually testing.
const seedSQL = `
INSERT INTO person (id, email, email_normalized, display_name) VALUES
  ('11111111-1111-1111-1111-111111111111','alice@example.test','alice@example.test','Alice'),
  ('22222222-2222-2222-2222-222222222222','bob@example.test','bob@example.test','Bob'),
  ('33333333-3333-3333-3333-333333333333','carol@example.test','carol@example.test','Carol');

INSERT INTO season (id, year, name, state, locked_at) VALUES
  ('aaaaaaaa-0000-0000-0000-000000000001', 2025, 'NOATP 2025', 'locked', now()),
  ('aaaaaaaa-0000-0000-0000-000000000002', 2026, 'NOATP 2026', 'voting', NULL);

INSERT INTO season_member (season_id, person_id, is_admin) VALUES
  ('aaaaaaaa-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111', true),
  ('aaaaaaaa-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222', false),
  ('aaaaaaaa-0000-0000-0000-000000000001','33333333-3333-3333-3333-333333333333', false),
  ('aaaaaaaa-0000-0000-0000-000000000002','11111111-1111-1111-1111-111111111111', true),
  ('aaaaaaaa-0000-0000-0000-000000000002','22222222-2222-2222-2222-222222222222', false),
  ('aaaaaaaa-0000-0000-0000-000000000002','33333333-3333-3333-3333-333333333333', false);

INSERT INTO movie (id, season_id, submitted_by, title, year) VALUES
  ('bbbbbbbb-0000-0000-0000-000000000001','aaaaaaaa-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111','The Thing',1982),
  ('bbbbbbbb-0000-0000-0000-000000000002','aaaaaaaa-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222','Suspiria',1977),
  ('bbbbbbbb-0000-0000-0000-000000000009','aaaaaaaa-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222','Locked Unranked',1968),
  ('cccccccc-0000-0000-0000-000000000001','aaaaaaaa-0000-0000-0000-000000000002','11111111-1111-1111-1111-111111111111','Hereditary',2018),
  ('cccccccc-0000-0000-0000-000000000002','aaaaaaaa-0000-0000-0000-000000000002','22222222-2222-2222-2222-222222222222','Possession',1981),
  ('cccccccc-0000-0000-0000-000000000009','aaaaaaaa-0000-0000-0000-000000000002','22222222-2222-2222-2222-222222222222','Live Unranked',1999);

INSERT INTO ballot_entry (season_id, person_id, movie_id, rank) VALUES
  ('aaaaaaaa-0000-0000-0000-000000000001','11111111-1111-1111-1111-111111111111','bbbbbbbb-0000-0000-0000-000000000001',1),
  ('aaaaaaaa-0000-0000-0000-000000000001','22222222-2222-2222-2222-222222222222','bbbbbbbb-0000-0000-0000-000000000002',1),
  ('aaaaaaaa-0000-0000-0000-000000000001','33333333-3333-3333-3333-333333333333','bbbbbbbb-0000-0000-0000-000000000001',1),
  ('aaaaaaaa-0000-0000-0000-000000000002','11111111-1111-1111-1111-111111111111','cccccccc-0000-0000-0000-000000000001',1),
  ('aaaaaaaa-0000-0000-0000-000000000002','22222222-2222-2222-2222-222222222222','cccccccc-0000-0000-0000-000000000002',1),
  ('aaaaaaaa-0000-0000-0000-000000000002','33333333-3333-3333-3333-333333333333','cccccccc-0000-0000-0000-000000000001',1);

INSERT INTO result (season_id, winner_movie_id, rounds) VALUES
  ('aaaaaaaa-0000-0000-0000-000000000001','bbbbbbbb-0000-0000-0000-000000000001',
   '[{"round":1,"counts":{"The Thing":2,"Suspiria":1}}]'::jsonb);

INSERT INTO audit_log (actor, action, target) VALUES ('system','season.lock','2025');
`

// testDSN is the migrated throwaway database from `make test-db-up`. Without
// it there is nothing to assert against, so the whole file skips and plain
// `go test ./...` still passes with no Docker.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping live database test")
	}
	return dsn
}

// openTx opens a connection and an always-rolled-back transaction, so every
// test -- including the ones that delete whole seasons -- leaves the database
// exactly as it found it.
func openTx(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return ctx, tx
}

// seed loads the fixture. It is separate from openTx because inserting ballots
// queues deferred foreign key checks, and Postgres refuses to ALTER a table
// that has pending trigger events -- so the adverse-order tests have to
// rebuild the constraints before any rows exist.
func seed(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	if _, err := tx.Exec(ctx, seedSQL); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// seeded is openTx plus seed, for the tests that do not touch constraints.
func seeded(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	ctx, tx := openTx(t)
	seed(t, ctx, tx)
	return ctx, tx
}

// sqlstate pulls the SQLSTATE out of an error, or "" if it is not a Postgres
// error at all.
func sqlstate(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// countInt runs a scalar count query.
func countInt(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", sql, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// nap-jri: DELETE FROM season must not depend on constraint creation order.
// ---------------------------------------------------------------------------

// TestMovieForeignKeysAreDeferred is the root property. Once these two
// constraints are DEFERRABLE INITIALLY DEFERRED the check happens at COMMIT,
// after every cascade has run, so the order Postgres walks the overlapping
// paths out of season cannot matter. Everything below is a consequence of
// this, which is why it is asserted directly from the catalog: a behavioural
// test could pass by luck of ordering, this one cannot.
//
// It reads pg_trigger, not pg_constraint.condeferred, and that distinction is
// the whole point of the test. Postgres accepts
// `ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED`, stores condeferred =
// true for it, and then builds the delete trigger non-deferrable anyway. A
// test asserting pg_constraint would pass against that silently downgraded
// constraint; this one fails, which is what stops someone "tidying" NO ACTION
// into RESTRICT and quietly reintroducing nap-jri.
func TestMovieForeignKeysAreDeferred(t *testing.T) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	for _, name := range []string{"ballot_entry_movie_fkey", "result_winner_movie_fkey"} {
		rows, err := conn.Query(ctx, `
			SELECT t.tgname, t.tgrelid::regclass::text, t.tgdeferrable, t.tginitdeferred
			  FROM pg_constraint c
			  JOIN pg_trigger t ON t.tgconstraint = c.oid
			 WHERE c.conname = $1
			 ORDER BY t.tgname`, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var n int
		for rows.Next() {
			var tg, tbl string
			var deferrable, deferred bool
			if err := rows.Scan(&tg, &tbl, &deferrable, &deferred); err != nil {
				t.Fatalf("scan: %v", err)
			}
			n++
			if !deferrable || !deferred {
				t.Errorf("%s: trigger %s on %s is deferrable=%v initdeferred=%v, want both true -- "+
					"the deferral was silently downgraded and DELETE FROM season "+
					"depends on constraint OID order again",
					name, tg, tbl, deferrable, deferred)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if n == 0 {
			t.Errorf("%s: no referential integrity triggers found", name)
		}
	}
}

// TestRestrictDeferrableIsSilentlyDowngraded documents the trap the test above
// exists to catch, by building it on the live server rather than trusting a
// comment. If a future Postgres ever stops downgrading RESTRICT, this fails
// and the warning in 00008 can be relaxed.
func TestRestrictDeferrableIsSilentlyDowngraded(t *testing.T) {
	ctx, tx := openTx(t)

	if _, err := tx.Exec(ctx, `
		CREATE TABLE trap_parent (id int PRIMARY KEY);
		CREATE TABLE trap_no_action (p_id int, CONSTRAINT trap_na_fk FOREIGN KEY (p_id)
			REFERENCES trap_parent (id) ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED);
		CREATE TABLE trap_restrict (p_id int, CONSTRAINT trap_r_fk FOREIGN KEY (p_id)
			REFERENCES trap_parent (id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED);
	`); err != nil {
		t.Fatalf("build the trap: %v", err)
	}

	// Both constraints claim to be deferred.
	for _, name := range []string{"trap_na_fk", "trap_r_fk"} {
		var deferred bool
		if err := tx.QueryRow(ctx,
			`SELECT condeferred FROM pg_constraint WHERE conname = $1`, name).Scan(&deferred); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !deferred {
			t.Errorf("%s: pg_constraint.condeferred = false, expected the catalog to claim a deferral", name)
		}
	}

	// Only one of them actually is. The delete-side triggers live on the
	// referenced table, which is where the downgrade shows up.
	deferredTriggers := func(constraint string) (deferred, total int) {
		t.Helper()
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE t.tgdeferrable AND t.tginitdeferred), count(*)
			  FROM pg_constraint c
			  JOIN pg_trigger t ON t.tgconstraint = c.oid
			 WHERE c.conname = $1 AND t.tgrelid = 'trap_parent'::regclass`,
			constraint).Scan(&deferred, &total); err != nil {
			t.Fatalf("%s: %v", constraint, err)
		}
		return deferred, total
	}

	if d, total := deferredTriggers("trap_na_fk"); d != total {
		t.Errorf("NO ACTION: %d of %d triggers deferred, want all of them", d, total)
	}
	if d, total := deferredTriggers("trap_r_fk"); d == total {
		t.Errorf("RESTRICT: all %d triggers deferred -- Postgres no longer downgrades it, "+
			"so the warning in 00008_integrity_guards.sql is now out of date", total)
	}
}

// adverseOrderSQL rebuilds every foreign key in the order pg_dump emits them,
// which is table-name order. That is the order a restored database has, and it
// is the opposite of the order the migrations produce: it puts
// ballot_entry_movie_fkey ahead of season_member_season_id_fkey, so the
// season -> movie path is walked before season -> season_member ->
// ballot_entry.
//
// It re-adds each constraint from pg_get_constraintdef rather than from a
// definition written out here, for two reasons. It cannot drift from the
// migrations. And it cannot accidentally apply the fix: hardcoding the
// DEFERRABLE form would make this test pass against a schema that does not
// have it, which is exactly the false negative nap-jri was hiding behind.
//
// This is a stand-in for a restore, not a substitute for one --
// TestDumpRestoreRoundTrip below does the real thing when pg_dump is on PATH.
// It earns its place by running everywhere and by performing the exact
// transformation a restore performs on the constraint set.
const adverseOrderSQL = `
DO $$
DECLARE
    drops text[] := '{}';
    adds  text[] := '{}';
    r     record;
    s     text;
BEGIN
    FOR r IN
        SELECT conrelid::regclass::text AS tbl,
               conname,
               pg_get_constraintdef(oid) AS def
          FROM pg_constraint
         WHERE contype = 'f' AND connamespace = 'public'::regnamespace
         ORDER BY conrelid::regclass::text, conname
    LOOP
        drops := drops || format('ALTER TABLE %s DROP CONSTRAINT %I', r.tbl, r.conname);
        adds  := adds  || format('ALTER TABLE %s ADD CONSTRAINT %I %s', r.tbl, r.conname, r.def);
    END LOOP;
    FOREACH s IN ARRAY drops LOOP EXECUTE s; END LOOP;
    FOREACH s IN ARRAY adds  LOOP EXECUTE s; END LOOP;
END $$;
`

// assertAdverseOrder fails if the rebuild did not actually produce the order
// that breaks things, so this cannot quietly become a test of nothing.
func assertAdverseOrder(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	var first string
	if err := tx.QueryRow(ctx,
		`SELECT conname FROM pg_constraint
		  WHERE contype = 'f' AND connamespace = 'public'::regnamespace
		  ORDER BY oid LIMIT 1`).Scan(&first); err != nil {
		t.Fatalf("read constraint order: %v", err)
	}
	if first != "ballot_entry_movie_fkey" {
		t.Fatalf("first foreign key by OID is %q, want ballot_entry_movie_fkey: "+
			"the rebuild did not produce the adverse order", first)
	}
}

// TestDeleteSeasonUnderBothConstraintOrders deletes every season under the
// order the migrations produce and under the order a restore produces. Before
// 00008 the second one failed with "violates foreign key constraint
// ballot_entry_movie_fkey", which is the whole of nap-jri.
func TestDeleteSeasonUnderBothConstraintOrders(t *testing.T) {
	for _, order := range []string{"migrated", "restored"} {
		t.Run(order, func(t *testing.T) {
			ctx, tx := openTx(t)
			if order == "restored" {
				if _, err := tx.Exec(ctx, adverseOrderSQL); err != nil {
					t.Fatalf("rebuild constraints in restore order: %v", err)
				}
				assertAdverseOrder(t, ctx, tx)
			}
			seed(t, ctx, tx)
			if _, err := tx.Exec(ctx, `DELETE FROM season`); err != nil {
				t.Fatalf("DELETE FROM season: %v", err)
			}
			// Force the deferred checks now rather than at a COMMIT this
			// transaction will never reach.
			if _, err := tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
				t.Fatalf("deferred checks failed at end of transaction: %v", err)
			}
			if n := countInt(t, ctx, tx, `SELECT count(*) FROM ballot_entry`); n != 0 {
				t.Errorf("ballot_entry left %d rows, want 0", n)
			}
		})
	}
}

// TestRankedMovieDeleteStillRejected is the property that actually matters:
// deferring the constraint must not turn a hard delete of a ranked movie into
// something that silently rewrites ballots. Soft delete stays the only way.
func TestRankedMovieDeleteStillRejected(t *testing.T) {
	ctx, tx := seeded(t)

	if _, err := tx.Exec(ctx, `DELETE FROM movie WHERE id = $1`, liveRanked); err != nil {
		t.Fatalf("the statement itself should succeed; the deferred check is what rejects it: %v", err)
	}
	_, err := tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	if err == nil {
		t.Fatal("deleting a ranked movie was accepted; ballots would have been rewritten")
	}
	if got := sqlstate(err); got != "23503" {
		t.Errorf("SQLSTATE = %q, want 23503 (foreign_key_violation): %v", got, err)
	}
}

// TestUnrankedMovieDeleteStillAllowed pins the other side of it: a movie
// nobody ranked is still removable from a live season.
func TestUnrankedMovieDeleteStillAllowed(t *testing.T) {
	ctx, tx := seeded(t)

	tag, err := tx.Exec(ctx, `DELETE FROM movie WHERE id = $1`, liveUnranked)
	if err != nil {
		t.Fatalf("DELETE unranked movie: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("deleted %d rows, want 1", tag.RowsAffected())
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		t.Fatalf("deferred checks rejected an unranked movie delete: %v", err)
	}
}

// TestSoftDeleteStillWorks: the remedy the schema actually wants.
func TestSoftDeleteStillWorks(t *testing.T) {
	ctx, tx := seeded(t)
	if _, err := tx.Exec(ctx, `UPDATE movie SET hidden = true WHERE id = $1`, liveRanked); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
}

// ---------------------------------------------------------------------------
// nap-u3r: a locked season is the archive and cannot be edited piecemeal.
// ---------------------------------------------------------------------------

// TestLockedSeasonRejectsPiecemealDeletes covers every route by which a
// routine admin action used to reach into a past year. Each must fail loudly
// with the project's own SQLSTATE rather than silently succeeding.
func TestLockedSeasonRejectsPiecemealDeletes(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		args []any
	}{
		{
			// The reported path: Carol only voted, so movie.submitted_by's
			// RESTRICT never fires and person -> season_member ->
			// ballot_entry used to cascade straight through the archive.
			"delete a person who voted in the locked season",
			`DELETE FROM person WHERE id = $1`, []any{carol},
		},
		{
			// 00001 calls this the routine way someone leaves the group.
			"remove a member of the locked season",
			`DELETE FROM season_member WHERE season_id = $1 AND person_id = $2`,
			[]any{lockedSeason, bob},
		},
		{
			"delete the locked season's ballots directly",
			`DELETE FROM ballot_entry WHERE season_id = $1`, []any{lockedSeason},
		},
		{
			// Nothing references an unranked movie, so no foreign key stood
			// between this and the archived slate.
			"delete an unranked movie from the locked season",
			`DELETE FROM movie WHERE id = $1`, []any{lockedSpare},
		},
		{
			"delete the locked season's frozen result snapshot",
			`DELETE FROM result WHERE season_id = $1`, []any{lockedSeason},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, tx := seeded(t)
			_, err := tx.Exec(ctx, tc.sql, tc.args...)
			if err == nil {
				t.Fatal("accepted; the archive was edited silently")
			}
			if got := sqlstate(err); got != lockedSeasonErrCode {
				t.Errorf("SQLSTATE = %q, want %q: %v", got, lockedSeasonErrCode, err)
			}
		})
	}
}

// TestRejectedDeleteIsAtomic: Carol is in both years. Her locked-season
// ballots stopping the delete must not leave her live-season ballots
// half-removed.
func TestRejectedDeleteIsAtomic(t *testing.T) {
	ctx, tx := seeded(t)

	before := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE person_id = $1`, carol)
	if before != 2 {
		t.Fatalf("fixture wrong: Carol has %d ballots, want 2 (one per season)", before)
	}

	// A savepoint, so the rejected statement rolls back to here rather than
	// aborting the whole transaction and taking the evidence with it.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	if _, err := sp.Exec(ctx, `DELETE FROM person WHERE id = $1`, carol); err == nil {
		t.Fatal("expected the locked-season guard to reject this")
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback to savepoint: %v", err)
	}

	if after := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE person_id = $1`, carol); after != before {
		t.Errorf("Carol has %d ballots after the rejected delete, want %d", after, before)
	}
	if live := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE person_id = $1 AND season_id = $2`,
		carol, liveSeason); live != 1 {
		t.Errorf("Carol's live-season ballot count = %d, want 1: the delete was not atomic", live)
	}
}

// TestNonLockedSeasonDeletesStillWork is the regression the guard could most
// easily cause: the live season has to keep behaving exactly as it did.
func TestNonLockedSeasonDeletesStillWork(t *testing.T) {
	ctx, tx := seeded(t)

	liveBefore := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE season_id = $1`, liveSeason)
	lockedBefore := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE season_id = $1`, lockedSeason)

	if _, err := tx.Exec(ctx,
		`DELETE FROM season_member WHERE season_id = $1 AND person_id = $2`,
		liveSeason, carol); err != nil {
		t.Fatalf("removing a member of the live season: %v", err)
	}

	if got := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE season_id = $1`, liveSeason); got != liveBefore-1 {
		t.Errorf("live ballots = %d, want %d: the cascade stopped doing its job", got, liveBefore-1)
	}
	if got := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE season_id = $1`, lockedSeason); got != lockedBefore {
		t.Errorf("locked ballots = %d, want %d", got, lockedBefore)
	}
}

// TestDeleteLockedSeasonWholesaleStillWorks is the hard case the ticket calls
// out: the guard must not block deliberate removal of a whole year, which runs
// down the very cascade the guard watches.
func TestDeleteLockedSeasonWholesaleStillWorks(t *testing.T) {
	ctx, tx := seeded(t)

	tag, err := tx.Exec(ctx, `DELETE FROM season WHERE id = $1`, lockedSeason)
	if err != nil {
		t.Fatalf("DELETE FROM season for a locked season: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("deleted %d seasons, want 1", tag.RowsAffected())
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		t.Fatalf("deferred checks failed: %v", err)
	}
	if n := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE season_id = $1`, lockedSeason); n != 0 {
		t.Errorf("%d ballots survived the season delete", n)
	}
	// The live year is untouched.
	if n := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE season_id = $1`, liveSeason); n != 3 {
		t.Errorf("live ballots = %d, want 3", n)
	}
}

// TestDeleteSeasonUnderAdverseOrderWithGuards is the combined case. Both fixes
// touch the same cascade, so the interesting question is not whether either
// works but whether they work together, in the adverse order, against a locked
// season -- which is every one of the two tickets at once.
func TestDeleteSeasonUnderAdverseOrderWithGuards(t *testing.T) {
	ctx, tx := openTx(t)
	if _, err := tx.Exec(ctx, adverseOrderSQL); err != nil {
		t.Fatalf("rebuild constraints in restore order: %v", err)
	}
	assertAdverseOrder(t, ctx, tx)
	seed(t, ctx, tx)
	if _, err := tx.Exec(ctx, `DELETE FROM season`); err != nil {
		t.Fatalf("DELETE FROM season: %v", err)
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		t.Fatalf("deferred checks failed: %v", err)
	}
	for _, tbl := range []string{"season", "season_member", "movie", "ballot_entry", "result"} {
		if n := countInt(t, ctx, tx, `SELECT count(*) FROM `+tbl); n != 0 {
			t.Errorf("%s left %d rows", tbl, n)
		}
	}
}

// ---------------------------------------------------------------------------
// Unlocking must not be the way around the guard.
// ---------------------------------------------------------------------------

// TestLockedSeasonCannotBeUnlocked: once `locked` is what protects the data,
// moving a season out of it has to be as hard as editing the data directly.
func TestLockedSeasonCannotBeUnlocked(t *testing.T) {
	for _, state := range []string{"draft", "submitting", "voting"} {
		t.Run("to "+state, func(t *testing.T) {
			ctx, tx := seeded(t)
			_, err := tx.Exec(ctx,
				`UPDATE season SET state = $1, locked_at = NULL WHERE id = $2`,
				state, lockedSeason)
			if err == nil {
				t.Fatal("a locked season was unlocked by a plain UPDATE")
			}
			if got := sqlstate(err); got != lockedSeasonErrCode {
				t.Errorf("SQLSTATE = %q, want %q: %v", got, lockedSeasonErrCode, err)
			}
		})
	}
}

// TestUnlockBypassIsClosed runs the actual attack rather than its first step:
// unlock the season, remove the member whose ballots you want gone, lock it
// again. Before the unlock guard every statement here succeeded and the
// locked-season delete guard never fired once.
func TestUnlockBypassIsClosed(t *testing.T) {
	ctx, tx := seeded(t)

	before := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE season_id = $1`, lockedSeason)

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	if _, err := sp.Exec(ctx,
		`UPDATE season SET state = 'voting', locked_at = NULL WHERE id = $1`,
		lockedSeason); err == nil {
		t.Fatal("step 1 of the bypass (unlock) succeeded")
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback to savepoint: %v", err)
	}

	if after := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE season_id = $1`, lockedSeason); after != before {
		t.Errorf("locked ballots = %d, want %d", after, before)
	}
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM season WHERE id = $1`, lockedSeason).Scan(&state); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != "locked" {
		t.Errorf("season state = %q, want locked", state)
	}
}

// TestDeliberateUnlockIsPossible: the guard is a lock, not a weld. A season
// locked by mistake has to be recoverable without shipping a migration, and
// the escape hatch has to be something no generated query does by accident.
func TestDeliberateUnlockIsPossible(t *testing.T) {
	ctx, tx := seeded(t)

	if _, err := tx.Exec(ctx, `SET LOCAL nap.allow_unlock = 'on'`); err != nil {
		t.Fatalf("set the escape hatch: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE season SET state = 'voting', locked_at = NULL WHERE id = $1`,
		lockedSeason); err != nil {
		t.Fatalf("deliberate unlock: %v", err)
	}

	// And the season is now genuinely editable, which is the point of asking.
	if _, err := tx.Exec(ctx,
		`DELETE FROM season_member WHERE season_id = $1 AND person_id = $2`,
		lockedSeason, bob); err != nil {
		t.Fatalf("editing the unlocked season: %v", err)
	}
}

// TestLockingASeasonStillWorks: the guard refuses to leave 'locked', not to
// reach it.
func TestLockingASeasonStillWorks(t *testing.T) {
	ctx, tx := seeded(t)
	if _, err := tx.Exec(ctx,
		`UPDATE season SET state = 'locked', locked_at = now() WHERE id = $1`,
		liveSeason); err != nil {
		t.Fatalf("locking a season: %v", err)
	}
}

// TestNonLockedSeasonDeleteOnAdverseOrder is the combination the Lead called
// out: both fixes in place, adverse constraint order, and the season being
// deleted is the live one -- so the locked-season guard is live on the other
// year's rows while this cascade runs.
func TestNonLockedSeasonDeleteOnAdverseOrder(t *testing.T) {
	ctx, tx := openTx(t)
	if _, err := tx.Exec(ctx, adverseOrderSQL); err != nil {
		t.Fatalf("rebuild constraints in restore order: %v", err)
	}
	assertAdverseOrder(t, ctx, tx)
	seed(t, ctx, tx)

	tag, err := tx.Exec(ctx, `DELETE FROM season WHERE id = $1`, liveSeason)
	if err != nil {
		t.Fatalf("deleting the non-locked season: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("deleted %d seasons, want 1", tag.RowsAffected())
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		t.Fatalf("deferred checks failed: %v", err)
	}
	if n := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE season_id = $1`, liveSeason); n != 0 {
		t.Errorf("%d live ballots survived", n)
	}
	// The locked year is untouched and still guarded.
	if n := countInt(t, ctx, tx,
		`SELECT count(*) FROM ballot_entry WHERE season_id = $1`, lockedSeason); n != 3 {
		t.Errorf("locked ballots = %d, want 3", n)
	}
}

// ---------------------------------------------------------------------------
// The real thing: dump, restore, delete.
// ---------------------------------------------------------------------------

// TestDumpRestoreRoundTrip is the only test here that catches nap-jri the way
// it was found, because a fresh-migration test cannot: the bug lives in the
// constraint order a restore produces, and nothing else produces it. It needs
// pg_dump and psql on PATH and skips without them, so the in-process
// reconstruction above stays the one that always runs.
func TestDumpRestoreRoundTrip(t *testing.T) {
	dsn := testDSN(t)
	pgDump, err := exec.LookPath("pg_dump")
	if err != nil {
		t.Skip("pg_dump not on PATH; skipping the real restore round trip")
	}
	psql, err := exec.LookPath("psql")
	if err != nil {
		t.Skip("psql not on PATH; skipping the real restore round trip")
	}

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	const restoreDB = "nap_restore_roundtrip"
	adminDSN := replaceDatabase(dsn, "postgres")
	ctx := context.Background()

	// The dump has to contain data, so the fixture is committed and cleaned up
	// afterwards -- and that cleanup is itself a DELETE FROM season, which is
	// the statement under test.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Registered before the row cleanup below so that LIFO closes the
	// connection last; t.Cleanup runs after deferred statements, so a defer
	// here would hand the cleanup a dead connection.
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(ctx, seedSQL); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(),
			`DELETE FROM season; DELETE FROM person; DELETE FROM audit_log`); err != nil {
			t.Errorf("cleanup of the source database failed: %v", err)
		}
	})

	run := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Env = append(os.Environ(), "PGPASSWORD="+cfg.Password)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		return string(out)
	}

	dumpFile := t.TempDir() + "/dump.sql"
	run(pgDump, "--dbname="+dsn, "--file="+dumpFile)

	run(psql, "--dbname="+adminDSN, "-v", "ON_ERROR_STOP=1",
		"-c", fmt.Sprintf("DROP DATABASE IF EXISTS %s", restoreDB))
	run(psql, "--dbname="+adminDSN, "-v", "ON_ERROR_STOP=1",
		"-c", fmt.Sprintf("CREATE DATABASE %s", restoreDB))
	t.Cleanup(func() {
		cmd := exec.Command(psql, "--dbname="+adminDSN,
			"-c", fmt.Sprintf("DROP DATABASE IF EXISTS %s", restoreDB))
		cmd.Env = append(os.Environ(), "PGPASSWORD="+cfg.Password)
		_ = cmd.Run()
	})

	restoredDSN := replaceDatabase(dsn, restoreDB)
	run(psql, "--dbname="+restoredDSN, "-v", "ON_ERROR_STOP=1", "-q", "-f", dumpFile)

	rconn, err := pgx.Connect(ctx, restoredDSN)
	if err != nil {
		t.Fatalf("connect to restored database: %v", err)
	}
	defer func() { _ = rconn.Close(context.Background()) }()

	// Confirm the restore really did produce the adverse order, so that a
	// future pg_dump that happens to preserve OID order does not turn this
	// into a test that proves nothing.
	var first string
	if err := rconn.QueryRow(ctx,
		`SELECT conname FROM pg_constraint WHERE contype = 'f' ORDER BY oid LIMIT 1`).Scan(&first); err != nil {
		t.Fatalf("read constraint order: %v", err)
	}
	if first != "ballot_entry_movie_fkey" {
		t.Logf("note: restored constraint order starts with %q, not the adverse "+
			"ballot_entry_movie_fkey; this run may not be exercising the bug", first)
	}

	tag, err := rconn.Exec(ctx, `DELETE FROM season`)
	if err != nil {
		t.Fatalf("DELETE FROM season on the restored database: %v", err)
	}
	if tag.RowsAffected() != 2 {
		t.Errorf("deleted %d seasons, want 2", tag.RowsAffected())
	}

	// And the guards survived the dump too.
	if _, err := rconn.Exec(ctx, `SELECT 1 FROM pg_trigger WHERE tgname = 'ballot_entry_locked_season_guard'`); err != nil {
		t.Errorf("locked-season guard missing after restore: %v", err)
	}
}

// replaceDatabase swaps the database name in a postgres URL, so the test can
// reach the admin database and the restore target without a second env var.
func replaceDatabase(dsn, db string) string {
	i := strings.LastIndex(dsn, "/")
	if i < 0 {
		return dsn
	}
	rest := ""
	if q := strings.Index(dsn[i:], "?"); q >= 0 {
		rest = dsn[i+q:]
	}
	return dsn[:i+1] + db + rest
}
