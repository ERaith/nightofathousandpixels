package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ERaith/nightofathousandpixels/internal/audit"
	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// errYearTaken is a second season for a year that already has one.
//
// season_year_key UNIQUE is what refuses it, and catching it here rather than
// pre-checking with a SELECT is deliberate: a check-then-insert would have a
// window between the two in which the other tab commits, and the constraint
// would then produce a 500 on the run where two admins opened the same year at
// the same moment. The constraint is the check; this turns it into a sentence.
var errYearTaken = errors.New("admin: a season already exists for that year")

// uniqueViolation is Postgres's SQLSTATE for a unique constraint violation.
const uniqueViolation = "23505"

// handleSeasonList renders every season, drafts included.
func (s *Service) handleSeasonList(w http.ResponseWriter, r *http.Request) {
	a := mustAdmin(r.Context())

	seasons, err := s.opts.Store.ListSeasons(r.Context())
	if err != nil {
		s.serverError(w, r, "admin: list seasons", err)

		return
	}

	rows := make([]viewmodel.AdminSeasonRow, 0, len(seasons))
	for _, season := range seasons {
		row := viewmodel.AdminSeasonRow{
			Name:              season.Name,
			Year:              int(season.Year),
			State:             viewmodel.SeasonState(season.State),
			SubmitClosesLabel: viewmodel.FormatDeadline(s.at(season.VoteOpensAt)),
			VoteClosesLabel:   viewmodel.FormatDeadline(s.at(season.VoteClosesAt)),
			IsCurrent:         a.hasSeason && season.ID == a.season.ID,
		}
		// No way in for a locked season. Migration 00008 makes locked terminal
		// -- the season_no_unlock_guard trigger refuses any move away from it
		// unless the session sets nap.allow_unlock -- and a deliberate admin
		// unlock is its own ticket (nap-hh3). Offering an edit control that
		// leads to a form whose save would be refused is worse than offering
		// nothing.
		if season.State != string(viewmodel.SeasonLocked) {
			row.EditHref = seasonHref(season.ID)
		}
		rows = append(rows, row)
	}

	page := viewmodel.AdminSeasonsPage{
		Layout:      s.page(r, "Seasons", &a),
		Seasons:     rows,
		NewHref:     NewSeasonPath,
		IsBootstrap: a.viaBootstrap,
	}

	s.render(w, r, http.StatusOK, SeasonListPage(page))
}

// handleNewSeasonForm renders the create screen.
func (s *Service) handleNewSeasonForm(w http.ResponseWriter, r *http.Request) {
	a := mustAdmin(r.Context())

	// The existing seasons, only to pick a year that is free. A failure here
	// is not worth refusing the page over: the form still works, the default
	// year is just this one, and season_year_key turns a collision into a
	// sentence rather than a 500.
	taken, err := s.opts.Store.ListSeasons(r.Context())
	if err != nil {
		s.opts.Logger.Warn("admin: could not read existing seasons for the default year",
			slog.Any("error", err))
		taken = nil
	}

	s.render(w, r, http.StatusOK, SeasonFormPage(s.newSeasonPage(r, &a, s.blankSeasonForm(taken))))
}

// blankSeasonForm is the create screen's starting state.
//
// The defaults are the answers that are right almost every time -- the next
// year that has no season, opening now, two picks each -- so that the form is
// two dates and a nod rather than seven decisions. They are values in the
// boxes and not hidden assumptions: every one is visible and editable.
//
// The year walks forward from this one until it finds a free slot, because the
// two cases are "it is September and this year has not been opened yet" and
// "we are planning next year in advance", and offering a year that already
// exists means the first thing the form does is refuse itself.
func (s *Service) blankSeasonForm(taken []store.Season) viewmodel.AdminSeasonForm {
	used := make(map[int32]bool, len(taken))
	for _, season := range taken {
		used[season.Year] = true
	}

	year := int32(s.opts.Now().In(s.opts.Location).Year())
	for used[year] && year < maxYear {
		year++
	}

	return viewmodel.AdminSeasonForm{
		Year:        fmt.Sprintf("%d", year),
		Name:        fmt.Sprintf("The %d Season", year),
		SubmitLimit: "2",
		State:       stateSubmitting,
	}
}

