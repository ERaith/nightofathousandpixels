// Package signin is where authentication becomes authorisation.
//
// internal/auth stops at "this is a verified human with this verified email
// address" and deliberately mints nothing. This package takes that identity
// and does the three things that turn it into a usable session:
//
//  1. resolves it to a person row (resolve.go), which is where the account
//     takeover guard lives;
//  2. mints the session cookie (internal/auth.Sessions);
//  3. decides, per request and per season, whether that person may act.
//
// Step 3 is separate from steps 1 and 2 on purpose, and the separation is
// visible in the routing: signing in always succeeds for anyone Google will
// vouch for, and the whitelist is a gate in FRONT of the pages that do
// something. That is why a person who is not on the list gets a page that
// knows who they are and tells them what to ask for, rather than a 403 -- and
// why someone removed from the whitelist mid-season stops being able to act on
// their next click rather than when their cookie expires.
package signin

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/auth"
	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
)

// Paths this package mounts. The two auth paths must match what the
// Authenticator was configured with, which is why they are read back off it
// rather than repeated here.
const (
	// SignedInPath is where a completed sign-in lands.
	SignedInPath = "/me"

	// LogoutPath ends a session. POST only; see signOutButton in pages.templ.
	LogoutPath = "/auth/logout"
)

// Store is the slice of the query set this package needs. It is an interface
// rather than *store.Queries so the gate can be tested without a database;
// the production wiring passes the real query set.
type Store interface {
	personStore

	GetCurrentSeason(ctx context.Context) (store.Season, error)
	GetSeasonMember(ctx context.Context, arg store.GetSeasonMemberParams) (store.SeasonMember, error)
	GetPerson(ctx context.Context, id uuid.UUID) (store.Person, error)
}

// Options configures the Service.
type Options struct {
	Auth     *auth.Authenticator
	Sessions *auth.Sessions
	Store    Store
	Logger   *slog.Logger

	// Origin and Theme are passed through to every page this package renders,
	// so its pages look like the rest of the site.
	Origin string
	Theme  templates.Theme

	// Nav is the site header's navigation, so these pages carry the same
	// header as the others.
	Nav []templates.NavItem

	// SignInHref is where the header's account control sends a reader who is
	// not signed in. Blank renders no control, which is what a build with no
	// authentication wired up wants.
	//
	// Several pages here are rendered TO a signed-in person -- the whitelist
	// refusal above all, where "you signed in with the wrong Google account"
	// is the most likely explanation for what they are reading -- so pageFor
	// below replaces this with the account half whenever the handler has the
	// person in hand. See nap-1j5.
	SignInHref string
}

// Service mounts the sign-in routes and gates the pages behind them.
type Service struct {
	opts Options

	// loginPath is captured from the Authenticator at construction rather than
	// read back from it per request. The gate needs somewhere to send an
	// anonymous visitor, and that is the only thing it needed the
	// Authenticator for -- holding the string instead means the gate can be
	// tested without standing up a provider, which is the difference between
	// a test that runs and a test that skips.
	loginPath string
}

