// Package admin is the screens that run a season: creating one, and changing
// its dates afterwards.
//
// It is its own package rather than a corner of internal/web because it is the
// one part of the site with a different answer to "who is this". Everywhere
// else, authorisation is signin.RequireMember: a session, the current season,
// and a season_member row. Here that chain has a hole in it that only shows up
// once, and shows up at the worst possible moment.
//
// # The bootstrap problem, and the answer this package implements
//
// Admin is per season -- season_member.is_admin, by design, so that running
// the 2025 season does not hand anybody the keys to 2026. Production is never
// seeded: migration and deploy leave an empty database on purpose, because a
// seeded production is a production with test accounts in it.
//
// Put those two together and the site cannot start. There is no season, so
// there is no season_member row, so nobody is an admin, so nobody can create
// the season that would make somebody one. signin.RequireMember does not even
// get as far as saying no -- with no season it renders "No season is open",
// which is a true and completely unhelpful thing to tell the person who is
// trying to open one.
//
// THE ANSWER: BOOTSTRAP_ADMIN_EMAILS, an environment variable holding one or
// more addresses that administer the admin screens regardless of any
// membership row. See config.Config.BootstrapAdminEmails, requireAdmin below,
// and the "Starting the first season" section of README.md.
//
// Two things make it a bootstrap rather than a back door:
//
//   - It grants exactly these screens. It is not a global admin flag, it does
//     not touch the whitelist gate, and it cannot put a film on the board or
//     cast a ballot -- those still need a season_member row like everybody
//     else's.
//   - It is self-extinguishing. Whoever creates a season is written into it as
//     an admin in the same transaction, so after the first season exists the
//     variable can be removed and the ordinary per-season rule is the only one
//     left.
//
// # The seam
//
// The same three rules hold here as in internal/web/board. No store row
// reaches a template; no decision is made in a template; nothing trusts the
// page it just rendered, so every POST re-establishes what the GET assumed.
// The templ components in this package take viewmodel structs only.
package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/auth"
	"github.com/ERaith/nightofathousandpixels/internal/signin"
	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// The paths this package mounts. All of them are behind requireAdmin.
const (
	// RootPath is what somebody types. It redirects to SeasonsPath.
	//
	// It exists because there is no link to these screens anywhere in the site
	// header yet, so the way an admin arrives is by typing /admin -- and a 404
	// on the guessable spelling of a page that does exist is a bad way to find
	// that out. It is behind the same gate as everything else here, so it is
	// not a way to learn whether the screens exist.
	RootPath = "/admin"

	// SeasonsPath is the list of seasons and the way in to everything else.
	SeasonsPath = RootPath + "/seasons"

	// NewSeasonPath is the create form.
	NewSeasonPath = SeasonsPath + "/new"

	// SeasonPattern is the re-date screen for one season, GET and POST.
	SeasonPattern = SeasonsPath + "/{seasonID}"

	// seasonIDParam is the URL parameter SeasonPattern carries.
	seasonIDParam = "seasonID"
)

// Store is the slice of the query set the read paths need.
//
// The write paths deliberately do not go through it: creating a season writes
// two tables and an audit row, and that is a transaction. See Beginner.
type Store interface {
	GetPerson(ctx context.Context, id uuid.UUID) (store.Person, error)
	GetCurrentSeason(ctx context.Context) (store.Season, error)
	GetSeason(ctx context.Context, id uuid.UUID) (store.Season, error)
	GetSeasonMember(ctx context.Context, arg store.GetSeasonMemberParams) (store.SeasonMember, error)
	ListSeasons(ctx context.Context) ([]store.Season, error)
}

// Beginner starts a transaction. *pgxpool.Pool satisfies it.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Options configures the Service. Store, DB and Sessions are required.
type Options struct {
	// Store is the read path.
	Store Store

	// DB starts the transactions the two writes run in.
	DB Beginner

	// Sessions reads the session cookie. This package holds its own gate
	// rather than being mounted behind signin.RequireMember, so it needs the
	// session directly -- see requireAdmin.
	Sessions *auth.Sessions

	// LoginPath starts a sign-in, for a visitor with no session at all.
	LoginPath string

	// IsBootstrapAdmin reports whether a normalized email address is on the
	// bootstrap list. Normally config.Config.IsBootstrapAdmin.
	//
	// Nil means nobody, which is the right default and the right steady state:
	// once a season exists, admin comes from season_member and this has
	// nothing left to do. It is a function rather than a []string so that this
	// package depends on the question and not on where the answer is
	// configured.
	IsBootstrapAdmin func(emailNormalized string) bool

	Logger *slog.Logger

	// Origin, Theme and Nav are passed through to every page, so the admin
	// screens look like the rest of the site rather than like a tool.
	Origin string
	Theme  templates.Theme
	Nav    []templates.NavItem

	// Location is the timezone every date on these screens is read AND written
	// in. It is the one setting on this package that can silently ruin a
	// season: an admin types "9pm on the 24th" into a datetime-local input,
	// which carries no zone at all, so whatever this is decides what 9pm
	// meant. Nil means time.Local, which is right for a homelab everybody
	// involved lives an hour from.
	Location *time.Location

	// Now is the clock, for the "both dates must be in the future" check.
	// Nil means time.Now. It is a field so that the validation has a test that
	// does not depend on what year it is when it runs.
	Now func() time.Time
}

