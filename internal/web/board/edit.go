package board

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ERaith/nightofathousandpixels/internal/audit"
	"github.com/ERaith/nightofathousandpixels/internal/signin"
	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// The routes for changing a film you already put up (ticket E3, nap-7hn).
//
// They hang off SubmitPath rather than off the slate because they are the
// submit form doing its other job, and because the slate is public: a URL
// space that is entirely behind the whitelist gate is easier to keep behind it
// than one where two of five routes are.
const (
	// EditPattern is the chi pattern for the edit form and its POST.
	EditPattern = SubmitPath + "/{movieID}/edit"

	// WithdrawPattern is the chi pattern for the withdraw POST. It is POST
	// only: there is no GET here at all, because a GET that hides a film is a
	// link a chat client's preview fetcher can follow.
	WithdrawPattern = SubmitPath + "/{movieID}/withdraw"

	// movieIDParam is the URL parameter both patterns carry.
	movieIDParam = "movieID"
)

// The reasons an edit or a withdrawal is refused.
//
// errWindowClosed, errNotMember and errBarred are shared with the submit path
// deliberately: they are the same three facts about the same season and the
// same membership row, and a second set of sentinels saying the same things is
// how the two paths come to disagree about what a closed window means.
var (
	// errNotYours is somebody else's film. It is its own sentinel rather than
	// a reuse of errNotMember because it is not an authorisation state at all
	// -- the person is a perfectly good member of this season, they are simply
	// not the submitter of this row.
	errNotYours = errors.New("board: that submission belongs to somebody else")

	// errAlreadyWithdrawn is an edit aimed at a film that is already hidden.
	// Withdrawing one twice is harmless and is allowed; editing one is not,
	// because a hidden film is not on the board and "save" would silently put
	// nothing anywhere a person could see.
	errAlreadyWithdrawn = errors.New("board: that film has already been withdrawn")

	// errWrongSeason is a movie id from another year. It is separated from
	// errNotFound so that the log says which of the two happened: one is a
	// stale bookmark, the other is somebody walking ids.
	errWrongSeason = errors.New("board: that film is not in this season")

	// errNotFound is no such movie.
	errNotFound = errors.New("board: no such submission")
)

// editRoutes mounts the three routes this file owns, inside the whitelist
// gate.
//
// The gate is on all three, POSTs included. A gate on the GET alone would
// refuse to show the form and then accept the write, which is the failure this
// codebase has a comment about on every route block for a reason.
func (s *Service) editRoutes(r chi.Router) {
	r.With(s.opts.RequireMember).Group(func(r chi.Router) {
		r.Get(EditPattern, s.handleEditForm)
		r.Post(EditPattern, s.handleEditPost)
		r.Post(WithdrawPattern, s.handleWithdrawPost)
	})
}

// EditHref is the URL of the edit page for one movie.
//
// It exists so that nothing builds this path by string concatenation at a call
// site: the pattern above and the link on the card are the same route, and a
// helper is what makes that a compile-time fact rather than a convention.
func EditHref(movieID uuid.UUID) string {
	return SubmitPath + "/" + url.PathEscape(movieID.String()) + "/edit"
}

// WithdrawHref is the URL the withdraw button posts to.
func WithdrawHref(movieID uuid.UUID) string {
	return SubmitPath + "/" + url.PathEscape(movieID.String()) + "/withdraw"
}

// handleEditForm renders the submit form filled in with an existing film.
func (s *Service) handleEditForm(w http.ResponseWriter, r *http.Request) {
	v := signin.MustCurrent(r.Context())

	movie, err := s.ownMovie(r, v)
	if err != nil {
		s.refuseEdit(w, r, v, uuid.Nil, err)

		return
	}

	page, err := s.editPage(r, v, movie, formFromMovie(movie))
	if err != nil {
		s.serverError(w, r, "edit: build page", err)

		return
	}

	s.render(w, r, http.StatusOK, templates.SubmitPage(page))
}

