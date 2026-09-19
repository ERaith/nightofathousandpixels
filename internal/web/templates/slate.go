package templates

import "github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"

// nextDate is the one date on the slate that a visitor is actually planning
// around: not all four windows, just the next thing that happens.
type nextDate struct {
	// Label is what the date is, e.g. "Ranking opens".
	Label string

	// Value is the formatted timestamp, or "" when that window has no date
	// set — in which case the page shows nothing rather than a label with an
	// empty space after it.
	Value string
}

// Set reports whether there is a date to print.
func (d nextDate) Set() bool {
	return d.Value != ""
}

// seasonNextDate picks the deadline that matters in each phase of the year.
//
// This is not a countdown, and deliberately so. A live "3 days left" needs the
// clock, and viewmodel's rule is that nothing a template calls may consult
// one: the same page would then render differently in two timezones and no
// test could pin it. The date itself is what people put in their calendar
// anyway, and it is the honest thing to render from a view model that carries
// four timestamps and no duration.
//
// If a real counting-down number is wanted later, it belongs in the view model
// as a handler-computed field, or in a progressive-enhancement script that
// reads the timestamp out of the markup. Not here.
func seasonNextDate(season SeasonSummary) nextDate {
	switch season.State {
	case viewmodel.SeasonDraft:
		return nextDate{Label: "Submissions open", Value: season.SubmitOpensLabel()}

	case viewmodel.SeasonSubmitting:
		// vote_opens_at is when the board shuts: there is no separate
		// submissions-close column, and the two are the same moment.
		// SeasonState.Summary has already said so in words just above, so the
		// label here is the short one.
		return nextDate{Label: "Ranking opens", Value: season.VoteOpensLabel()}

	case viewmodel.SeasonVoting:
		return nextDate{Label: "Voting closes", Value: season.VoteClosesLabel()}

	case viewmodel.SeasonLocked:
		return nextDate{Label: "Locked", Value: season.LockedLabel()}

	default:
		return nextDate{}
	}
}

// slateHeading is the slate's h1.
//
// SeasonSummary.Label falls back from the name to the year, but both can be
// absent at once — a season row always has a year, yet a page rendered from a
// zero SlatePage does not. An empty h1 is a document with no title for anyone
// navigating by heading, so there is a last fallback here.
func slateHeading(season SeasonSummary) string {
	if label := season.Label(); label != "" {
		return label
	}

	return "The slate"
}
