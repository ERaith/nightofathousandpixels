package board_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ERaith/nightofathousandpixels/internal/auth"
	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/board"
)

// testCookieSecret is long enough for auth.NewSessions, which refuses a short
// one. It is a literal because nothing here signs anything a person will see.
const testCookieSecret = "test-cookie-secret-that-is-long-enough-0123456789"

// fakeStore is the read path with no database behind it.
//
// Only the slate is tested through it. The write path deliberately has no fake
// at all: the whole point of createSubmission is what Postgres does with a
// lock and a transaction, and a fake that answered those questions would be
// answering them the way the test author guessed rather than the way the
// database does. See submit_db_test.go.
type fakeStore struct {
	season    store.Season
	seasonErr error

	people  map[uuid.UUID]store.Person
	members map[uuid.UUID]store.SeasonMember
	limits  map[uuid.UUID]int32
	counts  map[uuid.UUID]int64

	slate []store.ListVisibleMoviesWithSubmitterForSeasonRow
	mine  []store.Movie

	// movies backs GetMovie, which the edit page's read path uses to find the
	// film named in the URL. It is keyed by id rather than derived from mine,
	// because the interesting cases are the ones mine would never contain:
	// somebody else's film, and a film from another season.
	movies map[uuid.UUID]store.Movie
}

func (f *fakeStore) GetCurrentSeason(context.Context) (store.Season, error) {
	return f.season, f.seasonErr
}

func (f *fakeStore) GetPerson(_ context.Context, id uuid.UUID) (store.Person, error) {
	p, ok := f.people[id]
	if !ok {
		return store.Person{}, pgx.ErrNoRows
	}

	return p, nil
}

func (f *fakeStore) GetSeasonMember(_ context.Context, arg store.GetSeasonMemberParams) (store.SeasonMember, error) {
	m, ok := f.members[arg.PersonID]
	if !ok {
		return store.SeasonMember{}, pgx.ErrNoRows
	}

	return m, nil
}

func (f *fakeStore) GetEffectiveSubmitLimit(_ context.Context, arg store.GetEffectiveSubmitLimitParams) (int32, error) {
	limit, ok := f.limits[arg.PersonID]
	if !ok {
		return 0, pgx.ErrNoRows
	}

	return limit, nil
}

func (f *fakeStore) CountPersonMoviesInSeason(_ context.Context, arg store.CountPersonMoviesInSeasonParams) (int64, error) {
	return f.counts[arg.SubmittedBy], nil
}

func (f *fakeStore) ListPersonMoviesForSeason(context.Context, store.ListPersonMoviesForSeasonParams) ([]store.Movie, error) {
	return f.mine, nil
}

func (f *fakeStore) GetMovie(_ context.Context, id uuid.UUID) (store.Movie, error) {
	m, ok := f.movies[id]
	if !ok {
		return store.Movie{}, pgx.ErrNoRows
	}

	return m, nil
}

func (f *fakeStore) ListVisibleMoviesWithSubmitterForSeason(context.Context, uuid.UUID) ([]store.ListVisibleMoviesWithSubmitterForSeasonRow, error) {
	return f.slate, nil
}

// noBegin stands in for the pool on the read-only tests. Calling it is a test
// failure rather than a stub response: the slate must never start a
// transaction, and a fake that quietly returned one would hide it doing so.
type noBegin struct{ t *testing.T }

func (n noBegin) Begin(context.Context) (pgx.Tx, error) {
	n.t.Helper()
	n.t.Fatal("the slate started a transaction; it is a read-only page")

	return nil, nil
}

