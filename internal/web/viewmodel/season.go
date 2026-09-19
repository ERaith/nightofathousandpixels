package viewmodel

import (
	"strconv"
	"time"
)

// SeasonState is where a season is in its year. The four values match the
// season_state_valid CHECK in migration 00002 exactly, because a fifth one
// appearing in the database and not here would render as a blank label rather
// than failing loudly.
type SeasonState string

// The four phases a season moves through, in order.
const (
	// SeasonDraft is a season that has not opened. GetCurrentSeason hides
	// draft seasons from everyone but an admin, so a page in this state is
	// normally only reachable by one.
	SeasonDraft SeasonState = "draft"

	// SeasonSubmitting is the submission window: the board is filling up.
	SeasonSubmitting SeasonState = "submitting"

	// SeasonVoting is the ranking window. The slate is closed; ballots are open.
	SeasonVoting SeasonState = "voting"

	// SeasonLocked is the archive. A locked season is immutable — migration
	// 00008 enforces that with triggers — so a page showing one never offers
	// an edit control.
	SeasonLocked SeasonState = "locked"
)

// Label is the state as a person would say it, for a badge.
func (s SeasonState) Label() string {
	switch s {
	case SeasonDraft:
		return "Not open yet"
	case SeasonSubmitting:
		return "Submissions open"
	case SeasonVoting:
		return "Voting open"
	case SeasonLocked:
		return "Finished"
	default:
		return string(s)
	}
}

// Summary is the one line of copy under the heading that says what a visitor
// can do right now. It is copy rather than a decision: whether this particular
// viewer may act is a separate, handler-answered question.
func (s SeasonState) Summary() string {
	switch s {
	case SeasonDraft:
		return "This year has not started yet. The link lands in the group chat when it does."
	case SeasonSubmitting:
		return "Put your picks on the board. Ranking opens once submissions close."
	case SeasonVoting:
		return "Submissions are closed. Rank your top three — a second choice is what breaks the tie."
	case SeasonLocked:
		return "This year is finished and in the archive."
	default:
		return ""
	}
}

// IsLocked reports whether this season is the archive.
func (s SeasonState) IsLocked() bool {
	return s == SeasonLocked
}

// SeasonSummary is the season a page is about: its name, where it is in the
// year, and the four window timestamps that drive the countdown copy.
//
// Every timestamp is a plain time.Time, and the zero value means "not
// scheduled" — which is a real state, because all four columns in migration
// 00002 are nullable so a season can be created before its dates are decided.
// A zero time.Time is safe as that sentinel in a way a zero int is not: it is
// year 1, and season_year_range already forbids anything before 1888.
//
// The times arrive in whatever location the handler put them in, and that is
// deliberate: this package has no opinion about the site's timezone and no
// business reading one. FormatDeadline renders whatever it is handed.
type SeasonSummary struct {
	// Name is season.name, e.g. "The 2026 Season". Blank falls back to the
	// year in Label.
	Name string

	// Year is season.year. Zero means unknown, which no real season row is.
	Year int

	// State is where the season is now.
	State SeasonState

	// The four windows. Zero means the date has not been decided.
	SubmitOpensAt time.Time
	VoteOpensAt   time.Time
	VoteClosesAt  time.Time
	LockedAt      time.Time
}

// Label is the season's name for a heading, falling back to the year.
func (s SeasonSummary) Label() string {
	if s.Name != "" {
		return s.Name
	}
	if s.Year > 0 {
		return strconv.Itoa(s.Year) + " season"
	}

	return ""
}

// SubmitOpensLabel, VoteOpensLabel, VoteClosesLabel and LockedLabel render
// each window for display, or "" when that date has not been set. A template
// uses the emptiness to decide whether to show the line at all — "Voting
// closes: " with nothing after it is worse than no line.
func (s SeasonSummary) SubmitOpensLabel() string { return FormatDeadline(s.SubmitOpensAt) }

// VoteOpensLabel renders vote_opens_at, or "" when it is not scheduled.
func (s SeasonSummary) VoteOpensLabel() string { return FormatDeadline(s.VoteOpensAt) }

// VoteClosesLabel renders vote_closes_at, or "" when it is not scheduled.
func (s SeasonSummary) VoteClosesLabel() string { return FormatDeadline(s.VoteClosesAt) }

// LockedLabel renders locked_at, or "" when the season is not locked.
func (s SeasonSummary) LockedLabel() string { return FormatDeadline(s.LockedAt) }

// deadlineLayout is how every date on the site reads: "Friday 3 October, 9:00 PM".
// The weekday is spelled out because the dates people care about here are all
// within a few weeks, and "Friday" is the part anybody actually plans around.
const deadlineLayout = "Monday 2 January, 3:04 PM"

// FormatDeadline renders a window timestamp for display, or "" for the zero
// time, which means the date has not been decided.
//
// It formats in t's own location and does not consult the clock, so it is
// deterministic and safe to call from a template.
func FormatDeadline(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Format(deadlineLayout)
}

// SeasonResult is the frozen outcome of a locked season, as a slate page shows
// it: who won, or that nobody did.
//
// It deliberately does not model result.rounds, the round-by-round
// instant-runoff breakdown. That is a jsonb blob with a shape of its own and a
// page of its own, and guessing at it here would mean two ticket owners
// inventing incompatible versions of the same thing. When the results page is
// built, its view model is added to this package and this struct gains a
// pointer to it.
type SeasonResult struct {
	// WinnerTitle is the winning movie's title. Blank when Tied is true.
	WinnerTitle string

	// Tied is true when the final round had no single winner.
	// result.winner_movie_id is nullable for exactly this case.
	Tied bool

	// TalliedAt is when the count was run. Zero when unknown.
	TalliedAt time.Time
}

// TalliedLabel renders TalliedAt for display, or "" when it is not set.
func (r SeasonResult) TalliedLabel() string { return FormatDeadline(r.TalliedAt) }
