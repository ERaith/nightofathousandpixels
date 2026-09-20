package board

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/signin"
	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// Limits on what the form will accept.
//
// The database constrains almost none of this: movie.title is text with only a
// not-blank CHECK, and description is text with none at all. Without a cap
// here, a single POST can put a megabyte of title into a row that every future
// page load then renders. The numbers are chosen to be larger than any real
// answer and smaller than an attack.
const (
	// maxFormBytes bounds the whole request body before it is parsed.
	// http.Request.ParseForm reads it all into memory, so this is the only
	// limit that exists before the allocation happens.
	maxFormBytes = 64 << 10

	maxTitleLen       = 200
	maxTrailerURLLen  = 2000
	maxDescriptionLen = 2000
)

// The range the schema allows for a release year, from movie_year_range in
// migration 00004. Duplicated here so the person is told what is wrong in
// words instead of being handed a constraint violation.
const (
	minYear = 1888
	maxYear = 2200
)

// The reasons a submission is refused after the form itself was fine.
//
// They are sentinels rather than messages because none of them is shown as
// text: each one re-renders the submit page with CanSubmit false, and the
// template's own submitBlockedCopy picks the wording from the season state and
// the quota. There is exactly one set of words for "that is both your picks"
// on this site, and it is not in this file.
var (
	// errNotMember is the one that matters (ticket E7, nap-90j). movie has
	// foreign keys to season and to person but NOT to season_member, so the
	// database will accept a submission from somebody who is not on the
	// season's whitelist. ballot_entry routes through season_member and is
	// structurally safe; movie is not. This is the check that stands in for
	// the constraint that is not there.
	errNotMember = errors.New("board: not a member of this season")

	// errWindowClosed is a submission outside the submitting state.
	errWindowClosed = errors.New("board: the season is not accepting submissions")

	// errBarred is a member whose effective limit is an explicit 0: may vote,
	// may not submit.
	errBarred = errors.New("board: this member may not submit")

	// errAtLimit is a member who has used every pick.
	errAtLimit = errors.New("board: no picks left")
)

// handleSubmitForm renders the form (ticket E1, nap-ibx).
func (s *Service) handleSubmitForm(w http.ResponseWriter, r *http.Request) {
	v := signin.MustCurrent(r.Context())

	page, err := s.submitPage(r, v, viewmodel.SubmitForm{})
	if err != nil {
		s.serverError(w, r, "submit: build page", err)

		return
	}

	s.render(w, r, http.StatusOK, templates.SubmitPage(page))
}

// handleSubmitPost validates a submission and writes it.
//
// The shape is POST-redirect-GET: a success redirects to the slate, so a
// refresh re-runs the GET and cannot put the same film up twice. A failure
// re-renders this page in place, carrying back exactly what was typed.
func (s *Service) handleSubmitPost(w http.ResponseWriter, r *http.Request) {
	v := signin.MustCurrent(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		// An unparseable or oversized body. There is nothing to redisplay,
		// because there is nothing that parsed.
		s.opts.Logger.Warn("submit: unreadable form",
			slog.String("person_id", v.Person.ID.String()),
			slog.Any("error", err),
		)

		p := s.page(r, "That didn't go through")
		s.render(w, r, http.StatusBadRequest, templates.ErrorPage(p, templates.ErrorContent{
			Status:  http.StatusBadRequest,
			Heading: "That didn't go through",
			Body: "The form came back in a shape the server could not read, which usually means " +
				"it was interrupted or it was very large. Go back to the board and try again.",
		}))

		return
	}

	// The raw strings, untrimmed, because this is also what goes back in the
	// boxes if anything is wrong. Trimming happens in validation, against a
	// copy. See viewmodel.SubmitForm: the typed text is what the person gets
	// back, never a parsed or tidied version of it.
	form := viewmodel.SubmitForm{
		Title:       r.PostFormValue(viewmodel.FieldTitle),
		Year:        r.PostFormValue(viewmodel.FieldYear),
		TrailerURL:  r.PostFormValue(viewmodel.FieldTrailerURL),
		Description: r.PostFormValue(viewmodel.FieldDescription),
	}

	draft, errs := validate(form)
	if errs.Any() {
		form.Errors = errs
		page, err := s.submitPage(r, v, form)
		if err != nil {
			s.serverError(w, r, "submit: build page", err)

			return
		}

		// 422 rather than 200: the request was well-formed and the server
		// understood it and would not act on it, which is what the code is
		// for. A browser renders the body either way, so nothing about the
		// page depends on this — it is what a log or a test sees.
		s.render(w, r, http.StatusUnprocessableEntity, templates.SubmitPage(page))

		return
	}

	movie, err := s.createSubmission(r.Context(), v.Season.ID, v.Person.ID, draft)
	if err != nil {
		s.refuse(w, r, v, err)

		return
	}

	s.opts.Logger.Info("movie submitted",
		slog.String("movie_id", movie.ID.String()),
		slog.String("person_id", v.Person.ID.String()),
		slog.Int("season", int(v.Season.Year)),
		slog.String("title", movie.Title),
	)

	http.Redirect(w, r, SlatePath+"?"+addedParam+"="+url.QueryEscape(movie.ID.String()), http.StatusSeeOther)
}