// New returns a Service. Every field of Options except Theme and Nav is
// required; New panics on a missing one, because a server that starts with
// half a sign-in flow is worse than one that does not start.
func New(opts Options) *Service {
	switch {
	case opts.Auth == nil:
		panic("signin: Options.Auth is required")
	case opts.Sessions == nil:
		panic("signin: Options.Sessions is required")
	case opts.Store == nil:
		panic("signin: Options.Store is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Service{opts: opts, loginPath: opts.Auth.LoginPath()}
}

// Routes mounts the sign-in endpoints. The pages that require membership are
// mounted by the caller behind RequireMember.
func (s *Service) Routes(r chi.Router) {
	r.Get(s.opts.Auth.LoginPath(), s.opts.Auth.Login)
	r.Get(s.opts.Auth.CallbackPath(), s.opts.Auth.CallbackHandler(s.onIdentity, s.onAuthError))
	r.Post(LogoutPath, s.handleLogout)

	// The one page this package owns that needs a member. It is also the
	// smoke test for the whole path: reaching it means the provider, the
	// cookie, the person row and the whitelist all worked.
	r.With(s.RequireMember).Get(SignedInPath, s.handleSignedIn)
}

// onIdentity is the success half of the callback: resolve, then mint.
//
// It does NOT check the whitelist. Authentication succeeded, so a session is
// issued for anyone Google verified -- the gate is RequireMember, and letting
// the session exist is what allows the refusal page to greet the person by
// address and offer them a sign-out button.
func (s *Service) onIdentity(w http.ResponseWriter, r *http.Request, id *auth.Identity) {
	person, err := resolvePerson(r.Context(), s.opts.Store, id)
	if err != nil {
		if errors.Is(err, ErrIdentityCollision) {
			// Deliberately logged at warn with the address: this one needs a
			// human, and the log line is how an admin finds out it happened.
			s.opts.Logger.Warn("sign-in refused: identity collision",
				slog.String("email", id.Email),
				slog.String("subject", id.Subject),
			)
			s.render(w, r, http.StatusConflict, "That address is already claimed",
				IdentityCollisionPage(s.page(r, "That address is already claimed"), id.Email))
			return
		}
		s.opts.Logger.Error("sign-in failed: could not resolve person",
			slog.String("email", id.Email), slog.Any("error", err))
		s.serverError(w, r)
		return
	}

	if err := s.opts.Sessions.Issue(w, person.ID, person.Email); err != nil {
		s.opts.Logger.Error("sign-in failed: could not issue session",
			slog.String("person_id", person.ID.String()), slog.Any("error", err))
		s.serverError(w, r)
		return
	}

	s.opts.Logger.Info("signed in",
		slog.String("person_id", person.ID.String()),
		slog.String("email", person.Email),
	)
	http.Redirect(w, r, SignedInPath, http.StatusSeeOther)
}

// onAuthError handles a failed callback.
//
// Every one of auth's sentinels is a "start again" from the visitor's point of
// view -- a stale bookmark, a back button, an expired flow cookie, a declined
// consent screen -- so they all render the same page. The distinction that
// matters is in the log, not on the screen.
func (s *Service) onAuthError(w http.ResponseWriter, r *http.Request, err error) {
	level := slog.LevelInfo
	switch {
	case errors.Is(err, auth.ErrStateMismatch), errors.Is(err, auth.ErrNonceMismatch):
		// These two are the CSRF and token-binding gates. A stale tab produces
		// them honestly, but so does an attack, so they are louder.
		level = slog.LevelWarn
	case errors.Is(err, auth.ErrEmailNotVerified), errors.Is(err, auth.ErrNoEmail), errors.Is(err, auth.ErrNoSubject):
		level = slog.LevelWarn
	}
	s.opts.Logger.Log(r.Context(), level, "sign-in did not complete", slog.Any("error", err))

	// The session cookie is cleared too: arriving at a failed callback with an
	// old session still set is confusing, and clearing it makes "sign in
	// again" actually start from nothing.
	s.opts.Sessions.Clear(w)

	p := s.page(r, "Sign-in didn't finish")
	s.render(w, r, http.StatusUnauthorized, "Sign-in didn't finish",
		templates.ErrorPage(p, templates.ErrorContent{
			Status:  http.StatusUnauthorized,
			Heading: "Sign-in didn't finish",
			Body: "The sign-in didn't complete — usually a link that sat in a tab too long, " +
				"or a back button pressed at the wrong moment. Nothing is wrong with your account. " +
				"Start again from the front page.",
		}))
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.opts.Sessions.Clear(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Service) handleSignedIn(w http.ResponseWriter, r *http.Request) {
	v := MustCurrent(r.Context())
	s.render(w, r, http.StatusOK, "Signed in",
		SignedInPage(s.pageFor(r, "Signed in", v.Person, v.IsAdmin()), v))
}

// RequireMember is the whitelist gate (ticket C4).
//
// It runs on every request to a page behind it rather than once at sign-in,
// which is the whole reason the session cookie carries no authority: an admin
// who removes someone from the season's whitelist has done it by that person's
// next click.
//
// The four outcomes are deliberately four different answers:
//
//	no session         -> start a sign-in, remembering nothing
//	no season          -> "no season is open", not "you aren't on the list"
//	session, no member -> the friendly page, 200, NOT a 403
//	session and member -> the viewer goes in the context and the handler runs
func (s *Service) RequireMember(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := s.opts.Sessions.Read(r)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidSession) {
				// Tampered, expired, or sealed under an old secret. Clearing
				// it stops the browser presenting it on every request for the
				// next thirty days.
				s.opts.Sessions.Clear(w)
			}
			http.Redirect(w, r, s.loginPath, http.StatusSeeOther)
			return
		}

		person, err := s.opts.Store.GetPerson(r.Context(), session.PersonID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// The cookie is authentic but names a person who is gone. The
				// session is unusable, so it goes rather than becoming a
				// permanent redirect loop.
				s.opts.Logger.Warn("session names a person who no longer exists",
					slog.String("person_id", session.PersonID.String()))
				s.opts.Sessions.Clear(w)
				http.Redirect(w, r, s.loginPath, http.StatusSeeOther)
				return
			}
			s.opts.Logger.Error("whitelist gate: load person", slog.Any("error", err))
			s.serverError(w, r)
			return
		}

		season, err := s.opts.Store.GetCurrentSeason(r.Context())
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				s.render(w, r, http.StatusOK, "No season is open",
					NoSeasonPage(s.pageFor(r, "No season is open", person, false)))
				return
			}
			s.opts.Logger.Error("whitelist gate: load current season", slog.Any("error", err))
			s.serverError(w, r)
			return
		}

		member, err := s.opts.Store.GetSeasonMember(r.Context(), store.GetSeasonMemberParams{
			SeasonID: season.ID,
			PersonID: person.ID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Authenticated, not authorised. 200, because the visitor is
				// not being refused access to something they asked for by
				// mistake -- they are being told the truth about a list they
				// are not on, and there is an action for them to take.
				s.opts.Logger.Info("not on this season's whitelist",
					slog.String("email", person.Email),
					slog.Int("season", int(season.Year)),
				)
				s.render(w, r, http.StatusOK, "You're not on the list yet",
					NotOnTheListPage(s.pageFor(r, "You're not on the list yet", person, false),
						person.Email, season.Year))
				return
			}
			s.opts.Logger.Error("whitelist gate: load membership", slog.Any("error", err))
			s.serverError(w, r)
			return
		}

		ctx := withViewer(r.Context(), Viewer{Person: person, Season: season, Member: member})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin gates on season_member.is_admin. It is mounted INSIDE