// handleEditPost validates a change and writes it.
//
// It is handleSubmitPost's shape on purpose -- parse, validate, write in a
// transaction, redirect -- because the two are the same operation with a
// different verb, and a rejected edit has to come back showing what was typed
// in exactly the way a rejected submission does.
func (s *Service) handleEditPost(w http.ResponseWriter, r *http.Request) {
	v := signin.MustCurrent(r.Context())

	movie, err := s.ownMovie(r, v)
	if err != nil {
		s.refuseEdit(w, r, v, uuid.Nil, err)

		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.opts.Logger.Warn("edit: unreadable form",
			slog.String("person_id", v.Person.ID.String()),
			slog.String("movie_id", movie.ID.String()),
			slog.Any("error", err),
		)
		s.badForm(w, r)

		return
	}

	form := viewmodel.SubmitForm{
		Title:       r.PostFormValue(viewmodel.FieldTitle),
		Year:        r.PostFormValue(viewmodel.FieldYear),
		TrailerURL:  r.PostFormValue(viewmodel.FieldTrailerURL),
		Description: r.PostFormValue(viewmodel.FieldDescription),
	}

	d, errs := validate(form)
	if errs.Any() {
		form.Errors = errs
		page, err := s.editPage(r, v, movie, form)
		if err != nil {
			s.serverError(w, r, "edit: build page", err)

			return
		}
		s.render(w, r, http.StatusUnprocessableEntity, templates.SubmitPage(page))

		return
	}

	updated, err := s.updateSubmission(r.Context(), v, movie.ID, d)
	if err != nil {
		s.refuseEdit(w, r, v, movie.ID, err)

		return
	}

	s.opts.Logger.Info("movie edited",
		slog.String("movie_id", updated.ID.String()),
		slog.String("person_id", v.Person.ID.String()),
		slog.Int("season", int(v.Season.Year)),
		slog.String("title", updated.Title),
	)

	http.Redirect(w, r, SlatePath+"?"+addedParam+"="+url.QueryEscape(updated.ID.String()), http.StatusSeeOther)
}

// handleWithdrawPost takes a film off the board.
func (s *Service) handleWithdrawPost(w http.ResponseWriter, r *http.Request) {
	v := signin.MustCurrent(r.Context())

	movieID, err := movieIDFrom(r)
	if err != nil {
		s.refuseEdit(w, r, v, uuid.Nil, err)

		return
	}

	withdrawn, err := s.withdrawSubmission(r.Context(), v, movieID)
	if err != nil {
		s.refuseEdit(w, r, v, movieID, err)

		return
	}

	s.opts.Logger.Info("movie withdrawn",
		slog.String("movie_id", withdrawn.ID.String()),
		slog.String("person_id", v.Person.ID.String()),
		slog.Int("season", int(v.Season.Year)),
		slog.String("title", withdrawn.Title),
	)

	// Back to the submit page rather than the slate. The film is not on the
	// slate any more, so a redirect there would be a page with nothing to say
	// about what just happened -- whereas the submit page shows the pick that
	// came back, which is the thing the person wants to see.
	http.Redirect(w, r, SubmitPath, http.StatusSeeOther)
}

// ownMovie loads the movie named in the URL and refuses it unless it belongs
// to this viewer, in this season, and is still on the board.
//
// This is the READ-side answer only. Every one of these checks is made again
// inside the write transaction, because this one is made against a row that
// can change between here and the commit -- and because the rule that matters
// is the one enforced where the write happens.
func (s *Service) ownMovie(r *http.Request, v signin.Viewer) (store.Movie, error) {
	var zero store.Movie

	movieID, err := movieIDFrom(r)
	if err != nil {
		return zero, err
	}

	movie, err := s.opts.Store.GetMovie(r.Context(), movieID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, errNotFound
		}

		return zero, fmt.Errorf("load movie: %w", err)
	}

	if err := checkOwnership(movie, v.Season.ID, v.Person.ID); err != nil {
		return zero, err
	}
	if movie.Hidden {
		return zero, errAlreadyWithdrawn
	}

	return movie, nil
}

// checkOwnership is rule 1 and the season half of rule 2, in one place so the
// read path and the write transaction cannot come to different conclusions.
//
// The season check is not redundant next to the submitter check. movie.id is a
// uuid and is unique across every year, so a film of yours from 2025 passes the
// submitter test and is still not yours to edit now: the 2025 season is locked
// and its slate is the archive.
func checkOwnership(movie store.Movie, seasonID, personID uuid.UUID) error {
	if movie.SeasonID != seasonID {
		return errWrongSeason
	}
	if movie.SubmittedBy != personID {
		return errNotYours
	}

	return nil
}