// refuse re-renders the submit page for a submission the transaction turned
// down, or 500s if it was not a refusal at all.
func (s *Service) refuse(w http.ResponseWriter, r *http.Request, v signin.Viewer, cause error) {
	// 403 for "you may not", 409 for "the situation changed under you". Both
	// render the same page; the difference is what a log or a test sees, and
	// a closed window reported as Forbidden would read as an authorisation
	// problem for somebody who simply arrived a day late.
	status := http.StatusConflict

	switch {
	case errors.Is(cause, errNotMember):
		status = http.StatusForbidden
		// Reaching here means the whitelist gate passed this request and the
		// membership row was gone by the time the transaction looked, so it
		// is worth a line: it is either an admin removing somebody at exactly
		// the wrong moment, or the gate has been taken off this route.
		s.opts.Logger.Warn("submission refused: not a member of this season",
			slog.String("person_id", v.Person.ID.String()),
			slog.String("email", v.Person.Email),
			slog.Int("season", int(v.Season.Year)),
		)
	case errors.Is(cause, errBarred):
		// A setting somebody chose, not a situation that changed: this person
		// may vote and may not submit, and will still not be able to tomorrow.
		status = http.StatusForbidden

		s.opts.Logger.Info("submission refused",
			slog.String("person_id", v.Person.ID.String()),
			slog.Int("season", int(v.Season.Year)),
			slog.String("reason", cause.Error()),
		)
	case errors.Is(cause, errWindowClosed), errors.Is(cause, errAtLimit):
		s.opts.Logger.Info("submission refused",
			slog.String("person_id", v.Person.ID.String()),
			slog.Int("season", int(v.Season.Year)),
			slog.String("reason", cause.Error()),
		)
	default:
		s.serverError(w, r, "submit: create submission", cause)

		return
	}

	// Rebuilt from the database rather than from anything this request
	// assumed, so the page states the situation as it now is. The form's
	// values are dropped with the form: every one of these refusals means
	// there is nowhere to put them back.
	page, err := s.submitPage(r, v, viewmodel.SubmitForm{})
	if err != nil {
		s.serverError(w, r, "submit: build refusal page", err)

		return
	}
	page.CanSubmit = false

	s.render(w, r, status, templates.SubmitPage(page))
}

// submitPage gathers everything the form needs: the season, the quota, and
// what this person already has up.
func (s *Service) submitPage(r *http.Request, v signin.Viewer, form viewmodel.SubmitForm) (viewmodel.SubmitPage, error) {
	ctx := r.Context()

	quota, err := s.quotaFor(ctx, v.Season.ID, v.Person.ID)
	if err != nil {
		return viewmodel.SubmitPage{}, fmt.Errorf("load quota: %w", err)
	}

	mine, err := s.opts.Store.ListPersonMoviesForSeason(ctx, store.ListPersonMoviesForSeasonParams{
		SeasonID:    v.Season.ID,
		SubmittedBy: v.Person.ID,
	})
	if err != nil {
		return viewmodel.SubmitPage{}, fmt.Errorf("list own movies: %w", err)
	}

	existing := make([]viewmodel.MovieCard, 0, len(mine))
	for _, m := range mine {
		// The submitter is the person reading the page, so the name is already
		// in hand and there is nothing to join to.
		card := movieCard(m, v.DisplayName())
		// The way back into a film you already put up (ticket E3). It is set
		// only while the window is open, because the page must not offer a
		// control that the write would refuse -- and ListPersonMoviesForSeason
		// has already excluded the hidden rows, so every card here is one
		// there is still something to do with.
		if v.Season.State == stateSubmitting {
			card.EditHref = EditHref(m.ID)
		}
		existing = append(existing, card)
	}

	// Everyone who reaches this page is a member -- the whitelist gate is what
	// they came through -- so the member header is not a decision here.
	layout := s.page(r, "Put a movie on the board")
	layout.CurrentUser = &viewmodel.CurrentUser{
		DisplayName: v.Person.DisplayName,
		Email:       v.Person.Email,
		IsAdmin:     v.IsAdmin(),
	}
	layout.SignInHref = ""
	layout.SignOutHref = signin.LogoutPath
	layout.Nav = viewmodel.NavFor(s.opts.Nav, s.opts.MemberNav, true)

	page := viewmodel.SubmitPage{
		Layout:     layout,
		Season:     s.seasonSummary(v.Season),
		Form:       form,
		Quota:      quota,
		Existing:   existing,
		Action:     SubmitPath,
		CancelHref: SlatePath,
	}

	// The same three facts as the slate, in the same order and for the same
	// reason. Membership is not re-asked here because the gate this route is
	// mounted behind has already established it for this season — and the
	// transaction establishes it again before it writes, which is the one that
	// counts.
	page.CanSubmit = v.Season.State == stateSubmitting && quota.HasRoom()

	return page, nil
}

