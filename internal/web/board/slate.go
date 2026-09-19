package board

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// handleSlate renders the board (ticket E4, nap-1s9).
//
// It is public, and it has to work in four states that are all normal:
//
//	no season at all      before the first season leaves draft
//	a season, no films    the morning the link lands in the group chat
//	signed out            most people, most of the time
//	signed in, not a member   last year's group, or not added yet
//
// None of those is an error and none of them is a 500. In particular
// GetCurrentSeason returns pgx.ErrNoRows when no season has left draft, which
// is a fact about a new install rather than a failure, and the page it
// produces is the empty board.
func (s *Service) handleSlate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var (
		season store.Season
		exists bool
	)

	found, err := s.opts.Store.GetCurrentSeason(ctx)
	switch {
	case err == nil:
		season, exists = found, true
	case errors.Is(err, pgx.ErrNoRows):
		// No season has left draft. The board is empty because there is
		// nothing for it to be full of.
	default:
		s.serverError(w, r, "slate: load current season", err)

		return
	}

	seasonID := uuid.Nil
	if exists {
		seasonID = season.ID
	}

	v, err := s.currentViewer(r, seasonID)
	if err != nil {
		s.serverError(w, r, "slate: resolve viewer", err)

		return
	}

	page := viewmodel.SlatePage{
		Layout: s.withViewer(s.page(r, "The slate"), v),
	}
	if exists {
		page.Season = s.seasonSummary(season)
	}

	if exists {
		rows, err := s.opts.Store.ListVisibleMoviesWithSubmitterForSeason(ctx, season.ID)
		if err != nil {
			s.serverError(w, r, "slate: list movies", err)

			return
		}

		page.Movies = make([]viewmodel.MovieCard, 0, len(rows))
		for _, row := range rows {
			page.Movies = append(page.Movies, movieCard(row.Movie, row.DisplayName))
		}
	}

	// The quota is only asked for when there is both a season and somebody to
	// ask about. For everyone else the zero SubmitQuota is the right answer —
	// Known false, which the template reads as "no quota to describe" and
	// prints nothing rather than "0 picks left".
	if exists && v != nil {
		quota, err := s.quotaFor(ctx, season.ID, v.person.ID)
		if err != nil {
			s.serverError(w, r, "slate: load quota", err)

			return
		}
		page.Quota = quota
	}

	// The one decision on this page, made once and carried as a bool. All
	// three parts have to be true at the same time, and the season window is
	// first because it overrules the other two: a member with two picks left
	// still may not submit in the voting week.
	page.CanSubmit = exists &&
		season.State == stateSubmitting &&
		v != nil &&
		page.Quota.HasRoom()
	if page.CanSubmit {
		page.SubmitHref = SubmitPath
	}

	if flash, ok := s.addedFlash(r, page.Movies); ok {
		page.Layout.Flashes = append(page.Layout.Flashes, flash)
	}

	s.render(w, r, http.StatusOK, templates.SlatePage(page))
}

// addedFlash is the confirmation after a successful submission.
//
// It is built from the row rather than from the query string: the parameter is
// only ever used to look an id up in the list the page has already loaded, and
// the words come from the movie that was found. So a crafted ?added= link
// cannot put text on the page, and a stale one — a bookmarked redirect, or a
// film withdrawn since — simply produces no notice, which is the correct
// outcome rather than a claim about a film that is not on the board.
func (s *Service) addedFlash(r *http.Request, movies []viewmodel.MovieCard) (viewmodel.Flash, bool) {
	id := r.URL.Query().Get(addedParam)
	if id == "" {
		return viewmodel.Flash{}, false
	}

	for _, m := range movies {
		if m.ID == id {
			return viewmodel.Flash{
				Level:   viewmodel.FlashSuccess,
				Message: m.TitleLine() + " is on the board.",
			}, true
		}
	}

	return viewmodel.Flash{}, false
}