// movieIDFrom parses the {movieID} URL parameter.
func movieIDFrom(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, movieIDParam))
	id, err := uuid.Parse(raw)
	if err != nil {
		// Not a uuid at all: a hand-edited URL or a crawler. There is no such
		// film, and saying anything more specific would be describing the id
		// format to somebody guessing at it.
		return uuid.Nil, errNotFound
	}

	return id, nil
}

// formFromMovie fills the form with what is currently stored.
//
// The year comes back as the text of the number rather than as a number,
// because viewmodel.SubmitForm holds what a person would have typed -- see its
// comment. A nil year is a blank box, which is what "we do not know the year"
// looked like when it was submitted.
func formFromMovie(m store.Movie) viewmodel.SubmitForm {
	form := viewmodel.SubmitForm{
		Title:       m.Title,
		Description: m.Description,
	}
	if m.Year != nil {
		form.Year = fmt.Sprintf("%d", *m.Year)
	}
	if m.TrailerUrl != nil {
		form.TrailerURL = *m.TrailerUrl
	}

	return form
}

// editPage gathers what the edit form needs.
//
// It is submitPage's sibling and differs in exactly two ways, both of which
// matter:
//
//   - CanSubmit here is the season window ALONE. The quota is about putting a
//     new film up; somebody at 2 of 2 is exactly the person most likely to
//     want to change one, and gating an edit on having room would lock the
//     form for everybody who has finished submitting.
//   - Existing is left empty. The page is about one film and shows it in the
//     form; repeating the person's whole shelf underneath is noise, and one
//     of the cards would be the film being edited, with a link back to the
//     page it is already on.
func (s *Service) editPage(r *http.Request, v signin.Viewer, movie store.Movie, form viewmodel.SubmitForm) (viewmodel.SubmitPage, error) {
	quota, err := s.quotaFor(r.Context(), v.Season.ID, v.Person.ID)
	if err != nil {
		return viewmodel.SubmitPage{}, fmt.Errorf("load quota: %w", err)
	}

	layout := s.page(r, "Change your pick")
	layout.CurrentUser = &viewmodel.CurrentUser{
		DisplayName: v.Person.DisplayName,
		Email:       v.Person.Email,
		IsAdmin:     v.IsAdmin(),
	}
	layout.SignInHref = ""
	layout.SignOutHref = signin.LogoutPath

	page := viewmodel.SubmitPage{
		Layout:     layout,
		Season:     s.seasonSummary(v.Season),
		Form:       form,
		Quota:      quota,
		Action:     EditHref(movie.ID),
		CancelHref: SubmitPath,

		HeadingKey: viewmodel.KeyEditHeading,
		LeadKey:    viewmodel.KeyEditLead,
		ButtonKey:  viewmodel.KeyEditButton,

		WithdrawAction: WithdrawHref(movie.ID),
	}
	page.CanSubmit = v.Season.State == stateSubmitting

	return page, nil
}

