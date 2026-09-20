package viewmodel

import "strconv"

// The admin season form's field names.
//
// They are constants for the same reason the submit form's are: the handler
// that records an error and the template that prints it beside the input have
// to agree on one key, and a typo in either is silent -- the message simply
// never appears next to its field.
const (
	FieldSeasonYear        = "year"
	FieldSeasonName        = "name"
	FieldSubmitOpensAt     = "submit_opens_at"
	FieldSubmitClosesAt    = "submit_closes_at"
	FieldVoteClosesAt      = "vote_closes_at"
	FieldSeasonSubmitLimit = "submit_limit"
	FieldSeasonState       = "state"
)

// AdminSeasonForm is the state of the season form, including a rejected one.
//
// Every value is a string, deliberately, and for the reason SubmitForm gives:
// when validation fails the form has to come back showing what the person
// actually typed. A year of "twenty twenty seven" has nowhere to live in an
// int, and a field that redisplays as 0 looks like the site ate the input.
//
// THERE IS NO VOTING-START FIELD HERE AND ONE MUST NOT BE ADDED. Voting opens
// on a COUNT, not on a date: nap-h3l records the decision that people may rank
// as soon as three films exist, concurrently with submissions still coming in.
// A voting-start date would be a second, contradictory answer to a question
// the model already answers, and the two would disagree in the one week of the
// year anybody is looking.
//
// SubmitClosesAt maps to season.vote_opens_at, which is the schema's name for
// the same moment. There is no separate submissions-close column because there
// is no gap between the two: the board shuts and the slate is final. See the
// comment on seasonNextDate in internal/web/templates/slate.go, which says the
// same thing from the rendering side.
type AdminSeasonForm struct {
	// Year is the season's identity and its UNIQUE key. It is editable only on
	// creation -- see AdminSeasonFormPage.Editing.
	Year string

	// Name is season.name, the display name: "The 2027 Season".
	Name string

	// SubmitOpensAt is season.submit_opens_at. Optional: a season created as
	// 'submitting' opened when it was created, and the handler fills that in.
	SubmitOpensAt string

	// SubmitClosesAt is when the board shuts -- season.vote_opens_at. Required.
	SubmitClosesAt string

	// VoteClosesAt is the voting deadline -- season.vote_closes_at. Required.
	VoteClosesAt string

	// SubmitLimit is season.default_submit_limit, how many films each person
	// may put up before a per-person override.
	SubmitLimit string

	// State is 'submitting' or 'draft'. Creating a season is the act of
	// opening it, so 'submitting' is the default; 'draft' is for a season
	// prepared ahead of time and is deliberately the second option rather than
	// an absent one.
	State string

	// Errors is empty on a first render and on a successful one.
	Errors FieldErrors
}

// AdminSeasonFormPage is the create-a-season screen and the re-date-a-season
// screen, which are the same form with two of its controls behaving
// differently.
type AdminSeasonFormPage struct {
	Layout LayoutData

	// Form is the current values and any validation messages.
	Form AdminSeasonForm

	// Action is where the form posts.
	Action string

	// CancelHref is the way back to the season list.
	CancelHref string

	// Editing is true on the re-date screen. It locks two controls rather than
	// hiding them:
	//
	//   year   is the season's identity and the UNIQUE key the archive is
	//          browsed by. "2026 is now 2027" is a different operation from
	//          "the 2026 deadline moved", and it breaks every link already
	//          pasted into the group chat.
	//   state  moves through its own transitions, and migration 00008 makes
	//          one of them terminal. A date form that could also lock a season
	//          would be able to end the year by accident.
	//
	// They stay visible because the values are what the admin is checking they
	// are editing the right season, and a form that hides the year is a form
	// somebody re-dates 2025 with.
	Editing bool

	// SeasonLabel names the season being edited, for the heading. Blank on the
	// create screen.
	SeasonLabel string
}

// Heading is the h1.
func (p AdminSeasonFormPage) Heading() string {
	if p.Editing {
		if p.SeasonLabel != "" {
			return "Edit " + p.SeasonLabel
		}

		return "Edit this season"
	}

	return "Start a new season"
}

// Lead is the line under the heading. It says what the screen is for in terms
// of the thing the admin came here to change.
func (p AdminSeasonFormPage) Lead() string {
	if p.Editing {
		return "Dates slip every year. Change them here and the board and the " +
			"countdown follow on the next page load."
	}

	return "A year, a name, and the two deadlines. Everything else has a sensible " +
		"default you can change later."
}

// SubmitLabel is the form's button.
func (p AdminSeasonFormPage) SubmitLabel() string {
	if p.Editing {
		return "Save the dates"
	}

	return "Open the season"
}

// AdminSeasonRow is one season on the admin list.
//
// Every value is already rendered. The list is the one page where an admin
// checks at a glance that the right year is live and the dates are what they
// meant, so a nullable timestamp that has not been decided has to read as
// "not set" rather than as a blank cell that could equally be a bug.
type AdminSeasonRow struct {
	// Name is the display name, falling back to the year -- see Label.
	Name string

	// Year is season.year.
	Year int

	// State is where the season is now.
	State SeasonState

	// SubmitClosesLabel and VoteClosesLabel are the two deadlines, already
	// formatted, or "" when they have not been decided.
	SubmitClosesLabel string
	VoteClosesLabel   string

	// EditHref is the re-date screen for this season. Blank renders no control,
	// which is what a locked season gets: migration 00008 makes locked
	// terminal, and a locked season is the archive.
	EditHref string

	// IsCurrent marks the season GetCurrentSeason would return -- the newest
	// one that has left draft, which is the one the public site is showing.
	// It is worth a badge because "I edited the season" and "I edited the
	// season people are looking at" are different amounts of careful.
	IsCurrent bool
}

// Label is the season's name for the list, falling back to the year.
func (r AdminSeasonRow) Label() string {
	if r.Name != "" {
		return r.Name
	}
	if r.Year > 0 {
		return strconv.Itoa(r.Year) + " season"
	}

	return ""
}

// YearLabel is the year as text.
func (r AdminSeasonRow) YearLabel() string {
	if r.Year <= 0 {
		return ""
	}

	return strconv.Itoa(r.Year)
}

// CanEdit reports whether this row offers a way in.
func (r AdminSeasonRow) CanEdit() bool {
	return r.EditHref != ""
}

// AdminSeasonsPage is the admin's list of every season, drafts included.
type AdminSeasonsPage struct {
	Layout LayoutData

	// Seasons is every season, newest first. Empty is the state a production
	// deployment starts in -- production is never seeded -- so it is the first
	// thing an admin ever sees here and has to explain itself.
	Seasons []AdminSeasonRow

	// NewHref is the create screen.
	NewHref string

	// IsBootstrap is true when the viewer is reading this page on the strength
	// of BOOTSTRAP_ADMIN_EMAILS rather than a season_member row.
	//
	// The page says so out loud. Somebody who set that variable months ago has
	// forgotten it, and "why can I see this" is a question worth answering on
	// the page rather than in a log -- particularly because the answer comes
	// with an action: create a season and the membership row it writes makes
	// the variable unnecessary.
	IsBootstrap bool
}

// HasSeasons reports whether there is a list to show yet.
func (p AdminSeasonsPage) HasSeasons() bool {
	return len(p.Seasons) > 0
}
