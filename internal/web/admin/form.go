package admin

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ERaith/nightofathousandpixels/internal/store"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// Limits on what the form will accept.
//
// The year range is season_year_range from migration 00002, duplicated here so
// that an admin is told what is wrong in words rather than handed a constraint
// violation. The others have no constraint behind them at all: season.name is
// text with a not-blank CHECK and nothing more, and default_submit_limit is
// only required to be non-negative.
const (
	minYear = 1888
	maxYear = 2200

	maxNameLen = 200

	// maxSubmitLimit is a sanity bound, not a rule. The schema allows any
	// non-negative number; thirty friends with fifty picks each is a typo, and
	// a typo here quietly changes what every membership row means, because
	// season_member.submit_limit is NULL for almost everybody and NULL means
	// "use this number".
	maxSubmitLimit = 20

	// maxFormBytes bounds the whole request body before it is parsed.
	// http.Request.ParseForm reads it all into memory, so this is the only
	// limit that exists before the allocation happens.
	maxFormBytes = 32 << 10
)

// The two states this form can put a season in.
//
// 'voting' and 'locked' are deliberately absent. Both are transitions with
// consequences -- locked is terminal, enforced by a trigger in migration 00008
// -- and neither is something a date form should be able to do as a side
// effect of saving a typo.
const (
	stateSubmitting = string(viewmodel.SeasonSubmitting)
	stateDraft      = string(viewmodel.SeasonDraft)
)

// dateLayout is what an <input type="datetime-local"> posts.
//
// It carries NO timezone -- that is the whole shape of the control, and it is
// why Options.Location exists. The browser sends the wall-clock time the admin
// typed and nothing about where they were standing; this application decides
// what that meant, once, in one place.
//
// A browser may include seconds when the control has a step, so the seconds
// form is accepted too rather than rejected as unparseable.
const (
	dateLayout        = "2006-01-02T15:04"
	dateLayoutSeconds = "2006-01-02T15:04:05"
)

// seasonDraft is a validated season form, in the types the database takes.
type seasonDraft struct {
	year        int32
	name        string
	state       string
	submitLimit int32

	// The three windows, already in UTC-comparable pgtype form. submitOpensAt
	// may be invalid (SQL NULL); the other two are always set, because the
	// form requires them.
	submitOpensAt  pgtype.Timestamptz
	submitClosesAt pgtype.Timestamptz
	voteClosesAt   pgtype.Timestamptz
}

// createParams turns the draft into the insert.
//
// submitClosesAt goes into vote_opens_at. That is not a workaround: the schema
// has no separate submissions-close column because the board shutting and the
// slate becoming final are one moment, which is what seasonNextDate in
// internal/web/templates/slate.go already renders from this column. See
// viewmodel.AdminSeasonForm for the fuller note, including why there is no
// voting-START field anywhere in this package.
func (d seasonDraft) createParams() store.CreateSeasonParams {
	return store.CreateSeasonParams{
		Year:               d.year,
		Name:               d.name,
		State:              d.state,
		SubmitOpensAt:      d.submitOpensAt,
		VoteOpensAt:        d.submitClosesAt,
		VoteClosesAt:       d.voteClosesAt,
		DefaultSubmitLimit: d.submitLimit,
	}
}

// parseSeasonForm reads the posted values without tidying them.
//
// Nothing is trimmed here, because these are also the values that go back in
// the boxes if anything is wrong. Trimming happens in validation, against a
// copy -- the same rule viewmodel.SubmitForm states for the submit form.
func parseSeasonForm(value func(string) string) viewmodel.AdminSeasonForm {
	return viewmodel.AdminSeasonForm{
		Year:           value(viewmodel.FieldSeasonYear),
		Name:           value(viewmodel.FieldSeasonName),
		SubmitOpensAt:  value(viewmodel.FieldSubmitOpensAt),
		SubmitClosesAt: value(viewmodel.FieldSubmitClosesAt),
		VoteClosesAt:   value(viewmodel.FieldVoteClosesAt),
		SubmitLimit:    value(viewmodel.FieldSeasonSubmitLimit),
		State:          value(viewmodel.FieldSeasonState),
	}
}

