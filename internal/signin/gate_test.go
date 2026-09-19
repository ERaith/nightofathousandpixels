package signin

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/auth"
	"github.com/ERaith/nightofathousandpixels/internal/store"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// gateFixture builds a Service whose gate can be exercised without a provider.
// Auth is a real Authenticator only where the gate needs LoginPath, so
// discovery never runs.
func gateFixture(t *testing.T, f *fakeStore) (*Service, *auth.Sessions) {
	t.Helper()

	sessions, err := auth.NewSessions(testSecret, "https://pixels.example.test", 0)
	if err != nil {
		t.Fatalf("NewSessions: %v", err)
	}
	return &Service{
		opts: Options{
			Sessions: sessions,
			Store:    f,
			Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			Origin:   "https://pixels.example.test",
		},
		loginPath: "/auth/login",
	}, sessions
}

// signedInRequest returns a request carrying a valid session for personID.
func signedInRequest(t *testing.T, sessions *auth.Sessions, personID uuid.UUID, email string) *http.Request {
	t.Helper()

	rec := httptest.NewRecorder()
	if err := sessions.Issue(rec, personID, email); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	return req
}

// reached records whether the protected handler ran.
func reached(flag *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*flag = true
		w.WriteHeader(http.StatusOK)
	})
}

// TestGateAdmitsAMember is the happy path, and it also proves the viewer
// arrives in the context rather than the handler having to look anything up.
func TestGateAdmitsAMember(t *testing.T) {
	t.Parallel()

	person := newPerson(t, "alice@example.test", "sub-alice")
	season := store.Season{ID: uuid.New(), Year: 2026, Name: "2026", State: "submitting"}
	f := &fakeStore{
		personByID: map[uuid.UUID]store.Person{person.ID: person},
		season:     season,
		member:     store.SeasonMember{SeasonID: season.ID, PersonID: person.ID},
	}
	s, sessions := gateFixture(t, f)

	var ran bool
	var seen Viewer
	handler := s.RequireMember(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ran = true
		seen = MustCurrent(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedInRequest(t, sessions, person.ID, person.Email))

	if !ran {
		t.Fatal("the protected handler did not run for a whitelisted member")
	}
	if seen.Person.ID != person.ID || seen.Season.Year != 2026 {
		t.Errorf("viewer = %+v, want person %v in season 2026", seen, person.ID)
	}
	if seen.IsAdmin() {
		t.Error("an ordinary member reads as admin")
	}
}

// TestNotOnTheListIsAFriendlyPageNotA403 is ticket C4's actual requirement.
//
// The status code is asserted explicitly because 403 is what this code would
// become under any "tidy up the error handling" pass, and the difference is
// the whole point: the visitor is not being caught doing something forbidden,
// they are being told they were never added.
func TestNotOnTheListIsAFriendlyPageNotA403(t *testing.T) {
	t.Parallel()

	person := newPerson(t, "stranger@example.test", "sub-stranger")
	f := &fakeStore{
		personByID: map[uuid.UUID]store.Person{person.ID: person},
		season:     store.Season{ID: uuid.New(), Year: 2026, Name: "2026", State: "submitting"},
		memberErr:  pgx.ErrNoRows,
	}
	s, sessions := gateFixture(t, f)

	var ran bool
	rec := httptest.NewRecorder()
	s.RequireMember(reached(&ran)).ServeHTTP(rec, signedInRequest(t, sessions, person.ID, person.Email))

	if ran {
		t.Fatal("a non-member reached the protected handler")
	}
	if rec.Code == http.StatusForbidden {
		t.Error("the whitelist refusal is a 403: ticket C4 asks for a friendly page, not a refusal code")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	// The address has to be on the page: the most common cause of landing
	// here is signing in with the wrong Google account, and you cannot see
	// that unless you are told which one you used.
	if !strings.Contains(body, "stranger@example.test") {
		t.Error("the refusal page does not name the address that was refused")
	}
	if !strings.Contains(body, "Eraith") {
		t.Error("the refusal page does not say who to ask")
	}
	if !strings.Contains(body, "/auth/logout") {
		t.Error("the refusal page offers no way to sign out and try another account")
	}
}

// TestGateSendsAnonymousVisitorsToSignIn: no session is not an error, it is a
// person who has not signed in yet.
func TestGateSendsAnonymousVisitorsToSignIn(t *testing.T) {
	t.Parallel()

	s, _ := gateFixture(t, &fakeStore{})

	var ran bool
	rec := httptest.NewRecorder()
	s.RequireMember(reached(&ran)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))

	if ran {
		t.Fatal("an anonymous request reached the protected handler")
	}
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303 to the sign-in", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/auth/login" {
		t.Errorf("Location = %q, want /auth/login", got)
	}
}

// TestUnusableSessionIsClearedNotLoopedOn: a cookie that is authentic but
// names a person who is gone, or one sealed under a rotated COOKIE_SECRET,
// must be cleared. Left in place, the browser presents it on every request and
// the redirect to sign-in becomes a loop that only ends when it expires.
func TestUnusableSessionIsClearedNotLoopedOn(t *testing.T) {
	t.Parallel()

	s, sessions := gateFixture(t, &fakeStore{personByID: map[uuid.UUID]store.Person{}})

	var ran bool
	rec := httptest.NewRecorder()
	// A valid session naming a person the store does not have.
	s.RequireMember(reached(&ran)).ServeHTTP(rec, signedInRequest(t, sessions, uuid.New(), "ghost@example.test"))

	if ran {
		t.Fatal("a session naming a missing person reached the protected handler")
	}
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "nap_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("the unusable session cookie was not cleared: the browser will keep presenting it")
	}
}