// RequireMember, which has already put the viewer in the context.
func (s *Service) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, ok := Current(r.Context())
		if !ok {
			panic("signin: RequireAdmin is mounted outside RequireMember")
		}
		if !v.IsAdmin() {
			// A non-admin member asking for an admin page is a different thing
			// from a non-member: they are on the list, they simply do not run
			// the season. 404 rather than 403 -- there is nothing for them to
			// ask for, and naming the page only tells them it exists.
			s.render(w, r, http.StatusNotFound, templates.NotFoundContent.Heading,
				templates.NotFoundPage(s.pageFor(r, templates.NotFoundContent.Heading, v.Person, v.IsAdmin())))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// page builds the shell data this package's pages share, for a reader whose
// identity the handler does not have: the header offers the way in.
func (s *Service) page(r *http.Request, title string) templates.Page {
	return templates.Page{
		Theme:      s.opts.Theme,
		Origin:     s.opts.Origin,
		Path:       r.URL.Path,
		Nav:        s.opts.Nav,
		Title:      title,
		SignInHref: s.opts.SignInHref,
	}
}

// pageFor is page() for a reader the handler has already identified.
//
// The two halves are mutually exclusive by construction, exactly as in
// board.withViewer: SignInHref is dropped the moment there is somebody to
// greet, so a signed-in person is never offered a second sign-in, and
// CurrentUser stays nil for everyone else so the header cannot greet an empty
// name.
//
// isAdmin is passed rather than read off a membership row because the two
// pages that most need this -- the whitelist refusal and "no season is open"
// -- are reached precisely when there is no membership row to read.
func (s *Service) pageFor(r *http.Request, title string, person store.Person, isAdmin bool) templates.Page {
	p := s.page(r, title)
	p.CurrentUser = &templates.CurrentUser{
		DisplayName: person.DisplayName,
		Email:       person.Email,
		IsAdmin:     isAdmin,
	}
	p.SignInHref = ""
	p.SignOutHref = LogoutPath

	return p
}

func (s *Service) serverError(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, templates.ServerErrorContent.Heading)
	s.render(w, r, http.StatusInternalServerError, templates.ServerErrorContent.Heading, templates.ServerErrorPage(p))
}

// render writes a component as a complete HTML response. Status and
// Content-Type go out before rendering starts, so a template that fails
// halfway leaves a truncated page rather than a second set of headers.
func (s *Service) render(w http.ResponseWriter, r *http.Request, status int, _ string, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		log.Printf("signin: render %s: %v", r.URL.Path, err)
	}
}