// validateSeason turns what was typed into either a draft or a set of
// per-field messages. It never returns both, and it never modifies the form.
//
// requireFuture is true when creating and false when editing, and the
// asymmetry is the point rather than an oversight. A season being created must
// have both deadlines ahead of it: one in the past would be a season that was
// over before anybody saw it. A season being EDITED is very often one whose
// submission deadline has already passed -- that is what the voting week is --
// and refusing to let an admin fix a typo in the voting deadline because the
// submission deadline is behind them would make this screen useless in exactly
// the week it is used.
//
// What holds in both cases is the ordering: the board cannot shut after the
// ballots close, whatever the calendar says today.
func validateSeason(form viewmodel.AdminSeasonForm, loc *time.Location, now time.Time, requireFuture bool) (seasonDraft, viewmodel.FieldErrors) {
	var (
		d    seasonDraft
		errs viewmodel.FieldErrors
	)

	d.year = validateYear(form.Year, &errs)
	d.name = validateName(form.Name, &errs)
	d.state = validateState(form.State, &errs)
	d.submitLimit = validateSubmitLimit(form.SubmitLimit, &errs)

	opens, haveOpens := parseDate(form.SubmitOpensAt, loc, viewmodel.FieldSubmitOpensAt, false, &errs)
	closes, haveCloses := parseDate(form.SubmitClosesAt, loc, viewmodel.FieldSubmitClosesAt, true, &errs)
	votes, haveVotes := parseDate(form.VoteClosesAt, loc, viewmodel.FieldVoteClosesAt, true, &errs)

	// The ordering rules, checked only between dates that both parsed. A
	// second message against a field that is already unparseable would be
	// noise about a value that does not exist.
	if haveCloses && haveVotes && !closes.Before(votes) {
		errs.Add(viewmodel.FieldVoteClosesAt,
			"Voting has to close after the board does. Give people at least a day to rank what went up.")
	}
	if haveOpens && haveCloses && opens.After(closes) {
		errs.Add(viewmodel.FieldSubmitOpensAt,
			"Submissions cannot open after they close. Leave this blank to open the season now.")
	}

	if requireFuture {
		if haveCloses && !closes.After(now) {
			errs.Add(viewmodel.FieldSubmitClosesAt,
				"That date has already been and gone. A new season needs a deadline people can still make.")
		}
		if haveVotes && !votes.After(now) {
			errs.Add(viewmodel.FieldVoteClosesAt,
				"That date has already been and gone. A new season needs a deadline people can still make.")
		}
	}

	if errs.Any() {
		return seasonDraft{}, errs
	}

	if haveOpens {
		d.submitOpensAt = pgtype.Timestamptz{Time: opens, Valid: true}
	} else if d.state == stateSubmitting {
		// Creating a season as 'submitting' IS the act of opening it, so the
		// moment it was created is when submissions opened. Recording that is
		// a fact rather than a guess, and it is what stops the slate's "the
		// board opened" line from being blank on every season anybody actually
		// ran. A draft is left NULL: it has not opened.
		d.submitOpensAt = pgtype.Timestamptz{Time: now, Valid: true}
	}
	d.submitClosesAt = pgtype.Timestamptz{Time: closes, Valid: true}
	d.voteClosesAt = pgtype.Timestamptz{Time: votes, Valid: true}

	return d, errs
}

func validateYear(raw string, errs *viewmodel.FieldErrors) int32 {
	year := strings.TrimSpace(raw)
	if year == "" {
		errs.Add(viewmodel.FieldSeasonYear, "Every season is a year. This is the one thing that cannot be changed later.")

		return 0
	}

	n, err := strconv.Atoi(year)
	switch {
	case err != nil:
		errs.Add(viewmodel.FieldSeasonYear, "That doesn't look like a year. Four digits, like 2027.")
	case n < minYear || n > maxYear:
		errs.Add(viewmodel.FieldSeasonYear, fmt.Sprintf("Seasons run from %d to %d.", minYear, maxYear))
	default:
		return int32(n)
	}

	return 0
}