// handleCreateSeason validates a new season and writes it.
func (s *Service) handleCreateSeason(w http.ResponseWriter, r *http.Request) {
	a := mustAdmin(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.opts.Logger.Warn("admin: unreadable season form",
			slog.String("email", a.person.Email), slog.Any("error", err))
		s.render(w, r, http.StatusBadRequest, SeasonFormPage(s.newSeasonPage(r, &a, s.blankSeasonForm(nil))))

		return
	}

	form := parseSeasonForm(r.PostFormValue)

	draft, errs := validateSeason(form, s.opts.Location, s.opts.Now(), true)
	if errs.Any() {
		form.Errors = errs
		// 422: the request was well-formed, the server understood it, and it
		// will not act on it. A browser renders the body either way, so
		// nothing on the page depends on this -- it is what a log or a test
		// sees.
		s.render(w, r, http.StatusUnprocessableEntity, SeasonFormPage(s.newSeasonPage(r, &a, form)))

		return
	}

	season, err := s.createSeason(r.Context(), a, draft)
	if err != nil {
		if errors.Is(err, errYearTaken) {
			form.Errors.Add(viewmodel.FieldSeasonYear,
				fmt.Sprintf("There is already a %s season. Open the one that exists rather than a second one.", form.Year))
			s.render(w, r, http.StatusConflict, SeasonFormPage(s.newSeasonPage(r, &a, form)))

			return
		}
		s.serverError(w, r, "admin: create season", err)

		return
	}

	s.opts.Logger.Info("season created",
		slog.String("season_id", season.ID.String()),
		slog.Int("year", int(season.Year)),
		slog.String("state", season.State),
		slog.String("by", a.person.Email),
		slog.Bool("via_bootstrap", a.viaBootstrap),
	)

	// POST-redirect-GET, so a refresh cannot try to create the season twice --
	// and if somebody does resubmit the POST, season_year_key refuses it with
	// the sentence above rather than with a second season.
	http.Redirect(w, r, SeasonsPath, http.StatusSeeOther)
}

// handleEditSeasonForm renders the re-date screen for one season.
func (s *Service) handleEditSeasonForm(w http.ResponseWriter, r *http.Request) {
	a := mustAdmin(r.Context())

	season, ok := s.loadSeason(w, r, &a)
	if !ok {
		return
	}

	s.render(w, r, http.StatusOK, SeasonFormPage(s.editSeasonPage(r, &a, season, s.formFromSeason(season))))
}

// handleUpdateSeason validates a re-dating and writes it.
func (s *Service) handleUpdateSeason(w http.ResponseWriter, r *http.Request) {
	a := mustAdmin(r.Context())

	season, ok := s.loadSeason(w, r, &a)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.opts.Logger.Warn("admin: unreadable season form",
			slog.String("email", a.person.Email), slog.Any("error", err))
		s.render(w, r, http.StatusBadRequest,
			SeasonFormPage(s.editSeasonPage(r, &a, season, s.formFromSeason(season))))

		return
	}

	form := parseSeasonForm(r.PostFormValue)
	// The year and the state are not editable here, so whatever the request
	// carried for them is discarded and the stored values are put back before
	// validation. That is not belt and braces: the controls are rendered
	// disabled, a disabled control posts nothing at all, and a hand-built POST
	// could otherwise rename the year out from under every link that names it.
	form.Year = fmt.Sprintf("%d", season.Year)
	form.State = season.State

	// requireFuture is false: a season being edited in the voting week has a
	// submission deadline that is already behind it, and refusing to let an
	// admin fix the voting date because of it would make this screen useless
	// exactly when it is used. See validateSeason.
	draft, errs := validateSeason(form, s.opts.Location, s.opts.Now(), false)
	if errs.Any() {
		form.Errors = errs
		s.render(w, r, http.StatusUnprocessableEntity,
			SeasonFormPage(s.editSeasonPage(r, &a, season, form)))

		return
	}

	updated, err := s.updateSeason(r.Context(), a, season, draft)
	if err != nil {
		s.serverError(w, r, "admin: update season", err)

		return
	}

	s.opts.Logger.Info("season updated",
		slog.String("season_id", updated.ID.String()),
		slog.Int("year", int(updated.Year)),
		slog.String("by", a.person.Email),
	)

	http.Redirect(w, r, SeasonsPath, http.StatusSeeOther)
}

