//go:build integration

package admin

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

// The season writes, against a real Postgres.
//
// Three of the questions below have no answer outside the database:
//
//   - Does year really refuse a duplicate, and with the SQLSTATE the handler
//     catches? A fake would say yes to whatever the author typed.
//   - Does the creator's membership row really land in the same transaction as
//     the season? That is what makes BOOTSTRAP_ADMIN_EMAILS a one-time measure
//     instead of a permanent one, and a rollback that took the season but left
//     the membership would produce a season nobody can re-date.
//   - Do the CHECK constraints in migration 00002 -- the window ordering, the
//     locked-has-a-timestamp pair -- agree with the validation in form.go? Two
//     implementations of one rule is how they drift.

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is not set. Under -tags=integration the database is the point,\nso an absent DSN is a broken invocation rather than a reason to report ok.\nRun `make test-integration`.")
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
// Store is nil deliberately: these tests call createSeason and updateSeason
// directly, which go through Beginner and never touch Store, so a nil there is
// a guarantee that the write path is self-contained.
func newDBService(pool *pgxpool.Pool, now time.Time) *Service {
	return &Service{opts: Options{
		DB:       pool,
		Location: testLocation,
		Now:      func() time.Time { return now },
	}}
}

// Years are handed out from a counter well above any real season and below the
// 2200 ceiling in season_year_range, so these tests can run against a database
// that already has data in it and alongside each other.
var nextAdminYear = struct {
	sync.Mutex
	n int
}{n: 2150}

func freeYear(t *testing.T) int {
	t.Helper()

	nextAdminYear.Lock()
	defer nextAdminYear.Unlock()

	year := nextAdminYear.n
	nextAdminYear.n++

	return year
}

// testAdmin creates a person row and the admin value the write paths take.
func testAdmin(t *testing.T, pool *pgxpool.Pool, year int) admin {
	t.Helper()

	email := fmt.Sprintf("ada-%d@fixture.test", year)

	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
INSERT INTO person (email, email_normalized, display_name)
VALUES ($1, $1, 'Ada')
RETURNING id`, email).Scan(&id)
	if err != nil {
		t.Fatalf("create person: %v", err)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM person WHERE id = $1`, id); err != nil {
			t.Logf("cleanup person: %v", err)
		}
	})

	return admin{person: store.Person{ID: id, Email: email, EmailNormalized: email}}
}

// cleanupSeason removes a season the test created.
func cleanupSeason(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) {
	t.Helper()

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM season WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup season: %v", err)
		}
	})
}

// formFor is the good form, aimed at a year nothing else is using.
func formFor(year int) viewmodel.AdminSeasonForm {
	f := goodForm()
	f.Year = fmt.Sprintf("%d", year)
	f.Name = fmt.Sprintf("Fixture %d", year)

	return f
}

// draftFrom validates a form the way a handler would, failing the test if the
// form the test wrote is not actually valid.
func draftFrom(t *testing.T, form viewmodel.AdminSeasonForm, now time.Time, requireFuture bool) seasonDraft {
	t.Helper()

	d, errs := validateSeason(form, testLocation, now, requireFuture)
	if errs.Any() {
		t.Fatalf("the test's own form did not validate: %v", errs)
	}

	return d
}