// TestNoSeasonIsNotTheSameAsNotOnTheList: telling somebody to ask an admin to
// add them to a season that does not exist is a dead end, so the two answers
// are kept apart.
func TestNoSeasonIsNotTheSameAsNotOnTheList(t *testing.T) {
	t.Parallel()

	person := newPerson(t, "alice@example.test", "sub-alice")
	f := &fakeStore{
		personByID: map[uuid.UUID]store.Person{person.ID: person},
		seasonErr:  pgx.ErrNoRows,
	}
	s, sessions := gateFixture(t, f)

	var ran bool
	rec := httptest.NewRecorder()
	s.RequireMember(reached(&ran)).ServeHTTP(rec, signedInRequest(t, sessions, person.ID, person.Email))

	if ran {
		t.Fatal("the protected handler ran with no season")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No season is open") {
		t.Errorf("expected the no-season page, got:\n%s", body)
	}
	if strings.Contains(body, "not on the list") {
		t.Error("a missing season is being reported as 'not on the list', which sends people to ask for something that does not exist")
	}
}

// TestAuthorityIsReadPerRequestNotFromTheCookie proves the property the
// session's design pays for: an admin flag that changes in the database is
// visible on the very next request, with the same cookie.
func TestAuthorityIsReadPerRequestNotFromTheCookie(t *testing.T) {
	t.Parallel()

	person := newPerson(t, "admin@example.test", "sub-admin")
	season := store.Season{ID: uuid.New(), Year: 2026, Name: "2026", State: "submitting"}
	f := &fakeStore{
		personByID: map[uuid.UUID]store.Person{person.ID: person},
		season:     season,
		member:     store.SeasonMember{SeasonID: season.ID, PersonID: person.ID, IsAdmin: true},
	}
	s, sessions := gateFixture(t, f)

	req := signedInRequest(t, sessions, person.ID, person.Email)

	var first Viewer
	s.RequireMember(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		first = MustCurrent(r.Context())
	})).ServeHTTP(httptest.NewRecorder(), req)
	if !first.IsAdmin() {
		t.Fatal("the seeded admin did not read as admin")
	}

	// Revoke admin in the database. The cookie is untouched.
	f.member.IsAdmin = false

	var second Viewer
	s.RequireMember(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		second = MustCurrent(r.Context())
	})).ServeHTTP(httptest.NewRecorder(), req)
	if second.IsAdmin() {
		t.Error("admin survived its removal from the database: authority is coming from the cookie, " +
			"so revoking it would not take effect until the session expired")
	}
}

// TestRemovalFromTheWhitelistTakesEffectImmediately is the same property for
// membership rather than admin.
func TestRemovalFromTheWhitelistTakesEffectImmediately(t *testing.T) {
	t.Parallel()

	person := newPerson(t, "bob@example.test", "sub-bob")
	season := store.Season{ID: uuid.New(), Year: 2026, Name: "2026", State: "submitting"}
	f := &fakeStore{
		personByID: map[uuid.UUID]store.Person{person.ID: person},
		season:     season,
		member:     store.SeasonMember{SeasonID: season.ID, PersonID: person.ID},
	}
	s, sessions := gateFixture(t, f)
	req := signedInRequest(t, sessions, person.ID, person.Email)

	var ran bool
	s.RequireMember(reached(&ran)).ServeHTTP(httptest.NewRecorder(), req)
	if !ran {
		t.Fatal("the member was not admitted to begin with")
	}

	// Removed from the season. Same cookie, same request.
	f.memberErr = pgx.ErrNoRows
	ran = false
	rec := httptest.NewRecorder()
	s.RequireMember(reached(&ran)).ServeHTTP(rec, req)

	if ran {
		t.Error("a removed member still reached the protected handler: the gate is not re-reading membership")
	}
	if !strings.Contains(rec.Body.String(), "not on the list") {
		t.Error("expected the refusal page after removal")
	}
}
