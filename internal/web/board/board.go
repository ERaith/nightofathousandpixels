// Package board is the two pages this site exists for: the slate, and the form
// that puts a film on it.
//
// It is the seam internal/web deliberately does not cross. That package is
// presentation with no database at all; this one is where a request becomes
// queries, and queries become a view model. Nothing here renders markup of its
// own — it fills the structs in internal/web/viewmodel and hands them to
// internal/web/templates, which is why a template change and a handler change
// never touch the same file.
//
// Three rules hold everywhere in here, and all three are load-bearing:
//
//   - No store row reaches a template. Every pgtype, every nullable pointer
//     and every uuid is resolved on this side of the seam.
//   - No decision is made in a template. "May this person submit right now" is
//     answered once, here, from the season window, membership and the quota
//     together, and travels as a bool.
//   - Nothing trusts the page it just rendered. The submit form is only ever
//     shown to someone who may use it, and the POST re-establishes the same
//     three facts inside the transaction that writes — because the form is a
//     round trip through a browser, and the world moves in between.
package board

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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ERaith/nightofathousandpixels/internal/auth"
	"github.com/ERaith/nightofathousandpixels/internal/signin"
	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// The paths this package mounts.
const (
	// SlatePath is the board. It is public: anybody with the link can read
	// what is up, which is the whole reason the link gets pasted into the
	// group chat.
	SlatePath = "/slate"

	// SubmitPath is the form. It is mounted behind the whitelist gate.
	SubmitPath = "/submit"
)

// addedParam names the movie a redirect just created, so that the slate can
// confirm it.
//
// This is how the site does a flash message without a flash store. A
// successful POST redirects (POST-redirect-GET, so a refresh cannot
// double-submit) and carries the new movie's id in the query string; the slate
// looks that id up in the list it has already loaded and, only if it finds it,
// says so. The value is therefore never echoed — the confirmation is built
// from the row, not from the parameter — so a crafted link can at worst
// produce no notice at all.
const addedParam = "added"

// stateSubmitting is the one season state in which the board accepts writes.
// It is compared against season.state, whose values are fixed by the
// season_state_valid CHECK in migration 00002.
const stateSubmitting = string(viewmodel.SeasonSubmitting)

// Store is the slice of the query set these two pages need.
//
// It is an interface rather than *store.Queries so that the read paths can be
// tested without a database. The write path deliberately does not go through
// it: see Beginner.
type Store interface {
	GetCurrentSeason(ctx context.Context) (store.Season, error)
	GetPerson(ctx context.Context, id uuid.UUID) (store.Person, error)
	GetSeasonMember(ctx context.Context, arg store.GetSeasonMemberParams) (store.SeasonMember, error)
	GetMovie(ctx context.Context, id uuid.UUID) (store.Movie, error)
	GetEffectiveSubmitLimit(ctx context.Context, arg store.GetEffectiveSubmitLimitParams) (int32, error)
	CountPersonMoviesInSeason(ctx context.Context, arg store.CountPersonMoviesInSeasonParams) (int64, error)
	ListPersonMoviesForSeason(ctx context.Context, arg store.ListPersonMoviesForSeasonParams) ([]store.Movie, error)
	ListVisibleMoviesWithSubmitterForSeason(ctx context.Context, seasonID uuid.UUID) ([]store.ListVisibleMoviesWithSubmitterForSeasonRow, error)
}

// Beginner starts a transaction. *pgxpool.Pool satisfies it.
//
// The submit handler needs this rather than a Store because the cap is
// check-then-insert and has to hold under concurrency: the count, the lock
// that serialises it and the insert are one transaction or they are nothing.
// store.New takes a DBTX, and a pgx.Tx is one, so the same generated query set
// runs inside it.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Options configures the Service. Store, DB and Sessions are required.
type Options struct {
	// Store is the read path.
	Store Store

	// DB starts the transaction the submit handler writes in.
	DB Beginner

	// Sessions reads the session cookie. The slate needs it because the slate
	// is public and still has to know who is looking: it is the one page here
	// that is neither behind the gate nor indifferent to the viewer.
	Sessions *auth.Sessions

	// RequireMember is the whitelist gate, normally signin.Service.RequireMember.
	// It is a field rather than an import so that this package depends on the
	// shape of the gate and not on the service that owns it.
	//
	// Nil is refused by New. A submit form mounted without a gate would accept
	// a write from anybody who could reach the port, and "we forgot to wrap
	// it" is not something that should be possible to do quietly.
	RequireMember func(http.Handler) http.Handler

	// SignInHref starts a sign-in, for the "sign in to add yours" control on
	// the public slate. Blank renders no such control.
	SignInHref string

	Logger *slog.Logger

	// Origin and Theme are passed through to every page, so these look like
	// the rest of the site.
	Origin string
	Theme  templates.Theme
	Nav    []templates.NavItem

	// Location is the timezone every date on these pages is rendered in.
	//
	// It is a field rather than time.Local because viewmodel has no opinion
	// about the site's timezone and a test that rendered in the machine's
	// would pass in one place and fail in another. Nil means time.Local, which
	// is right for a homelab everybody involved lives an hour from.
	Location *time.Location
}

