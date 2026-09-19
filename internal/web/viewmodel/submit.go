package viewmodel

import "strconv"

// SubmitQuota is how many movies a viewer may still put on the board, and it
// is deliberately not a bare int.
//
// The database answers the limit question with
//
//	COALESCE(season_member.submit_limit, season.default_submit_limit)
//
// and GetEffectiveSubmitLimit is careful about it, because an explicit
// submit_limit of 0 is a real setting rather than an absent one: that person
// may vote but may not submit. The query's own comment spells out the trap —
// "the obvious wrong version treats a NULL override as 0 and silently bans the
// person from submitting" — and it draws a third distinction on top: no rows
// at all means not a member of this season, which is a different answer again
// from a limit of 0.
//
// A single int field on this side of the seam would throw that away. Go's zero
// value for int is 0, so a struct nobody populated and a person who is
// explicitly barred from submitting would be indistinguishable, and the
// obvious repair — "if limit == 0 { limit = seasonDefault }" — is exactly the
// COALESCE bug the store layer avoided, reintroduced one layer up where no
// constraint can catch it.
//
// Known is what keeps the three answers apart:
//
//	Known == false             no quota here: signed out, or signed in but not
//	                           a member of this season.
//	Known && Limit == 0        a member who may vote but may not submit.
//	Known && Used < Limit      a member with room left.
//	Known && Used >= Limit     a member who has used their picks.
//
// The zero value is the safe one. HasRoom reports false on a struct nobody
// filled in, so the failure mode of forgetting to populate this is a hidden
// submit button rather than one that the handler then refuses.
//
// Known is set by whether GetEffectiveSubmitLimit returned a row, not by
// whether Limit is non-zero. Those are different questions, and conflating
// them is the bug this type exists to make unrepresentable.
type SubmitQuota struct {
	// Known is true when this viewer has a submission limit in this season at
	// all — that is, when they are a member of it. It must be set explicitly.
	Known bool

	// Used is how many live submissions they already have, from
	// CountPersonMoviesInSeason. Hidden movies are not counted there, so
	// withdrawing a film gives the slot back, and Used and the movies a page
	// lists always agree.
	Used int

	// Limit is the already-resolved effective limit — the COALESCE above, not
	// the raw override. Zero is meaningful and only meaningful when Known is
	// true.
	Limit int
}

// HasRoom reports whether this viewer has a slot left.
//
// It answers a question about the quota alone. It is deliberately not called
// CanSubmit: whether the submit form should appear also depends on the season
// window and on authorisation, and that decision belongs to a handler and
// arrives as SlatePage.CanSubmit.
func (q SubmitQuota) HasRoom() bool {
	return q.Known && q.Used < q.Limit
}

// Barred reports the "may vote but may not submit" case: a member of this
// season whose effective limit is an explicit zero. It is worth its own method
// because it needs different copy from "you have used your picks" — one is a
// setting, the other is progress.
func (q SubmitQuota) Barred() bool {
	return q.Known && q.Limit == 0
}

// AtLimit reports a member who had picks and has used them all.
func (q SubmitQuota) AtLimit() bool {
	return q.Known && q.Limit > 0 && q.Used >= q.Limit
}

// Remaining is how many picks are left, never negative.
//
// It clamps rather than returning a negative number because Used can exceed
// Limit for real: an admin lowering someone's submit_limit after they have
// already submitted is a legitimate edit, and nothing in the schema prevents
// it. "-1 picks left" is not a thing to print on a page.
func (q SubmitQuota) Remaining() int {
	if !q.Known {
		return 0
	}

	if left := q.Limit - q.Used; left > 0 {
		return left
	}

	return 0
}

// Label is the sentence a page puts under the submit button, or "" when this
// viewer has no quota to describe.
func (q SubmitQuota) Label() string {
	switch {
	case !q.Known:
		return ""
	case q.Barred():
		return "You can vote this year, but not put films on the board."
	case q.AtLimit():
		return "You have used all " + plural(q.Limit, "pick", "picks") + " for this year."
	default:
		return plural(q.Remaining(), "pick", "picks") + " left of " + strconv.Itoa(q.Limit) + "."
	}
}

// plural renders a count with the right noun: "1 pick", "2 picks".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return strconv.Itoa(n) + " " + many
}

// The names of the submit form's fields. They are constants because the
// handler that records an error and the template that prints it next to the
// input have to agree on the same key, and a typo in either would fail
// silently — the error would simply never appear beside its field.
const (
	FieldTitle       = "title"
	FieldYear        = "year"
	FieldTrailerURL  = "trailer_url"
	FieldDescription = "description"
)

// FieldError is one validation message against one form field.
type FieldError struct {
	// Field is one of the Field* constants above.
	Field string

	// Message is what to show the person. It is written for them, not for a
	// log: "That does not look like a year" rather than "strconv.Atoi failed".
	Message string
}

// FieldErrors is a form's validation messages, in the order they should be
// listed.
//
// It is a slice rather than a map so the order is stable. An accessible form
// leads with a summary that links to each bad field (WCAG 3.3.1), and that
// summary has to read in the form's own order every time — a map would
// reorder it on every render.
type FieldErrors []FieldError

// Any reports whether there is anything wrong with the form.
func (e FieldErrors) Any() bool {
	return len(e) > 0
}

// Has reports whether a particular field is in error, for the aria-invalid
// attribute on its input.
func (e FieldErrors) Has(field string) bool {
	return e.For(field) != ""
}

// For is the message against one field, or "" when that field is fine.
func (e FieldErrors) For(field string) string {
	for _, err := range e {
		if err.Field == field {
			return err.Message
		}
	}

	return ""
}

// Add records a message against a field, replacing any message already there.
// One field carries one message: a list of everything wrong with a single
// input is not something anyone reads.
func (e *FieldErrors) Add(field, message string) {
	for i := range *e {
		if (*e)[i].Field == field {
			(*e)[i].Message = message

			return
		}
	}

	*e = append(*e, FieldError{Field: field, Message: message})
}

// SubmitForm is the state of the submit form, including a rejected one.
//
// Every value is a string, including Year, and that is the point rather than
// laziness. When validation fails the form has to come back showing what the
// person actually typed — if Year were an int, "nineteen eighty-four" would
// have nowhere to live and the field would redisplay as empty or as 0, which
// looks like the site ate the input. The typed text is what goes back in the
// box; the parsed value never reaches this struct.
type SubmitForm struct {
	Title       string
	Year        string
	TrailerURL  string
	Description string

	// Errors is empty on a first render and on a successful one.
	Errors FieldErrors
}