// loadSeason reads the season named in the URL, rendering the refusal itself
// and reporting whether the caller should carry on.
//
// A locked season is not found here, not merely uneditable. It is the archive:
// migration 00008 refuses to move it out of locked, and there is no edit for
// this screen to offer that would survive the save.
func (s *Service) loadSeason(w http.ResponseWriter, r *http.Request, a *admin) (store.Season, bool) {
	var zero store.Season

	id, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, seasonIDParam)))
	if err != nil {
		s.notFound(w, r, a)

		return zero, false
	}

	season, err := s.opts.Store.GetSeason(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r, a)

			return zero, false
		}
		s.serverError(w, r, "admin: load season", err)

		return zero, false
	}

	if season.State == string(viewmodel.SeasonLocked) {
		s.opts.Logger.Info("admin: refused to edit a locked season",
			slog.String("season_id", season.ID.String()),
			slog.Int("year", int(season.Year)),
			slog.String("email", a.person.Email),
		)
		s.notFound(w, r, a)

		return zero, false
	}

	return season, true
}

// newSeasonPage builds the create screen.
func (s *Service) newSeasonPage(r *http.Request, a *admin, form viewmodel.AdminSeasonForm) viewmodel.AdminSeasonFormPage {
	return viewmodel.AdminSeasonFormPage{
		Layout:     s.page(r, "Start a new season", a),
		Form:       form,
		Action:     SeasonsPath,
		CancelHref: SeasonsPath,
	}
}

// editSeasonPage builds the re-date screen.
func (s *Service) editSeasonPage(r *http.Request, a *admin, season store.Season, form viewmodel.AdminSeasonForm) viewmodel.AdminSeasonFormPage {
	label := season.Name
	if label == "" {
		label = fmt.Sprintf("%d", season.Year)
	}

	return viewmodel.AdminSeasonFormPage{
		Layout:      s.page(r, "Edit "+label, a),
		Form:        form,
		Action:      seasonHref(season.ID),
		CancelHref:  SeasonsPath,
		Editing:     true,
		SeasonLabel: label,
	}
}

// createSeason is the write, and it is the half of ticket D3 that makes the
// bootstrap answer self-extinguishing.
//
// Three things happen in one transaction, and the second is the one that
// matters most:
//
//  1. The season row goes in. year is UNIQUE, so a second attempt for the same
//     year comes back as 23505 and becomes a sentence rather than a 500.
//  2. THE CREATOR IS WRITTEN IN AS AN ADMIN OF IT. Without this, an admin who
//     created next year's season would immediately lose the ability to edit
//     it: GetCurrentSeason returns the NEWEST non-draft season, so the moment
//     2027 opens, admin of 2026 stops being admin of anything. It is also what
//     makes BOOTSTRAP_ADMIN_EMAILS a one-time measure rather than a permanent
//     configuration: the person who used it to open the first season is an
//     ordinary per-season admin from the next request onwards.
//  3. The audit row. Creating a season is exactly the event migration 00007
//     exists for.
//
// All three or none. A season with no admin is a season nobody can re-date,
// and on a fresh deployment it is one nobody can recover from without a psql
// prompt.
func (s *Service) createSeason(ctx context.Context, a admin, d seasonDraft) (store.Season, error) {
	var zero store.Season

	tx, err := s.opts.DB.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("begin: %w", err)
	}
	// Safe after a successful Commit: pgx makes Rollback on a finished
	// transaction a no-op returning ErrTxClosed.
	defer func() { _ = tx.Rollback(ctx) }()

	q := store.New(tx)

	season, err := q.CreateSeason(ctx, d.createParams())
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return zero, errYearTaken
		}

		return zero, fmt.Errorf("insert season: %w", err)
	}

	// The upsert rather than a plain insert, because an admin who deleted a
	// season and recreated the same year should not hit a primary key
	// violation on their own membership row.
	if _, err := q.UpsertSeasonMember(ctx, store.UpsertSeasonMemberParams{
		SeasonID: season.ID,
		PersonID: a.person.ID,
		IsAdmin:  true,
		// NULL, not 0. NULL means "use season.default_submit_limit"; 0 would
		// mean this admin may vote and may not submit, which is not what
		// creating a season should do to you.
		SubmitLimit: nil,
	}); err != nil {
		return zero, fmt.Errorf("make creator an admin: %w", err)
	}

	err = audit.Record(ctx, q, a.person.Email, audit.ActionSeasonCreated, season.ID.String(), map[string]any{
		"year":                 season.Year,
		"name":                 season.Name,
		"state":                season.State,
		"default_submit_limit": season.DefaultSubmitLimit,
		"submit_closes_at":     timestampForAudit(season.VoteOpensAt),
		"vote_closes_at":       timestampForAudit(season.VoteClosesAt),
		"via_bootstrap":        a.viaBootstrap,
	})
	if err != nil {
		return zero, err
	}

	// The commit is checked. Migration 00008 makes two constraints DEFERRABLE
	// INITIALLY DEFERRED, so this codebase contains writes whose integrity
	// errors arrive here rather than at the statement.
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit: %w", err)
	}

	return season, nil
}