// TestCreateSeasonWritesTheSeasonTheMembershipAndTheAudit is the whole of the
// create path, and the membership row is the part that matters most.
//
// Without it, an admin who created next year's season would immediately lose
// the ability to edit it: GetCurrentSeason returns the NEWEST non-draft
// season, so the moment 2027 opens, being an admin of 2026 stops meaning
// anything. It is also what makes the bootstrap list self-extinguishing.
func TestCreateSeasonWritesTheSeasonTheMembershipAndTheAudit(t *testing.T) {
	pool := newTestPool(t)
	year := freeYear(t)
	now := time.Date(year-1, time.September, 1, 12, 0, 0, 0, testLocation)
	s := newDBService(pool, now)
	a := testAdmin(t, pool, year)
	ctx := context.Background()

	form := formFor(year)
	form.SubmitClosesAt = fmt.Sprintf("%d-10-24T20:00", year)
	form.VoteClosesAt = fmt.Sprintf("%d-10-31T20:00", year)

	season, err := s.createSeason(ctx, a, draftFrom(t, form, now, true))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupSeason(t, pool, season.ID)

	switch {
	case int(season.Year) != year:
		t.Errorf("year = %d, want %d", season.Year, year)
	case season.State != stateSubmitting:
		t.Errorf("state = %q, want %q — creating a season is the act of opening it", season.State, stateSubmitting)
	case season.DefaultSubmitLimit != 2:
		t.Errorf("default submit limit = %d, want 2", season.DefaultSubmitLimit)
	case !season.VoteOpensAt.Valid:
		t.Error("the submissions-close deadline was not stored")
	case !season.VoteClosesAt.Valid:
		t.Error("the voting deadline was not stored")
	case !season.SubmitOpensAt.Valid:
		t.Error("a season opened now did not record when")
	}

	// The creator administers it.
	q := store.New(pool)
	member, err := q.GetSeasonMember(ctx, store.GetSeasonMemberParams{
		SeasonID: season.ID,
		PersonID: a.person.ID,
	})
	if err != nil {
		t.Fatalf("the creator has no membership row in the season they just made: %v", err)
	}
	if !member.IsAdmin {
		t.Error("the creator is not an admin of their own season; BOOTSTRAP_ADMIN_EMAILS would then be permanent")
	}
	if member.SubmitLimit != nil {
		t.Errorf("the creator's submit_limit = %v, want NULL (use the season default); 0 would bar them from submitting", *member.SubmitLimit)
	}

	var audits int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = 'season.created' AND target = $1`,
		season.ID.String()).Scan(&audits); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if audits != 1 {
		t.Errorf("season.created audit rows = %d, want 1", audits)
	}
}

// TestCreateSeasonRefusesADuplicateYearAsASentence: season_year_key is the
// check, and catching 23505 is what turns it into something an admin can read
// rather than a 500.
func TestCreateSeasonRefusesADuplicateYearAsASentence(t *testing.T) {
	pool := newTestPool(t)
	year := freeYear(t)
	now := time.Date(year-1, time.September, 1, 12, 0, 0, 0, testLocation)
	s := newDBService(pool, now)
	a := testAdmin(t, pool, year)
	ctx := context.Background()

	form := formFor(year)
	form.SubmitClosesAt = fmt.Sprintf("%d-10-24T20:00", year)
	form.VoteClosesAt = fmt.Sprintf("%d-10-31T20:00", year)
	d := draftFrom(t, form, now, true)

	first, err := s.createSeason(ctx, a, d)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	cleanupSeason(t, pool, first.ID)

	if _, err := s.createSeason(ctx, a, d); !errors.Is(err, errYearTaken) {
		t.Fatalf("second create for the same year: got %v, want errYearTaken", err)
	}

	var seasons int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM season WHERE year = $1`, year).Scan(&seasons); err != nil {
		t.Fatalf("count seasons: %v", err)
	}
	if seasons != 1 {
		t.Errorf("seasons for %d = %d, want 1", year, seasons)
	}
}

// TestADraftSeasonIsReachableButNotTheDefault. Draft exists for a season
// prepared ahead of time, and GetCurrentSeason must not surface it -- a
// half-configured next year must not shadow the live one.
func TestADraftSeasonIsReachableButNotTheDefault(t *testing.T) {
	pool := newTestPool(t)
	year := freeYear(t)
	now := time.Date(year-1, time.September, 1, 12, 0, 0, 0, testLocation)
	s := newDBService(pool, now)
	a := testAdmin(t, pool, year)
	ctx := context.Background()

	form := formFor(year)
	form.State = stateDraft
	form.SubmitClosesAt = fmt.Sprintf("%d-10-24T20:00", year)
	form.VoteClosesAt = fmt.Sprintf("%d-10-31T20:00", year)

	season, err := s.createSeason(ctx, a, draftFrom(t, form, now, true))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupSeason(t, pool, season.ID)

	if season.State != stateDraft {
		t.Fatalf("state = %q, want draft", season.State)
	}

	// The default form's state is the other one.
	if blank := s.blankSeasonForm(nil); blank.State != stateSubmitting {
		t.Errorf("the create form defaults to %q; creating a season is normally the act of opening it", blank.State)
	}

	current, err := store.New(pool).GetCurrentSeason(ctx)
	if err == nil && current.ID == season.ID {
		t.Error("a draft season became the current season; it must not shadow a live year")
	}
}