// Service renders the slate and the submit form.
type Service struct {
	opts Options
}

// New returns a Service. It panics on a missing dependency rather than
// degrading: a board with no gate, or no database, is not a smaller feature,
// it is a hole.
func New(opts Options) *Service {
	switch {
	case opts.Store == nil:
		panic("board: Options.Store is required")
	case opts.DB == nil:
		panic("board: Options.DB is required")
	case opts.Sessions == nil:
		panic("board: Options.Sessions is required")
	case opts.RequireMember == nil:
		panic("board: Options.RequireMember is required - an ungated submit form accepts writes from anybody")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}

	return &Service{opts: opts}
}

// Routes mounts the two pages.
//
// The asymmetry is the point. The slate is public and signs nobody in; the
// submit form is behind the gate, and so is its POST — a gate on the GET alone
// would refuse to show the form and then happily accept the write.
func (s *Service) Routes(r chi.Router) {
	r.Get(SlatePath, s.handleSlate)

	r.With(s.opts.RequireMember).Group(func(r chi.Router) {
		r.Get(SubmitPath, s.handleSubmitForm)
		r.Post(SubmitPath, s.handleSubmitPost)
	})

	// Changing or withdrawing a film you already put up (ticket E3). It lives
	// in edit.go and mounts its own routes behind the same gate, so that the
	// three routes and the five preconditions they share sit in one file.
	s.editRoutes(r)
}

// viewer is who is looking at a public page: nobody, somebody signed in who is
// not on this season's list, or a member.
//
// It exists because the slate is the one page here that is public AND cares
// who you are, so it cannot use signin.RequireMember (which redirects a
// stranger to a sign-in they did not ask for) and cannot use signin.Current
// (which is only populated behind that gate). Everything it establishes is
// re-established from scratch on the submit path, by the gate and then again
// inside the write transaction; nothing downstream trusts this.
type viewer struct {
	person store.Person
	member store.SeasonMember

	// isMember is whether there is a season_member row. It is separate from
	// member because the zero SeasonMember is indistinguishable from a real
	// row belonging to a non-admin, and "not on the list" must not read as
	// "on the list, not an admin".
	isMember bool
}

// currentViewer resolves the signed-in person for a public page, or nil.
//
// Every failure is anonymous rather than fatal. A cookie that is expired,
// tampered with or sealed under a rotated secret means "not signed in" on a
// page anybody may read; it is signin.RequireMember's job to clear it and say
// so, on the next request that actually needs an identity. A public page that
// started deleting cookies would be a surprising thing for a public page to do.
//
// A database error is different and is returned: it means the page cannot be
// built correctly, and quietly rendering the signed-out version would show a
// member the "sign in to add yours" button they do not need.
func (s *Service) currentViewer(r *http.Request, seasonID uuid.UUID) (*viewer, error) {
	session, err := s.opts.Sessions.Read(r)
	if err != nil {
		return nil, nil
	}

	person, err := s.opts.Store.GetPerson(r.Context(), session.PersonID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// An authentic cookie naming a person who is gone. Anonymous, for
			// the same reason as above.
			return nil, nil
		}

		return nil, err
	}

	v := &viewer{person: person}

	// No season yet means nothing to be a member of. Asking anyway would be a
	// lookup against the nil uuid.
	if seasonID == uuid.Nil {
		return v, nil
	}

	member, err := s.opts.Store.GetSeasonMember(r.Context(), store.GetSeasonMemberParams{
		SeasonID: seasonID,
		PersonID: person.ID,
	})
	switch {
	case err == nil:
		v.member = member
		v.isMember = true
	case errors.Is(err, pgx.ErrNoRows):
		// Signed in, not on this year's list. A real and unremarkable state:
		// last year's group, or somebody who has not been added yet.
	default:
		return nil, err
	}

	return v, nil
}