// draft is a validated submission, in the types the database takes. The
// pointers are nullable columns: nil is SQL NULL, which for year and
// trailer_url means "not given" rather than "empty".
type draft struct {
	title       string
	year        *int32
	trailerURL  *string
	description string
}

// validate turns what was typed into either a draft or a set of per-field
// messages.
//
// It never returns both, and it never modifies the form: the values the person
// sees again are the ones they typed, whitespace included. Messages are
// written for the person rather than for a log — "That doesn't look like a
// year" rather than "strconv.Atoi: invalid syntax" — and every one of them
// says what to do instead.
func validate(form viewmodel.SubmitForm) (draft, viewmodel.FieldErrors) {
	var (
		d    draft
		errs viewmodel.FieldErrors
	)

	title := strings.TrimSpace(form.Title)
	switch {
	case title == "":
		errs.Add(viewmodel.FieldTitle, "A film needs a title — it is the one part we cannot guess.")
	case len([]rune(title)) > maxTitleLen:
		errs.Add(viewmodel.FieldTitle, fmt.Sprintf(
			"That title is longer than the board can show. Keep it under %d characters.", maxTitleLen))
	default:
		d.title = title
	}

	// Year, trailer and description are all optional, so a blank one is not an
	// error and does not even reach a parser.
	if year := strings.TrimSpace(form.Year); year != "" {
		switch n, err := strconv.Atoi(year); {
		case err != nil:
			errs.Add(viewmodel.FieldYear, "That doesn't look like a year. Four digits, like 1982 — or leave it blank.")
		case n < minYear || n > maxYear:
			errs.Add(viewmodel.FieldYear, fmt.Sprintf(
				"Years on the board run from %d to %d. Leave it blank if you are not sure.", minYear, maxYear))
		default:
			n32 := int32(n)
			d.year = &n32
		}
	}

	if trailer := strings.TrimSpace(form.TrailerURL); trailer != "" {
		switch {
		case len(trailer) > maxTrailerURLLen:
			errs.Add(viewmodel.FieldTrailerURL, "That link is too long to store. Try the short form of it.")
		case !validTrailerURL(trailer):
			errs.Add(viewmodel.FieldTrailerURL, "A trailer link has to be a web address starting with http:// or https://.")
		case !Embeddable(trailer):
			// Ticket E5 (nap-0z8). Anything that is not a video the board can
			// play used to be accepted and then sat on a card as a link
			// nobody checked until movie night. The message names the two
			// shapes that work rather than saying "invalid", because the
			// mistake is almost always a channel page, a search result or a
			// link that lost its ?v= on the way through a chat client.
			errs.Add(viewmodel.FieldTrailerURL,
				"That isn't a video link we can play. Use the YouTube or Vimeo address for the "+
					"trailer itself — like https://www.youtube.com/watch?v=4sDyy2Ndm5k — or leave it blank.")
		default:
			// Stored exactly as typed. The embed is derived at render time;
			// see TrailerEmbed.
			d.trailerURL = &trailer
		}
	}

	// Not trimmed for storage the way the others are: leading whitespace in a
	// paragraph somebody wrote is theirs, and a description is prose rather
	// than an identifier. Only the length is checked.
	if len([]rune(form.Description)) > maxDescriptionLen {
		errs.Add(viewmodel.FieldDescription, fmt.Sprintf(
			"That is longer than the box will take. Keep it under %d characters.", maxDescriptionLen))
	} else {
		d.description = form.Description
	}

	return d, errs
}