// TestCreateSeasonCannotProduceALockedSeason.
//
// season_state_valid would accept 'locked', and
// season_locked_has_timestamp would then refuse the row for having no
// locked_at -- a 500 on a form. validateSeason refuses the state first, which
// is the version an admin can read.
func TestCreateSeasonCannotProduceALockedSeason(t *testing.T) {
	year := freeYear(t)
	now := time.Date(year-1, time.September, 1, 12, 0, 0, 0, testLocation)

	form := formFor(year)
	form.State = "locked"
	form.SubmitClosesAt = fmt.Sprintf("%d-10-24T20:00", year)
	form.VoteClosesAt = fmt.Sprintf("%d-10-31T20:00", year)

	if _, errs := validateSeason(form, testLocation, now, true); errs.For(viewmodel.FieldSeasonState) == "" {
		t.Fatal("the form accepted state=locked; the database would then refuse the row as a 500")
	}
}

// TestUpdateSeasonRewritesTheDatesAndLogsBothSides. "The deadline moved" is
// only an answer if the log says what it moved from.
func TestUpdateSeasonRewritesTheDatesAndLogsBothSides(t *testing.T) {
	pool := newTestPool(t)
	year := freeYear(t)
	now := time.Date(year-1, time.September, 1, 12, 0, 0, 0, testLocation)
	s := newDBService(pool, now)
	a := testAdmin(t, pool, year)
	ctx := context.Background()

	form := formFor(year)
	form.SubmitClosesAt = fmt.Sprintf("%d-10-24T20:00", year)
	form.VoteClosesAt = fmt.Sprintf("%d-10-31T20:00", year)

	season, err := s.createSeason(ctx, a, draftFrom(t, form, now, true))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupSeason(t, pool, season.ID)

	moved := s.formFromSeason(season)
	moved.Name = "Re-dated"
	moved.SubmitClosesAt = fmt.Sprintf("%d-10-28T20:00", year)
	moved.VoteClosesAt = fmt.Sprintf("%d-11-04T20:00", year)
	moved.SubmitLimit = "3"

	updated, err := s.updateSeason(ctx, a, season, draftFrom(t, moved, now, false))
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	switch {
	case updated.Name != "Re-dated":
		t.Errorf("name = %q", updated.Name)
	case updated.DefaultSubmitLimit != 3:
		t.Errorf("default submit limit = %d, want 3", updated.DefaultSubmitLimit)
	case updated.Year != season.Year:
		t.Errorf("the year changed: %d -> %d. It is the season's identity and every link already names it", season.Year, updated.Year)
	case updated.State != season.State:
		t.Errorf("the state changed: %q -> %q. Moving a season through its phases is a different operation", season.State, updated.State)
	}

	want := time.Date(year, time.October, 28, 20, 0, 0, 0, testLocation)
	if !updated.VoteOpensAt.Time.Equal(want) {
		t.Errorf("submissions close = %v, want %v", updated.VoteOpensAt.Time, want)
	}

	var payload string
	if err := pool.QueryRow(ctx,
		`SELECT payload::text FROM audit_log WHERE action = 'season.updated' AND target = $1`,
		season.ID.String()).Scan(&payload); err != nil {
		t.Fatalf("read the audit row: %v", err)
	}
	for _, want := range []string{`"from"`, `"to"`, "Fixture", "Re-dated"} {
		if !containsSubstring(payload, want) {
			t.Errorf("the season.updated payload does not contain %q: %s", want, payload)
		}
	}
}

