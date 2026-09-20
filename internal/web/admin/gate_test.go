package admin

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/auth"
	"github.com/ERaith/nightofathousandpixels/internal/store"
)

// The admin gate, which is the one piece of this package that has to be right
// the first time: everything behind it can rewrite a season.
//
// It is tested with a fake store because every question here is about the gate
// and not about Postgres. The writes behind it are tested against a real
// database in seasons_db_test.go, which is the split this codebase keeps
// everywhere.

const testSecret = "0123456789abcdef0123456789abcdef"

// gateStore is the read path, with no database.
type gateStore struct {
	people   map[uuid.UUID]store.Person
	season   store.Season
	noSeason bool
	members  map[uuid.UUID]store.SeasonMember
}

func (g *gateStore) GetPerson(_ context.Context, id uuid.UUID) (store.Person, error) {
	p, ok := g.people[id]
	if !ok {
		return store.Person{}, pgx.ErrNoRows
	}

	return p, nil
}

func (g *gateStore) GetCurrentSeason(context.Context) (store.Season, error) {
	if g.noSeason {
		return store.Season{}, pgx.ErrNoRows
	}

	return g.season, nil
}

func (g *gateStore) GetSeason(_ context.Context, id uuid.UUID) (store.Season, error) {
	if g.noSeason || g.season.ID != id {
		return store.Season{}, pgx.ErrNoRows
	}

	return g.season, nil
}

func (g *gateStore) GetSeasonMember(_ context.Context, arg store.GetSeasonMemberParams) (store.SeasonMember, error) {
	m, ok := g.members[arg.PersonID]
	if !ok {
		return store.SeasonMember{}, pgx.ErrNoRows
	}

	return m, nil
}

func (g *gateStore) ListSeasons(context.Context) ([]store.Season, error) {
	if g.noSeason {
		return nil, nil
	}

	return []store.Season{g.season}, nil
}

// noBegin stands in for the pool on the gate tests. Calling it is a test
// failure rather than a stub: the gate must never start a transaction.
type noBegin struct{ t *testing.T }

func (n noBegin) Begin(context.Context) (pgx.Tx, error) {
	n.t.Helper()
	n.t.Fatal("the gate started a database transaction")

	return nil, nil
}