func validateName(raw string, errs *viewmodel.FieldErrors) string {
	name := strings.TrimSpace(raw)
	switch {
	case name == "":
		errs.Add(viewmodel.FieldSeasonName, "Give the season a name. It is the heading on every page for a month.")
	case len([]rune(name)) > maxNameLen:
		errs.Add(viewmodel.FieldSeasonName, fmt.Sprintf("That name is longer than a heading can show. Keep it under %d characters.", maxNameLen))
	default:
		return name
	}

	return ""
}

// validateState refuses anything but the two the form offers.
//
// The season_state_valid CHECK would catch 'banana', but it would also accept
// 'locked' -- and a season created locked is an archive with no ballots in it,
// which the season_locked_has_timestamp CHECK then refuses as a 500 rather
// than as a sentence. Naming the two allowed values here is what keeps this a
// form error.
func validateState(raw string, errs *viewmodel.FieldErrors) string {
	state := strings.TrimSpace(raw)
	switch state {
	case "":
		// The control is a pair of radio buttons with one pre-selected, so an
		// empty value means the markup and this function disagree rather than
		// that somebody chose nothing. Default to the ordinary case rather
		// than making an admin re-pick something they never unpicked.
		return stateSubmitting
	case stateSubmitting, stateDraft:
		return state
	default:
		errs.Add(viewmodel.FieldSeasonState, "A season either opens now or waits as a draft.")

		return ""
	}
}

func validateSubmitLimit(raw string, errs *viewmodel.FieldErrors) int32 {
	limit := strings.TrimSpace(raw)
	if limit == "" {
		errs.Add(viewmodel.FieldSeasonSubmitLimit, "How many films may each person put up? Two is the usual answer.")

		return 0
	}

	n, err := strconv.Atoi(limit)
	switch {
	case err != nil:
		errs.Add(viewmodel.FieldSeasonSubmitLimit, "That doesn't look like a number. Two is the usual answer.")
	case n < 0:
		errs.Add(viewmodel.FieldSeasonSubmitLimit, "A limit cannot be negative. Zero means nobody submits, which is a real but unusual choice.")
	case n > maxSubmitLimit:
		errs.Add(viewmodel.FieldSeasonSubmitLimit, fmt.Sprintf("That is more than anybody needs. Keep it at %d or under.", maxSubmitLimit))
	default:
		return int32(n)
	}

	return 0
}

// parseDate reads one datetime-local value in the site's timezone.
//
// The returned bool is "there is a usable time here", which is NOT the same as
// "there was no error": a blank optional field yields false with no message,
// and an unparseable one yields false with one. The ordering checks above use
// it to stay quiet about values that do not exist.
func parseDate(raw string, loc *time.Location, field string, required bool, errs *viewmodel.FieldErrors) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		if required {
			errs.Add(field, "This one is required. The whole season is scheduled around it.")
		}

		return time.Time{}, false
	}

	for _, layout := range []string{dateLayout, dateLayoutSeconds} {
		// ParseInLocation, never Parse. Parse would read a zoneless string as
		// UTC, which in this application's timezone is an hour or more out --
		// enough to close the board on the wrong evening.
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t, true
		}
	}

	errs.Add(field, "That date did not come through. Pick it again with the calendar control.")

	return time.Time{}, false
}

// formFromSeason fills the form with what is currently stored, for the
// re-date screen.
func (s *Service) formFromSeason(season store.Season) viewmodel.AdminSeasonForm {
	return viewmodel.AdminSeasonForm{
		Year:           strconv.Itoa(int(season.Year)),
		Name:           season.Name,
		SubmitOpensAt:  s.formatDate(season.SubmitOpensAt),
		SubmitClosesAt: s.formatDate(season.VoteOpensAt),
		VoteClosesAt:   s.formatDate(season.VoteClosesAt),
		SubmitLimit:    strconv.Itoa(int(season.DefaultSubmitLimit)),
		State:          season.State,
	}
}

// formatDate renders a nullable timestamp for a datetime-local input, or "".
//
// In the site's timezone, matching parseDate. A round trip through this screen
// therefore leaves a date unchanged, which it would not if one direction used
// UTC and the other did not.
func (s *Service) formatDate(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}

	return ts.Time.In(s.opts.Location).Format(dateLayout)
}