// Service renders the admin screens.
type Service struct {
	opts Options
}

// New returns a Service. It panics on a missing dependency rather than
// degrading: admin screens with no gate are not a smaller feature, they are a
// site anybody can re-date.
func New(opts Options) *Service {
	switch {
	case opts.Store == nil:
		panic("admin: Options.Store is required")
	case opts.DB == nil:
		panic("admin: Options.DB is required")
	case opts.Sessions == nil:
		panic("admin: Options.Sessions is required - an ungated admin screen is a season anybody can rewrite")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	return &Service{opts: opts}
}

// Routes mounts the admin screens behind requireAdmin.
//
// The gate is on the POSTs as well as the GETs. A gate on the GET alone would
// refuse to show the form and then accept the write, which for this form means
// accepting a new season from anybody who could reach the port.
func (s *Service) Routes(r chi.Router) {
	r.With(s.requireAdmin).Group(func(r chi.Router) {
		r.Get(RootPath, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, SeasonsPath, http.StatusSeeOther)
		})
		r.Get(SeasonsPath, s.handleSeasonList)
		r.Post(SeasonsPath, s.handleCreateSeason)
		r.Get(NewSeasonPath, s.handleNewSeasonForm)
		r.Get(SeasonPattern, s.handleEditSeasonForm)
		r.Post(SeasonPattern, s.handleUpdateSeason)
	})
}

// admin is who is making the current request: the person, and how they came to
// be allowed in.
type admin struct {
	person store.Person

	// season is the current season, when there is one. A bootstrap admin on a
	// brand new deployment has no season at all, which is the entire reason
	// this package does not use signin.RequireMember.
	season    store.Season
	hasSeason bool

	// viaBootstrap records that this person is here on BOOTSTRAP_ADMIN_EMAILS
	// rather than on a membership row. The list page says so; nothing else
	// behaves differently, because the privilege is the same either way.
	viaBootstrap bool
}

// adminKey is the context key. Unexported so no other package can forge one.
type adminKey struct{}

// currentAdmin returns the admin for this request. ok is false on any request
// that did not pass through requireAdmin.
func currentAdmin(ctx context.Context) (admin, bool) {
	a, ok := ctx.Value(adminKey{}).(admin)

	return a, ok
}

// mustAdmin is currentAdmin for handlers that are mounted behind requireAdmin
// and would have nothing sensible to render without one.
func mustAdmin(ctx context.Context) admin {
	a, ok := currentAdmin(ctx)
	if !ok {
		panic("admin: no admin in context - this handler is not mounted behind requireAdmin")
	}

	return a
}