func gateService(t *testing.T, g *gateStore, bootstrap func(string) bool) (*Service, *auth.Sessions) {
	t.Helper()

	sessions, err := auth.NewSessions(testSecret, "https://pixels.example.test", 0)
	if err != nil {
		t.Fatalf("NewSessions: %v", err)
	}

	return &Service{opts: Options{
		Store:            g,
		DB:               noBegin{t},
		Sessions:         sessions,
		LoginPath:        "/auth/login",
		IsBootstrapAdmin: bootstrap,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}, sessions
}

func person(email string) store.Person {
	return store.Person{ID: uuid.New(), Email: email, EmailNormalized: email, DisplayName: "Test"}
}

// signedIn returns a request carrying a valid session for p.
func signedIn(t *testing.T, sessions *auth.Sessions, p store.Person) *http.Request {
	t.Helper()

	rec := httptest.NewRecorder()
	if err := sessions.Issue(rec, p.ID, p.Email); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, SeasonsPath, nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	return req
}

// run puts a request through the gate and reports whether the handler behind
// it ran, along with what the gate wrote.
func run(t *testing.T, s *Service, req *http.Request) (bool, *httptest.ResponseRecorder, admin) {
	t.Helper()

	var (
		ran  bool
		seen admin
	)
	handler := s.requireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ran = true
		seen = mustAdmin(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return ran, rec, seen
}

// TestTheGateAdmitsASeasonAdmin is the ordinary path: a season_member row with
// is_admin.
func TestTheGateAdmitsASeasonAdmin(t *testing.T) {
	ada := person("ada@example.test")
	season := store.Season{ID: uuid.New(), Year: 2026, State: "submitting"}
	g := &gateStore{
		people:  map[uuid.UUID]store.Person{ada.ID: ada},
		season:  season,
		members: map[uuid.UUID]store.SeasonMember{ada.ID: {SeasonID: season.ID, PersonID: ada.ID, IsAdmin: true}},
	}
	s, sessions := gateService(t, g, nil)

	ran, rec, seen := run(t, s, signedIn(t, sessions, ada))
	if !ran {
		t.Fatalf("a season admin was refused: %d", rec.Code)
	}
	if seen.viaBootstrap {
		t.Error("a season admin was reported as a bootstrap admin")
	}
	if !seen.hasSeason {
		t.Error("the current season did not reach the handler")
	}
}

// TestTheGateRefusesAnOrdinaryMember. Being on the list is not running the
// season.
//
// 404 rather than 403, following signin.RequireAdmin: there is nothing for
// them to ask for, and naming the page only tells them it exists.
func TestTheGateRefusesAnOrdinaryMember(t *testing.T) {
	bob := person("bob@example.test")
	season := store.Season{ID: uuid.New(), Year: 2026, State: "submitting"}
	g := &gateStore{
		people:  map[uuid.UUID]store.Person{bob.ID: bob},
		season:  season,
		members: map[uuid.UUID]store.SeasonMember{bob.ID: {SeasonID: season.ID, PersonID: bob.ID, IsAdmin: false}},
	}
	s, sessions := gateService(t, g, nil)

	ran, rec, _ := run(t, s, signedIn(t, sessions, bob))
	if ran {
		t.Fatal("an ordinary member reached an admin screen")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestTheGateRefusesANonMember, likewise.
func TestTheGateRefusesANonMember(t *testing.T) {
	stranger := person("stranger@example.test")
	g := &gateStore{
		people: map[uuid.UUID]store.Person{stranger.ID: stranger},
		season: store.Season{ID: uuid.New(), Year: 2026, State: "submitting"},
	}
	s, sessions := gateService(t, g, nil)

	ran, rec, _ := run(t, s, signedIn(t, sessions, stranger))
	if ran {
		t.Fatal("somebody with no membership row reached an admin screen")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestTheGateStartsASignInForAnAnonymousVisitor. No session is not a refusal;
// it is somebody who has not said who they are yet.
func TestTheGateStartsASignInForAnAnonymousVisitor(t *testing.T) {
	g := &gateStore{}
	s, _ := gateService(t, g, nil)

	ran, rec, _ := run(t, s, httptest.NewRequest(http.MethodGet, SeasonsPath, nil))
	if ran {
		t.Fatal("an anonymous request reached an admin screen")
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/auth/login" {
		t.Errorf("Location = %q, want /auth/login", got)
	}
}

// TestTheBootstrapAdminGetsInWithNoSeasonAtAll is the whole reason this
// package has a gate of its own.
//
// On a fresh production database there is no season, so there is no membership
// row, so the per-season rule refuses everybody -- including the person trying
// to create the first season. signin.RequireMember would not even get as far
// as refusing: it renders "No season is open".
func TestTheBootstrapAdminGetsInWithNoSeasonAtAll(t *testing.T) {
	ada := person("ada@example.test")
	g := &gateStore{
		people:   map[uuid.UUID]store.Person{ada.ID: ada},
		noSeason: true,
	}
	s, sessions := gateService(t, g, func(email string) bool { return email == "ada@example.test" })

	ran, rec, seen := run(t, s, signedIn(t, sessions, ada))
	if !ran {
		t.Fatalf("the bootstrap admin could not reach the screen that creates the first season: %d", rec.Code)
	}
	if !seen.viaBootstrap {
		t.Error("the page was not told this admin is here on the bootstrap list")
	}
	if seen.hasSeason {
		t.Error("hasSeason is true with no season")
	}
}

// TestTheBootstrapListDoesNotShadowARealAdmin.
//
// Somebody who is a real admin of the live season AND happens to be named in
// the environment variable is here on the membership row. The list page tells
// people which it is, so this must not lie.
func TestTheBootstrapListDoesNotShadowARealAdmin(t *testing.T) {
	ada := person("ada@example.test")
	season := store.Season{ID: uuid.New(), Year: 2026, State: "submitting"}
	g := &gateStore{
		people:  map[uuid.UUID]store.Person{ada.ID: ada},
		season:  season,
		members: map[uuid.UUID]store.SeasonMember{ada.ID: {SeasonID: season.ID, PersonID: ada.ID, IsAdmin: true}},
	}
	s, sessions := gateService(t, g, func(string) bool { return true })

	ran, _, seen := run(t, s, signedIn(t, sessions, ada))
	if !ran {
		t.Fatal("refused")
	}
	if seen.viaBootstrap {
		t.Error("a real season admin was reported as being here on the bootstrap list")
	}
}

// TestTheBootstrapListIsMatchedOnTheNormalizedAddress.
//
// The gate passes person.email_normalized, never person.email. Those differ
// for anybody who signed in with a capital letter, and matching the wrong one
// means the variable silently grants nobody anything -- which looks exactly
// like "the deployment forgot to set it" on the one day it matters.
func TestTheBootstrapListIsMatchedOnTheNormalizedAddress(t *testing.T) {
	ada := store.Person{
		ID:              uuid.New(),
		Email:           "Ada@Example.Test",
		EmailNormalized: "ada@example.test",
		DisplayName:     "Ada",
	}
	g := &gateStore{
		people:   map[uuid.UUID]store.Person{ada.ID: ada},
		noSeason: true,
	}

	var asked string
	s, sessions := gateService(t, g, func(email string) bool {
		asked = email

		return email == "ada@example.test"
	})

	ran, _, _ := run(t, s, signedIn(t, sessions, ada))
	if asked != ada.EmailNormalized {
		t.Errorf("the gate asked about %q; it must pass person.email_normalized (%q)", asked, ada.EmailNormalized)
	}
	if !ran {
		t.Error("the bootstrap admin was refused on a case difference")
	}
}

// TestNoBootstrapListMeansNobody. Nil is the right default and the right
// steady state.
func TestNoBootstrapListMeansNobody(t *testing.T) {
	ada := person("ada@example.test")
	g := &gateStore{
		people:   map[uuid.UUID]store.Person{ada.ID: ada},
		noSeason: true,
	}
	s, sessions := gateService(t, g, nil)

	ran, rec, _ := run(t, s, signedIn(t, sessions, ada))
	if ran {
		t.Fatal("with no bootstrap list configured, somebody still got in")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestASessionNamingSomebodyGoneIsClearedRatherThanLooped.
//
// An authentic cookie for a person who is no longer in the table is unusable.
// Leaving it set would be a permanent redirect loop through sign-in.
func TestASessionNamingSomebodyGoneIsClearedRatherThanLooped(t *testing.T) {
	ghost := person("ghost@example.test")
	g := &gateStore{people: map[uuid.UUID]store.Person{}} // deliberately empty
	s, sessions := gateService(t, g, func(string) bool { return true })

	ran, rec, _ := run(t, s, signedIn(t, sessions, ghost))
	if ran {
		t.Fatal("a session naming a person who does not exist reached an admin screen")
	}
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", rec.Code)
	}

	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 || c.Value == "" {
			cleared = true
		}
	}
	if !cleared {
		t.Error("the unusable session cookie was not cleared")
	}
}

// TestEveryAdminRouteIsBehindTheGate.
//
// Routes mounts five handlers and it would be entirely possible to add a sixth
// outside the group -- which for the POSTs means accepting a new season from
// anybody who can reach the port. This drives the real router.
func TestEveryAdminRouteIsBehindTheGate(t *testing.T) {
	g := &gateStore{}
	s, _ := gateService(t, g, nil)

	router := chi.NewRouter()
	s.Routes(router)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, RootPath},
		{http.MethodGet, SeasonsPath},
		{http.MethodPost, SeasonsPath},
		{http.MethodGet, NewSeasonPath},
		{http.MethodGet, SeasonsPath + "/" + uuid.New().String()},
		{http.MethodPost, SeasonsPath + "/" + uuid.New().String()},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))

			// Anonymous, so the gate's answer is a redirect to sign-in. Any
			// 2xx here means the route is mounted outside the group.
			if rec.Code != http.StatusSeeOther {
				t.Errorf("status = %d, want 303 to sign-in — this route is not behind requireAdmin", rec.Code)
			}
		})
	}
}