// refuseEdit renders the right refusal for an edit or a withdrawal.
//
// The shapes are deliberately different from each other:
//
//	not found / not yours / wrong season  -> 404, the site's own page
//	window closed                         -> 409 and a page that says so
//	not a member / barred                 -> 403, same page
//	anything else                         -> 500
//
// 404 for "not yours" is not a slip. Telling somebody that a film they cannot
// touch exists, and that it belongs to somebody else, is an answer they have
// no use for and a probe they can repeat -- and internal/signin already takes
// the same line for a non-admin asking for an admin page.
func (s *Service) refuseEdit(w http.ResponseWriter, r *http.Request, v signin.Viewer, movieID uuid.UUID, cause error) {
	switch {
	case errors.Is(cause, errNotFound), errors.Is(cause, errWrongSeason), errors.Is(cause, errNotYours):
		if errors.Is(cause, errNotYours) {
			// Worth a line: it is either a stale link somebody shared, or
			// somebody trying ids. Either way the refusal happened server-side,
			// which is the thing to be able to prove later.
			s.opts.Logger.Warn("edit refused: not the submitter",
				slog.String("person_id", v.Person.ID.String()),
				slog.String("movie_id", movieID.String()),
				slog.Int("season", int(v.Season.Year)),
			)
		}
		s.notFound(w, r, v)

		return

	case errors.Is(cause, errAlreadyWithdrawn):
		// Not an error the person caused and not one they can fix by trying
		// again: the film is off the board, which is what they asked for. A
		// 404 would be a lie about a row that is right there; the withdrawn
		// state IS "there is nothing here to edit".
		s.notFound(w, r, v)

		return

	case errors.Is(cause, errWindowClosed):
		s.opts.Logger.Info("edit refused: window closed",
			slog.String("person_id", v.Person.ID.String()),
			slog.Int("season", int(v.Season.Year)),
		)
		p := s.page(r, "The board is shut")
		p.CurrentUser = &viewmodel.CurrentUser{
			DisplayName: v.Person.DisplayName,
			Email:       v.Person.Email,
			IsAdmin:     v.IsAdmin(),
		}
		p.SignInHref = ""
		p.SignOutHref = signin.LogoutPath
		s.render(w, r, http.StatusConflict, templates.ErrorPage(p, templates.ErrorContent{
			Status:     http.StatusConflict,
			Heading:    viewmodel.BaseCopy[viewmodel.KeyEditClosedHeading],
			Body:       viewmodel.BaseCopy[viewmodel.KeyEditClosedBody],
			HeadingKey: viewmodel.KeyEditClosedHeading,
			BodyKey:    viewmodel.KeyEditClosedBody,
		}))

		return

	case errors.Is(cause, errAlreadyUp):
		// The one refusal here that is not about the person at all, so it is
		// the one that keeps what they typed: they have done nothing wrong and
		// there is a real form to come back to. 409, because the situation
		// changed rather than permission being absent.
		s.opts.Logger.Info("edit refused: that film is already on the board",
			slog.String("person_id", v.Person.ID.String()),
			slog.String("movie_id", movieID.String()),
			slog.Int("season", int(v.Season.Year)),
		)
		s.rejectEdit(w, r, v, movieID, http.StatusConflict)

		return

	case errors.Is(cause, errNotMember), errors.Is(cause, errBarred):
		s.opts.Logger.Warn("edit refused",
			slog.String("person_id", v.Person.ID.String()),
			slog.String("email", v.Person.Email),
			slog.Int("season", int(v.Season.Year)),
			slog.String("reason", cause.Error()),
		)
		s.notFound(w, r, v)

		return

	default:
		s.serverError(w, r, "edit: write", cause)
	}
}