// validTrailerURL reports whether a link is one the schema will accept and a
// browser can follow.
//
// movie_trailer_url_shape only checks `^https?://`, which "http://" on its own
// satisfies. A URL with no host is not a link, it is a string that will render
// as a dead anchor on the board forever, so the host is required here. This is
// also what keeps javascript: and data: off a page that renders the value as
// an href.
func validTrailerURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	return u.Host != ""
}

// createSubmission is the write, and it is the whole of ticket E7 (nap-90j)
// and the cap in one transaction.
//
// Four things are established here, in this order, and none of them is taken
// on trust from the page that was rendered a minute ago:
//
//  1. The season is still open. The window gates writes, not just the UI.
//  2. The person is still a member of THIS season, and what their effective
//     limit is. Both come from one row, and no rows means not a member.
//  3. They still have a pick left.
//  4. The row goes in.
//
// Why a transaction, and why it is not enough on its own: steps 3 and 4 are
// check-then-insert, and at READ COMMITTED two concurrent submissions from one
// person would both read the same count and both insert. The transaction gives
// atomicity; the FOR UPDATE inside LockSubmitLimitForUpdate gives the mutual
// exclusion, by making the second transaction wait on the first person's
// membership row until the first has committed the film it counted against.
// A lock per person per season, so two different people never wait on
// each other.
//
// The membership check is here rather than only in the middleware because the
// schema does not have it. movie references season and person; it does not
// reference season_member, so Postgres will accept a submission from anybody
// with a person row. Checking it inside the transaction, keyed on the same
// season_id that the INSERT uses, is as close to a constraint as this can get:
// there is no window between the check and the write in which the answer could
// change, because the row it checked is locked for the rest of the
// transaction.
func (s *Service) createSubmission(ctx context.Context, seasonID, personID uuid.UUID, d draft) (store.Movie, error) {
	var zero store.Movie

	tx, err := s.opts.DB.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("begin: %w", err)
	}
	// Safe after a successful Commit: pgx makes Rollback on a finished
	// transaction a no-op returning ErrTxClosed, which is exactly what this
	// wants — one unconditional cleanup, no flag to get wrong.
	defer func() { _ = tx.Rollback(ctx) }()

	q := store.New(tx)

	// Re-read rather than trusting the season the gate loaded. It was read
	// before the form was even rendered on a GET, and an admin can close the
	// window in between.
	season, err := q.GetSeason(ctx, seasonID)
	if err != nil {
		return zero, fmt.Errorf("load season: %w", err)
	}
	if season.State != stateSubmitting {
		return zero, fmt.Errorf("%w: state %q", errWindowClosed, season.State)
	}

	limit, err := q.LockSubmitLimitForUpdate(ctx, store.LockSubmitLimitForUpdateParams{
		SeasonID: seasonID,
		PersonID: personID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No membership row: not on this season's whitelist. Distinct from
			// a limit of 0 below, and the refusal the database cannot make.
			return zero, errNotMember
		}

		return zero, fmt.Errorf("lock submit limit: %w", err)
	}

	// An explicit 0 is a setting, not an absent one: this person may vote and
	// may not submit. There is deliberately no fallback to the season default
	// here — the COALESCE is already resolved in the query, and re-resolving
	// it in Go is the bug that query exists to prevent.
	if limit <= 0 {
		return zero, errBarred
	}

	used, err := q.CountPersonMoviesInSeason(ctx, store.CountPersonMoviesInSeasonParams{
		SeasonID:    seasonID,
		SubmittedBy: personID,
	})
	if err != nil {
		return zero, fmt.Errorf("count submissions: %w", err)
	}
	if used >= int64(limit) {
		return zero, fmt.Errorf("%w: %d of %d used", errAtLimit, used, limit)
	}

	movie, err := q.CreateMovie(ctx, store.CreateMovieParams{
		SeasonID:    seasonID,
		SubmittedBy: personID,
		Title:       d.title,
		Year:        d.year,
		TmdbID:      nil,
		TrailerUrl:  d.trailerURL,
		Description: d.description,
	})
	if err != nil {
		return zero, fmt.Errorf("insert movie: %w", err)
	}

	// The commit is checked, and that is not a formality. Migration 00008
	// makes two constraints DEFERRABLE INITIALLY DEFERRED, so this codebase
	// contains writes whose integrity errors arrive here and not at the
	// statement. Those two are on ballot_entry and result rather than on
	// movie, so this particular INSERT would still fail above — but a handler
	// that treats a clean INSERT as a successful write is wrong in a way that
	// only shows up once somebody adds a deferred constraint to this table.
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit: %w", err)
	}

	return movie, nil
}