// requireAdmin is the gate on every screen in this package.
//
// It is this package's own rather than signin.RequireAdmin, and the difference
// is the whole bootstrap answer. signin.RequireAdmin sits inside
// signin.RequireMember, which loads the current season first and renders "No
// season is open" when there is none -- so on a fresh production database it
// would stop the one person who could fix that, with a page telling them about
// the thing they were trying to create.
//
// The order is: identity, then the ordinary per-season rule, then the
// bootstrap list as a fallback.
//
//	no session                   -> start a sign-in
//	session names nobody         -> clear it and start a sign-in
//	admin of the current season  -> in, the ordinary way
//	on the bootstrap list        -> in, whether or not a season exists
//	anything else                -> 404
//
// The membership question is asked FIRST and the absence of a season is not an
// error on the way to it -- that is the whole difference from
// signin.RequireAdmin, which renders "No season is open" and never gets as far
// as asking. Asking first also keeps viaBootstrap honest: somebody who is a
// real admin of the live season and happens to also be named in the
// environment variable is here on the membership row, and the list page should
// not tell them otherwise.
//
// 404 and not 403 for the refusal, following signin.RequireAdmin: a non-admin
// asking for an admin page has nothing to ask for, and naming the page only
// tells them it exists.
func (s *Service) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := s.opts.Sessions.Read(r)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidSession) {
				// Tampered, expired, or sealed under a rotated secret.
				// Clearing it stops the browser presenting it on every
				// request for the next thirty days.
				s.opts.Sessions.Clear(w)
			}
			s.redirectToSignIn(w, r)

			return
		}

		person, err := s.opts.Store.GetPerson(r.Context(), session.PersonID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				s.opts.Logger.Warn("admin: session names a person who no longer exists",
					slog.String("person_id", session.PersonID.String()))
				s.opts.Sessions.Clear(w)
				s.redirectToSignIn(w, r)

				return
			}
			s.serverError(w, r, "admin: load person", err)

			return
		}

		a := admin{person: person}

		// No season at all is a state, not an error: it is a fresh production
		// database, which is the one this whole package exists to get out of.
		season, err := s.opts.Store.GetCurrentSeason(r.Context())
		switch {
		case err == nil:
			a.season = season
			a.hasSeason = true
		case errors.Is(err, pgx.ErrNoRows):
		default:
			s.serverError(w, r, "admin: load current season", err)

			return
		}

		seasonAdmin := false
		if a.hasSeason {
			member, err := s.opts.Store.GetSeasonMember(r.Context(), store.GetSeasonMemberParams{
				SeasonID: season.ID,
				PersonID: person.ID,
			})
			switch {
			case err == nil:
				seasonAdmin = member.IsAdmin
			case errors.Is(err, pgx.ErrNoRows):
				// Signed in, not on this year's list. Unremarkable.
			default:
				s.serverError(w, r, "admin: load membership", err)

				return
			}
		}

		if !seasonAdmin {
			if s.opts.IsBootstrapAdmin == nil || !s.opts.IsBootstrapAdmin(person.EmailNormalized) {
				s.opts.Logger.Info("admin: refused",
					slog.String("email", person.Email),
					slog.Bool("has_season", a.hasSeason),
				)
				s.notFound(w, r, &a)

				return
			}
			a.viaBootstrap = true
			s.opts.Logger.Warn("admin: allowed on the bootstrap list",
				slog.String("email", person.Email),
				slog.Bool("has_season", a.hasSeason),
			)
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminKey{}, a)))
	})
}

// redirectToSignIn sends a visitor with no usable session to sign in, or to
// the front page when this build has no sign-in configured.
func (s *Service) redirectToSignIn(w http.ResponseWriter, r *http.Request) {
	target := s.opts.LoginPath
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// page builds the shell every screen here shares.
//
// a may be nil, which is the refusal case: the gate renders a 404 for somebody
// it has identified and for somebody it has not, and the header has to be
// right either way.
func (s *Service) page(r *http.Request, title string, a *admin) viewmodel.LayoutData {
	p := viewmodel.LayoutData{
		Theme:      s.opts.Theme,
		Title:      title,
		Path:       r.URL.Path,
		Origin:     s.opts.Origin,
		Nav:        s.opts.Nav,
		SignInHref: s.opts.LoginPath,
	}
	if a == nil {
		return p
	}

	// The two halves are mutually exclusive by construction, as in
	// board.withViewer: there is somebody to greet, so the second sign-in
	// offer goes.
	p.CurrentUser = &viewmodel.CurrentUser{
		DisplayName: a.person.DisplayName,
		Email:       a.person.Email,
		IsAdmin:     true,
	}
	p.SignInHref = ""
	p.SignOutHref = signin.LogoutPath

	return p
}

// notFound renders the site's 404.
func (s *Service) notFound(w http.ResponseWriter, r *http.Request, a *admin) {
	p := s.page(r, templates.NotFoundContent.Heading, a)
	if a != nil {
		// Identified, and not an admin. The header must not claim otherwise.
		p.CurrentUser.IsAdmin = false
	}

	s.render(w, r, http.StatusNotFound, templates.NotFoundPage(p))
}

// render writes a component as a complete HTML response.
//
// Status and Content-Type go out before rendering starts, so a template that
// fails halfway leaves a truncated page rather than a second set of headers.
func (s *Service) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if err := c.Render(r.Context(), w); err != nil {
		s.opts.Logger.Error("admin: render failed",
			slog.String("path", r.URL.Path), slog.Any("error", err))
	}
}

// serverError renders the site's 500. The underlying error is logged rather
// than shown: an admin cannot act on it either.
func (s *Service) serverError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	s.opts.Logger.Error(msg, slog.String("path", r.URL.Path), slog.Any("error", err))

	p := s.page(r, templates.ServerErrorContent.Heading, nil)
	s.render(w, r, http.StatusInternalServerError, templates.ServerErrorPage(p))
}

// seasonHref is the re-date screen for one season.
func seasonHref(id uuid.UUID) string {
	return SeasonsPath + "/" + id.String()
}