// isAlreadyUp reports whether err is the partial unique index on
// (season_id, tmdb_id) refusing a second live copy of one film.
//
// It is a function rather than an inline errors.As so that the classification
// has one definition and can be tested directly -- which matters here more
// than usual, because the condition is currently UNREACHABLE through
// updateSubmission (see there) and so cannot be exercised end to end.
func isAlreadyUp(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

// rejectEdit re-renders the edit form with the film's stored values and a
// message against the title, for a refusal the person can act on.
//
// It is separate from the other branches of refuseEdit because it is the only
// one where there is still a form to come back to: every other refusal there
// means the page itself is gone.
func (s *Service) rejectEdit(w http.ResponseWriter, r *http.Request, v signin.Viewer, movieID uuid.UUID, status int) {
	movie, err := s.opts.Store.GetMovie(r.Context(), movieID)
	if err != nil {
		s.serverError(w, r, "edit: reload for refusal", err)

		return
	}

	form := formFromMovie(movie)
	form.Errors.Add(viewmodel.FieldTitle, alreadyUpMessage)

	page, err := s.editPage(r, v, movie, form)
	if err != nil {
		s.serverError(w, r, "edit: build refusal page", err)

		return
	}

	s.render(w, r, status, templates.SubmitPage(page))
}

// notFound renders the site's 404 for a viewer the handler already knows, so
// the header still greets them rather than offering a second sign-in.
func (s *Service) notFound(w http.ResponseWriter, r *http.Request, v signin.Viewer) {
	p := s.page(r, templates.NotFoundContent.Heading)
	p.CurrentUser = &viewmodel.CurrentUser{
		DisplayName: v.Person.DisplayName,
		Email:       v.Person.Email,
		IsAdmin:     v.IsAdmin(),
	}
	p.SignInHref = ""
	p.SignOutHref = signin.LogoutPath

	s.render(w, r, http.StatusNotFound, templates.NotFoundPage(p))
}

// badForm is the response to a body that could not be parsed at all.
func (s *Service) badForm(w http.ResponseWriter, r *http.Request) {
	p := s.page(r, "That didn't go through")
	s.render(w, r, http.StatusBadRequest, templates.ErrorPage(p, templates.ErrorContent{
		Status:  http.StatusBadRequest,
		Heading: "That didn't go through",
		Body: "The form came back in a shape the server could not read, which usually means " +
			"it was interrupted or it was very large. Go back to the board and try again.",
	}))
}

// updateSubmission is the edit write.
//
// It establishes the same things createSubmission does, in the same order and
// inside one transaction, and none of them is taken on trust from the page
// that was rendered a minute ago:
//
//  1. The season is still open -- the window gates the WRITE, not the button.
//  2. The person is still a member of THIS season. movie has no foreign key to
//     season_member, so this is the check the schema does not have.
//  3. The row is still theirs, still in this season and still on the board.
//  4. The update goes in, and the audit row goes in with it.
//
// The errAlreadyUp catch on that update is for a case this function cannot
// currently reach, and that is deliberate rather than an oversight. tmdb_id is
// carried over from the stored row, so the UPDATE rewrites the value it
// already holds and cannot collide with itself. It becomes reachable the
// moment the edit form can change tmdb_id -- which is nap-eie's TMDB search
// arriving on this form -- and at that point an uncaught unique violation is a
// 500, appearing in a merge that neither branch's author wrote. Two lines now
// is cheaper than that.
//
// The membership row is locked for the same reason the submit path locks it,
// even though an edit changes no count: it serialises this transaction against
// that person's own concurrent submission, so a second tab cannot read a count
// that this one is in the middle of invalidating. One lock per person per
// season, so nobody waits on anybody else.
func (s *Service) updateSubmission(ctx context.Context, v signin.Viewer, movieID uuid.UUID, d draft) (store.Movie, error) {
	var zero store.Movie

	return s.inOwnedTx(ctx, v, movieID, func(q *store.Queries, movie store.Movie) (store.Movie, error) {
		if movie.Hidden {
			return zero, errAlreadyWithdrawn
		}

		// TmdbID comes from the row re-read inside this transaction, never
		// from the form. UpdateMovie sets the column unconditionally, so
		// passing nil here would silently clear a real id -- and because
		// movie_season_tmdb_unique_idx is partial on tmdb_id IS NOT NULL,
		// clearing it does not fail, it just quietly stops that film being
		// deduplicated for the rest of the season.
		updated, err := q.UpdateMovie(ctx, store.UpdateMovieParams{
			ID:          movie.ID,
			Title:       d.title,
			Year:        d.year,
			TmdbID:      movie.TmdbID,
			TrailerUrl:  d.trailerURL,
			Description: d.description,
		})
		if err != nil {
			if isAlreadyUp(err) {
				return zero, errAlreadyUp
			}

			return zero, fmt.Errorf("update movie: %w", err)
		}

		err = audit.Record(ctx, q, v.Person.Email, audit.ActionMovieEdited, movie.ID.String(), map[string]any{
			"season_id":  v.Season.ID.String(),
			"season":     v.Season.Year,
			"person_id":  v.Person.ID.String(),
			"title_from": movie.Title,
			"title_to":   updated.Title,
		})
		if err != nil {
			return zero, err
		}

		return updated, nil
	})
}

// withdrawSubmission is the soft delete, and it is rules 3, 4 and 5 together.
//
// hidden = true, never DELETE. Migration 00005's composite foreign key from
// ballot_entry to movie(season_id, id) exists precisely so that a film
// somebody has already ranked cannot be removed out from under their ballot;
// 00008 makes it deferred, so a hard delete now fails at COMMIT rather than at
// the statement, which is a worse place to discover it. Setting hidden is not
// a way around that constraint, it is the operation the constraint was put
// there to force.
//
// The pick comes back with no arithmetic anywhere: CountPersonMoviesInSeason
// counts `WHERE NOT hidden`, so the row stops counting the moment this commits
// and the quota the next page load reads is already right. Nothing here
// decrements anything, which is the only version of rule 4 that cannot drift
// out of step with the cap the submit path enforces.
//
// Withdrawing an already-hidden film succeeds and changes nothing. Two taps on
// a slow connection is the ordinary way that happens, and an error page for
// "the thing you wanted is already true" helps nobody -- so the second one is
// a no-op with an audit row, not a refusal.
func (s *Service) withdrawSubmission(ctx context.Context, v signin.Viewer, movieID uuid.UUID) (store.Movie, error) {
	var zero store.Movie

	return s.inOwnedTx(ctx, v, movieID, func(q *store.Queries, movie store.Movie) (store.Movie, error) {
		if movie.Hidden {
			return movie, nil
		}

		hidden, err := q.SetMovieHidden(ctx, store.SetMovieHiddenParams{
			ID:     movie.ID,
			Hidden: true,
		})
		if err != nil {
			return zero, fmt.Errorf("hide movie: %w", err)
		}

		err = audit.Record(ctx, q, v.Person.Email, audit.ActionMovieWithdrawn, movie.ID.String(), map[string]any{
			"season_id": v.Season.ID.String(),
			"season":    v.Season.Year,
			"person_id": v.Person.ID.String(),
			"title":     movie.Title,
		})
		if err != nil {
			return zero, err
		}

		return hidden, nil
	})
}

// inOwnedTx runs write inside a transaction that has already established the
// season window, the membership row (locked) and the ownership of the film.
//
// It exists because the edit write and the withdraw write need exactly the
// same five preconditions and differ only in the statement at the end. Written
// out twice, one of the two copies eventually loses the season check or the
// lock, and the one that loses it is the one nobody wrote a test for.
//
// The order is the order createSubmission uses, and it is not arbitrary: the
// season is read first because a closed window overrules everything else, and
// the membership row is locked before the movie is read so that the movie is
// read under the lock rather than before it.
func (s *Service) inOwnedTx(
	ctx context.Context,
	v signin.Viewer,
	movieID uuid.UUID,
	write func(q *store.Queries, movie store.Movie) (store.Movie, error),
) (store.Movie, error) {
	var zero store.Movie

	tx, err := s.opts.DB.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("begin: %w", err)
	}
	// Safe after a successful Commit: pgx makes Rollback on a finished
	// transaction a no-op returning ErrTxClosed, so this is one unconditional
	// cleanup with no flag to get wrong.
	defer func() { _ = tx.Rollback(ctx) }()

	q := store.New(tx)

	// Re-read rather than trusting the season the gate loaded: it was read
	// before the form was rendered, and an admin can close the window in
	// between.
	season, err := q.GetSeason(ctx, v.Season.ID)
	if err != nil {
		return zero, fmt.Errorf("load season: %w", err)
	}
	if season.State != stateSubmitting {
		return zero, fmt.Errorf("%w: state %q", errWindowClosed, season.State)
	}

	// The membership row, locked. No rows means not on this season's whitelist
	// -- the refusal the database cannot make, because movie references season
	// and person and not season_member.
	limit, err := q.LockSubmitLimitForUpdate(ctx, store.LockSubmitLimitForUpdateParams{
		SeasonID: v.Season.ID,
		PersonID: v.Person.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, errNotMember
		}

		return zero, fmt.Errorf("lock submit limit: %w", err)
	}
	// An explicit 0 is "may vote, may not submit". Somebody barred from
	// submitting has no business rewriting a film that is on the board, even
	// one they put there before the override was set.
	if limit <= 0 {
		return zero, errBarred
	}

	movie, err := q.GetMovie(ctx, movieID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, errNotFound
		}

		return zero, fmt.Errorf("load movie: %w", err)
	}
	if err := checkOwnership(movie, v.Season.ID, v.Person.ID); err != nil {
		return zero, err
	}

	result, err := write(q, movie)
	if err != nil {
		return zero, err
	}

	// The commit is checked. Migration 00008 makes two constraints DEFERRABLE
	// INITIALLY DEFERRED, so this codebase contains writes whose integrity
	// errors arrive here rather than at the statement -- and a handler that
	// treats a clean statement as a successful write is wrong in a way that
	// only shows up once somebody defers a constraint on this table.
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit: %w", err)
	}

	return result, nil
}