// quotaFor answers "how many picks does this person have left in this season",
// keeping the three answers apart that viewmodel.SubmitQuota exists to keep
// apart.
//
// Known is set by whether GetEffectiveSubmitLimit returned a row, never by
// whether the limit is non-zero. No rows means not a member; a row with 0
// means a member who may vote and may not submit. Collapsing those is the
// COALESCE bug the store layer went to trouble to avoid, and it would be
// reintroduced here by any line that reads "if limit == 0".
func (s *Service) quotaFor(ctx context.Context, seasonID, personID uuid.UUID) (viewmodel.SubmitQuota, error) {
	var q viewmodel.SubmitQuota

	limit, err := s.opts.Store.GetEffectiveSubmitLimit(ctx, store.GetEffectiveSubmitLimitParams{
		SeasonID: seasonID,
		PersonID: personID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Not a member. The zero quota is the right answer, and HasRoom
			// reports false on it.
			return q, nil
		}

		return q, err
	}

	q.Known = true
	q.Limit = int(limit)

	used, err := s.opts.Store.CountPersonMoviesInSeason(ctx, store.CountPersonMoviesInSeasonParams{
		SeasonID:    seasonID,
		SubmittedBy: personID,
	})
	if err != nil {
		return viewmodel.SubmitQuota{}, err
	}
	q.Used = int(used)

	return q, nil
}

// seasonSummary turns a season row into the shape a page renders.
//
// The four windows are nullable in the schema — a season can exist before its
// dates are decided — and the zero time.Time is viewmodel's sentinel for that,
// so an invalid timestamptz becomes a zero time and the page omits the line
// rather than printing a label with nothing after it.
func (s *Service) seasonSummary(season store.Season) viewmodel.SeasonSummary {
	return viewmodel.SeasonSummary{
		Name:          season.Name,
		Year:          int(season.Year),
		State:         viewmodel.SeasonState(season.State),
		SubmitOpensAt: s.at(season.SubmitOpensAt),
		VoteOpensAt:   s.at(season.VoteOpensAt),
		VoteClosesAt:  s.at(season.VoteClosesAt),
		LockedAt:      s.at(season.LockedAt),
	}
}

// at renders a nullable timestamp in the site's timezone, or the zero time.
func (s *Service) at(ts pgtype.Timestamptz) time.Time {
	if !ts.Valid {
		return time.Time{}
	}

	return ts.Time.In(s.opts.Location)
}

// movieCard turns a movie row and its submitter's name into a card.
//
// TrailerEmbedURL is deliberately left blank. Deciding which links can be
// played in place is ticket E5's (nap-0z8), and a half-version of it here
// would be the thing E5 has to unpick. Blank is not a degraded state: the card
// template treats "a link, no embed" as the common case and renders the link,
// which is correct for every trailer URL that is not a YouTube watch page.
func movieCard(m store.Movie, submittedBy string) viewmodel.MovieCard {
	card := viewmodel.MovieCard{
		ID:          m.ID.String(),
		Title:       m.Title,
		Description: m.Description,
		SubmittedBy: submittedBy,
		Hidden:      m.Hidden,
	}
	if m.Year != nil {
		card.Year = int(*m.Year)
	}
	if m.TrailerUrl != nil {
		card.TrailerURL = *m.TrailerUrl
	}

	return card
}

// page builds the shell every page here shares.
func (s *Service) page(r *http.Request, title string) viewmodel.LayoutData {
	return viewmodel.LayoutData{
		Theme:      s.opts.Theme,
		Title:      title,
		Path:       r.URL.Path,
		Origin:     s.opts.Origin,
		Nav:        s.opts.Nav,
		SignInHref: s.opts.SignInHref,
	}
}

// withViewer fills in the shell's account half.
//
// The signed-in half and the signed-out half are mutually exclusive by
// construction: CurrentUser stays nil for a visitor, so a template cannot
// greet an empty name, and SignInHref is dropped once there is somebody to
// greet, so a signed-in member is never offered a second sign-in.
func (s *Service) withViewer(p viewmodel.LayoutData, v *viewer) viewmodel.LayoutData {
	if v == nil {
		return p
	}

	p.CurrentUser = &viewmodel.CurrentUser{
		DisplayName: v.person.DisplayName,
		Email:       v.person.Email,
		IsAdmin:     v.isMember && v.member.IsAdmin,
	}
	p.SignInHref = ""
	p.SignOutHref = signin.LogoutPath

	return p
}

// render writes a component as a complete HTML response.
//
// Status and Content-Type go out before rendering starts, so a template that
// fails halfway leaves a truncated page rather than a second set of headers.
// There is nothing better available at that point — the response has already
// begun — so the error is logged and the connection is left to close.
func (s *Service) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if err := c.Render(r.Context(), w); err != nil {
		s.opts.Logger.Error("render failed", slog.String("path", r.URL.Path), slog.Any("error", err))
	}
}

// serverError renders the site's 500. The underlying error is logged here
// rather than shown, because a visitor cannot act on it.
func (s *Service) serverError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	s.opts.Logger.Error(msg,
		slog.String("path", r.URL.Path),
		slog.Any("error", err),
	)

	p := s.page(r, templates.ServerErrorContent.Heading)
	s.render(w, r, http.StatusInternalServerError, templates.ServerErrorPage(p))
}