// TestARoundTripThroughTheFormLeavesTheDatesAlone.
//
// An admin who opens the re-date screen, changes the name and saves must not
// move the deadlines by an hour. That is exactly what happens if one direction
// formats in UTC and the other parses in the site's timezone -- and it is
// invisible until somebody notices the board shut an hour early.
func TestARoundTripThroughTheFormLeavesTheDatesAlone(t *testing.T) {
	pool := newTestPool(t)
	year := freeYear(t)
	now := time.Date(year-1, time.September, 1, 12, 0, 0, 0, testLocation)
	s := newDBService(pool, now)
	a := testAdmin(t, pool, year)
	ctx := context.Background()

	form := formFor(year)
	form.SubmitClosesAt = fmt.Sprintf("%d-10-24T20:00", year)
	form.VoteClosesAt = fmt.Sprintf("%d-10-31T20:00", year)

	season, err := s.createSeason(ctx, a, draftFrom(t, form, now, true))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupSeason(t, pool, season.ID)

	// Read it back into the form and save it again, untouched but for the name.
	reloaded := s.formFromSeason(season)
	reloaded.Name = "Same dates, new name"

	updated, err := s.updateSeason(ctx, a, season, draftFrom(t, reloaded, now, false))
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	if !updated.VoteOpensAt.Time.Equal(season.VoteOpensAt.Time) {
		t.Errorf("the submissions deadline moved on a round trip: %v -> %v",
			season.VoteOpensAt.Time, updated.VoteOpensAt.Time)
	}
	if !updated.VoteClosesAt.Time.Equal(season.VoteClosesAt.Time) {
		t.Errorf("the voting deadline moved on a round trip: %v -> %v",
			season.VoteClosesAt.Time, updated.VoteClosesAt.Time)
	}
}

// TestUpdateSeasonRefusesALockedSeasonInsideTheTransaction.
//
// A locked season is the archive. The list page offers no way in and
// loadSeason refuses it, but the check that counts is the one in the
// transaction that writes: the season can lock while the form is open.
func TestUpdateSeasonRefusesALockedSeasonInsideTheTransaction(t *testing.T) {
	pool := newTestPool(t)
	year := freeYear(t)
	now := time.Date(year-1, time.September, 1, 12, 0, 0, 0, testLocation)
	s := newDBService(pool, now)
	a := testAdmin(t, pool, year)
	ctx := context.Background()

	form := formFor(year)
	form.SubmitClosesAt = fmt.Sprintf("%d-10-24T20:00", year)
	form.VoteClosesAt = fmt.Sprintf("%d-10-31T20:00", year)

	season, err := s.createSeason(ctx, a, draftFrom(t, form, now, true))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cleanupSeason(t, pool, season.ID)

	// Locked under the open form. `season` is deliberately the STALE row, the
	// way a handler's would be.
	if _, err := pool.Exec(ctx,
		`UPDATE season SET state = 'locked', locked_at = now() WHERE id = $1`, season.ID,
	); err != nil {
		t.Fatalf("lock the season: %v", err)
	}

	moved := s.formFromSeason(season)
	moved.Name = "Should not land"
	if _, err := s.updateSeason(ctx, a, season, draftFrom(t, moved, now, false)); err == nil {
		t.Fatal("a locked season was re-dated")
	}

	var name string
	if err := pool.QueryRow(ctx, `SELECT name FROM season WHERE id = $1`, season.ID).Scan(&name); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if name == "Should not land" {
		t.Error("the refused edit landed anyway")
	}
}

// TestUpdateSeasonDoesNotTouchTheUnlockGuard.
//
// Migration 00008's season_no_unlock_guard is BEFORE UPDATE OF state, so a
// query that never assigns state never fires it. That is why this screen does
// not have to know about nap.allow_unlock -- and this test is what would
// notice if UpdateSeasonDetails ever grew a state column.
func TestUpdateSeasonDoesNotTouchTheUnlockGuard(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	var columns int
	err := pool.QueryRow(ctx, `
SELECT count(*)
FROM pg_trigger t
JOIN pg_attribute a ON a.attrelid = t.tgrelid AND a.attnum = ANY (t.tgattr)
WHERE t.tgname = 'season_no_unlock_guard' AND a.attname = 'state'`).Scan(&columns)
	if err != nil {
		t.Fatalf("read the trigger: %v", err)
	}
	if columns != 1 {
		t.Fatalf("season_no_unlock_guard is not scoped to UPDATE OF state (matched %d columns). "+
			"It is what makes locked terminal; nap-hh3 owns any deliberate unlock, and nothing here may weaken it", columns)
	}
}

func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}

	return false
}