// updateSeason re-dates an existing season.
//
// It re-reads the season inside the transaction rather than trusting the row
// the GET loaded: the form was rendered before it was filled in, and a season
// can be locked in between. A locked season is refused here as well as on the
// way in, because the check that counts is the one in the transaction that
// writes.
//
// The state is not touched, which is why this does not have to worry about
// migration 00008's unlock guard at all -- that trigger is BEFORE UPDATE OF
// state, so a query that never assigns state never fires it. Deliberately
// unlocking a season is nap-hh3's, and nothing here weakens the trigger.
func (s *Service) updateSeason(ctx context.Context, a admin, season store.Season, d seasonDraft) (store.Season, error) {
	var zero store.Season

	tx, err := s.opts.DB.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := store.New(tx)

	current, err := q.GetSeason(ctx, season.ID)
	if err != nil {
		return zero, fmt.Errorf("reload season: %w", err)
	}
	if current.State == string(viewmodel.SeasonLocked) {
		return zero, fmt.Errorf("admin: %d locked before the edit could be saved", current.Year)
	}

	updated, err := q.UpdateSeasonDetails(ctx, store.UpdateSeasonDetailsParams{
		ID:                 current.ID,
		Name:               d.name,
		SubmitOpensAt:      d.submitOpensAt,
		VoteOpensAt:        d.submitClosesAt,
		VoteClosesAt:       d.voteClosesAt,
		DefaultSubmitLimit: d.submitLimit,
	})
	if err != nil {
		return zero, fmt.Errorf("update season: %w", err)
	}

	// Both the before and the after. "The deadline moved" is only an answer if
	// the log says what it moved from, and this table is the only place that
	// survives the row being edited again next week.
	err = audit.Record(ctx, q, a.person.Email, audit.ActionSeasonUpdated, updated.ID.String(), map[string]any{
		"year": updated.Year,
		"from": map[string]any{
			"name":                 current.Name,
			"default_submit_limit": current.DefaultSubmitLimit,
			"submit_opens_at":      timestampForAudit(current.SubmitOpensAt),
			"submit_closes_at":     timestampForAudit(current.VoteOpensAt),
			"vote_closes_at":       timestampForAudit(current.VoteClosesAt),
		},
		"to": map[string]any{
			"name":                 updated.Name,
			"default_submit_limit": updated.DefaultSubmitLimit,
			"submit_opens_at":      timestampForAudit(updated.SubmitOpensAt),
			"submit_closes_at":     timestampForAudit(updated.VoteOpensAt),
			"vote_closes_at":       timestampForAudit(updated.VoteClosesAt),
		},
	})
	if err != nil {
		return zero, err
	}

	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit: %w", err)
	}

	return updated, nil
}

// at renders a nullable timestamp in the site's timezone, or the zero time,
// which is viewmodel's sentinel for "not scheduled".
func (s *Service) at(ts pgtype.Timestamptz) time.Time {
	if !ts.Valid {
		return time.Time{}
	}

	return ts.Time.In(s.opts.Location)
}

// timestampForAudit renders a nullable timestamp for the audit payload, as
// RFC 3339 in UTC or as nil.
//
// UTC and not the site's timezone, deliberately: an audit row is read years
// later, possibly from a different machine, and a local time with no offset in
// it is not a time. The screens render in Options.Location; the log records
// the instant.
func timestampForAudit(ts pgtype.Timestamptz) any {
	if !ts.Valid {
		return nil
	}

	return ts.Time.UTC().Format(time.RFC3339)
}