// newTestService wires a Service with the fake read path and a real session
// codec, so that "signed out" is decided by a genuinely absent cookie rather
// than by a flag.
func newTestService(t *testing.T, st board.Store) http.Handler {
	t.Helper()

	sessions, err := auth.NewSessions(testCookieSecret, "http://localhost", 0)
	if err != nil {
		t.Fatalf("new sessions: %v", err)
	}

	svc := board.New(board.Options{
		Store:    st,
		DB:       noBegin{t: t},
		Sessions: sessions,
		// A pass-through gate. The submit routes are not exercised here; what
		// matters is that New accepts a gate and mounts without one being nil.
		RequireMember: func(next http.Handler) http.Handler { return next },
		SignInHref:    "/auth/login",
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	r := chi.NewRouter()
	svc.Routes(r)

	return r
}

func getSlate(t *testing.T, h http.Handler, target string) (*httptest.ResponseRecorder, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec, rec.Body.String()
}

func seasonRow(state string) store.Season {
	return store.Season{
		ID:                 uuid.New(),
		Year:               2026,
		Name:               "Night of a Thousand Pixels 2026",
		State:              state,
		DefaultSubmitLimit: 2,
		VoteOpensAt:        pgtype.Timestamptz{Valid: false},
	}
}

// TestSlateSignedOutWithNoSeason is the first request a fresh install ever
// sees: no season has left draft, nobody is signed in.
//
// GetCurrentSeason returns pgx.ErrNoRows for that, and reading it as a failure
// would make a brand new deployment answer its own front door with a 500.
func TestSlateSignedOutWithNoSeason(t *testing.T) {
	h := newTestService(t, &fakeStore{seasonErr: pgx.ErrNoRows})

	rec, body := getSlate(t, h, board.SlatePath)

	if rec.Code != http.StatusOK {
		t.Fatalf("no season: status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(body, "Nobody has gone first yet") {
		t.Error("no season: the empty board did not render")
	}
	// Signed out and no season: there is nothing to submit to, so the only
	// control offered is the way in.
	if strings.Contains(body, "Put a movie up") {
		t.Error("no season: offered a submit control with no season to submit to")
	}
}

// TestSlateSignedOutShowsSignIn is the common case: a public board, read by
// somebody who has never signed in.
func TestSlateSignedOutShowsSignIn(t *testing.T) {
	season := seasonRow("submitting")
	h := newTestService(t, &fakeStore{
		season: season,
		slate: []store.ListVisibleMoviesWithSubmitterForSeasonRow{{
			Movie: store.Movie{
				ID:          uuid.New(),
				SeasonID:    season.ID,
				Title:       "The Thing",
				Year:        ptr(int32(1982)),
				Description: "Practical effects, no CGI.",
			},
			DisplayName: "Alice Voter",
		}},
	})

	rec, body := getSlate(t, h, board.SlatePath)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	for _, want := range []string{"The Thing (1982)", "Submitted by Alice Voter", "Sign in to add yours"} {
		if !strings.Contains(body, want) {
			t.Errorf("slate is missing %q", want)
		}
	}
	if strings.Contains(body, "Put a movie up") {
		t.Error("offered the submit control to a signed-out visitor")
	}
	// The quota line belongs to somebody with a quota. A signed-out visitor
	// has none, and "0 picks left" would be a claim about a person who is not
	// there.
	if strings.Contains(body, "picks left") {
		t.Error("printed a quota for a signed-out visitor")
	}
}

// TestSlateCreditsTheBlankDisplayName covers a whitelisted person who has
// never signed in: person.display_name defaults to ” and nothing forbids it,
// so the card has to have a fallback rather than "Submitted by ".
func TestSlateCreditsTheBlankDisplayName(t *testing.T) {
	season := seasonRow("submitting")
	h := newTestService(t, &fakeStore{
		season: season,
		slate: []store.ListVisibleMoviesWithSubmitterForSeasonRow{{
			Movie:       store.Movie{ID: uuid.New(), SeasonID: season.ID, Title: "Alien"},
			DisplayName: "",
		}},
	})

	_, body := getSlate(t, h, board.SlatePath)

	if !strings.Contains(body, "Submitted by someone in the group") {
		t.Error("a blank display name did not fall back")
	}
}

// TestSlateAddedFlashOnlyNamesAFilmOnTheBoard pins the confirmation down to
// the row rather than to the query string, which is what makes the
// flash-without-a-flash-store safe.
func TestSlateAddedFlashOnlyNamesAFilmOnTheBoard(t *testing.T) {
	season := seasonRow("submitting")
	id := uuid.New()
	h := newTestService(t, &fakeStore{
		season: season,
		slate: []store.ListVisibleMoviesWithSubmitterForSeasonRow{{
			Movie:       store.Movie{ID: id, SeasonID: season.ID, Title: "Alien", Year: ptr(int32(1979))},
			DisplayName: "Alice Voter",
		}},
	})

	t.Run("a real id is confirmed", func(t *testing.T) {
		_, body := getSlate(t, h, board.SlatePath+"?added="+id.String())
		if !strings.Contains(body, "Alien (1979) is on the board.") {
			t.Error("the submission was not confirmed")
		}
	})

	t.Run("an id that is not on the board says nothing", func(t *testing.T) {
		_, body := getSlate(t, h, board.SlatePath+"?added="+uuid.New().String())
		if strings.Contains(body, "is on the board.") {
			t.Error("confirmed a film that is not on the board")
		}
	})

	t.Run("a crafted value cannot reach the page", func(t *testing.T) {
		_, body := getSlate(t, h, board.SlatePath+"?added=%3Cscript%3Ehello%3C%2Fscript%3E")
		if strings.Contains(body, "hello") {
			t.Error("the added parameter reached the page")
		}
	})
}

// TestSlateSignedInMember is the member's view: the submit control, and the
// quota line that says what it is worth.
//
// The empty board and the filled one are separate cases on purpose, because
// the template offers the submit control from two different places. A board
// with films puts it in the action strip; an empty one folds it into the
// invitation and says "Be the first" instead, and a test that only covered one
// of them would pass while the other silently stopped offering anything.
func TestSlateSignedInMember(t *testing.T) {
	season := seasonRow("submitting")
	person := store.Person{ID: uuid.New(), Email: "alice@example.test", DisplayName: "Alice Voter"}

	newStore := func(rows []store.ListVisibleMoviesWithSubmitterForSeasonRow, used int64) *fakeStore {
		return &fakeStore{
			season:  season,
			people:  map[uuid.UUID]store.Person{person.ID: person},
			members: map[uuid.UUID]store.SeasonMember{person.ID: {SeasonID: season.ID, PersonID: person.ID}},
			limits:  map[uuid.UUID]int32{person.ID: 2},
			counts:  map[uuid.UUID]int64{person.ID: used},
			slate:   rows,
		}
	}

	t.Run("with films on the board", func(t *testing.T) {
		st := newStore([]store.ListVisibleMoviesWithSubmitterForSeasonRow{{
			Movie:       store.Movie{ID: uuid.New(), SeasonID: season.ID, Title: "The Thing", Year: ptr(int32(1982))},
			DisplayName: "Alice Voter",
		}}, 1)

		body := getSlateAs(t, st, person.ID, person.Email)

		if !strings.Contains(body, "Put a movie up") {
			t.Error("a member with a pick left was not offered the submit control")
		}
		if !strings.Contains(body, `href="`+board.SubmitPath+`"`) {
			t.Error("the submit control does not point at the submit form")
		}
		if !strings.Contains(body, "1 pick left of 2") {
			t.Error("the quota line is wrong or missing")
		}
		// Signed in: the header's account half is filled and the sign-in
		// invitation is gone, so nobody is offered a second sign-in.
		if strings.Contains(body, "Sign in to add yours") {
			t.Error("offered a sign-in to somebody who is already signed in")
		}
	})

	t.Run("on an empty board", func(t *testing.T) {
		st := newStore(nil, 0)

		body := getSlateAs(t, st, person.ID, person.Email)

		if !strings.Contains(body, "Be the first") {
			t.Error("an empty board did not invite a member who may submit")
		}
		if !strings.Contains(body, "2 picks left of 2") {
			t.Error("the quota line is wrong or missing")
		}
	})
}

// TestSlateQuotaCasesStayApart is the reason SubmitQuota is not an int.
//
// Three different people, three different answers, and the two that both
// involve a zero must not read the same: a non-member has no quota at all, and
// a member with an explicit limit of 0 may vote and may not submit.
func TestSlateQuotaCasesStayApart(t *testing.T) {
	season := seasonRow("submitting")

	newPerson := func(email string) store.Person {
		return store.Person{ID: uuid.New(), Email: email, DisplayName: email}
	}

	barred := newPerson("barred@example.test")
	atLimit := newPerson("atlimit@example.test")
	stranger := newPerson("stranger@example.test")

	st := &fakeStore{
		season: season,
		people: map[uuid.UUID]store.Person{
			barred.ID:   barred,
			atLimit.ID:  atLimit,
			stranger.ID: stranger,
		},
		members: map[uuid.UUID]store.SeasonMember{
			barred.ID:  {SeasonID: season.ID, PersonID: barred.ID},
			atLimit.ID: {SeasonID: season.ID, PersonID: atLimit.ID},
		},
		// stranger is absent from both maps: no membership row, and so no
		// effective limit. That is a different answer from a limit of 0.
		limits: map[uuid.UUID]int32{barred.ID: 0, atLimit.ID: 2},
		counts: map[uuid.UUID]int64{atLimit.ID: 2},
	}

	cases := []struct {
		name    string
		person  store.Person
		want    string
		notWant []string
	}{
		{
			name:   "an explicit limit of zero is a setting",
			person: barred,
			want:   "You can vote this year, but not put films on the board.",
			// The other zero-shaped answer. A barred member must never be told
			// they used something up: one is a setting somebody chose, the
			// other is progress they made.
			notWant: []string{"You have used all"},
		},
		{
			name:    "a used-up limit is progress",
			person:  atLimit,
			want:    "You have used all 2 picks for this year.",
			notWant: []string{"You can vote this year"},
		},
		{
			// Nothing here, and specifically not "0 picks left": a non-member
			// has no quota, which is a different thing from a quota of zero.
			// The assertion is against the quota sentences rather than against
			// the word "picks", because the season's own summary line says
			// "Put your picks on the board" and is not a claim about anybody.
			name:   "a non-member has no quota to describe",
			person: stranger,
			notWant: []string{
				"picks left", "pick left",
				"You can vote this year", "You have used all",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := getSlateAs(t, st, tc.person.ID, tc.person.Email)

			if tc.want != "" && !strings.Contains(body, tc.want) {
				t.Errorf("missing %q", tc.want)
			}
			for _, unwanted := range tc.notWant {
				if strings.Contains(body, unwanted) {
					t.Errorf("unexpectedly contains %q", unwanted)
				}
			}
			if strings.Contains(body, "Put a movie up") {
				t.Error("offered the submit control to somebody who may not submit")
			}
		})
	}
}

// TestSlateRefusesSubmitOutsideTheWindow: a member with picks left, in the
// voting week. The window overrules the quota.
func TestSlateRefusesSubmitOutsideTheWindow(t *testing.T) {
	for _, state := range []string{"voting", "locked"} {
		t.Run(state, func(t *testing.T) {
			season := seasonRow(state)
			person := store.Person{ID: uuid.New(), Email: "alice@example.test", DisplayName: "Alice Voter"}

			st := &fakeStore{
				season:  season,
				people:  map[uuid.UUID]store.Person{person.ID: person},
				members: map[uuid.UUID]store.SeasonMember{person.ID: {SeasonID: season.ID, PersonID: person.ID}},
				limits:  map[uuid.UUID]int32{person.ID: 2},
				counts:  map[uuid.UUID]int64{person.ID: 0},
			}

			body := getSlateAs(t, st, person.ID, person.Email)

			if strings.Contains(body, "Put a movie up") {
				t.Errorf("state %q: offered the submit control outside the submission window", state)
			}
		})
	}
}

// getSlateAs fetches the slate carrying a real session cookie for personID.
func getSlateAs(t *testing.T, st board.Store, personID uuid.UUID, email string) string {
	t.Helper()

	sessions, err := auth.NewSessions(testCookieSecret, "http://localhost", 0)
	if err != nil {
		t.Fatalf("new sessions: %v", err)
	}

	// Issue against a throwaway recorder to get a genuine sealed cookie, so
	// the handler's own Sessions.Read is what decides who this is.
	issued := httptest.NewRecorder()
	if err := sessions.Issue(issued, personID, email); err != nil {
		t.Fatalf("issue session: %v", err)
	}

	h := newTestService(t, st)
	req := httptest.NewRequest(http.MethodGet, board.SlatePath, nil)
	for _, c := range issued.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	return rec.Body.String()
}

func ptr[T any](v T) *T { return &v }
