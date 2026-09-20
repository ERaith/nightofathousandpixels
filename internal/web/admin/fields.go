package admin

import (
	"strings"

	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// field is one control's labelling, as the markup needs it.
//
// It is internal/web/templates.formField's twin, and it is a twin rather than
// an import because that type is unexported and exporting it would mean
// touching the submit form's files for the benefit of a screen the submit form
// knows nothing about. The duplication is forty lines and is contained to this
// package; the alternative is a shared surface that two tickets are editing at
// once.
//
// What it exists for is the same thing: the accessible parts of a form field
// are not the input, they are the ids that have to agree about it. The label
// points at the control, the control points back at its hint and its error,
// and the summary at the top of the form points at the control again. Written
// out by hand four times per field, one of the four eventually disagrees and
// the failure is silent -- the error is on screen, correctly styled, and
// simply never announced.
type field struct {
	// ID is the control's id and the stem for its hint and error ids. It is
	// also what the error summary links to.
	ID string

	// Name is the posted field name, one of viewmodel's Field* constants.
	Name string

	// Label is the visible label. The summary reuses it, so it has to read on
	// its own.
	Label string

	// Hint is the line under the label, and it is where this form does most of
	// its explaining: "the board shuts" means something to the person who
	// wrote this application and nothing to the person filling it in in a
	// year's time.
	Hint string

	// Value is what goes back in the box -- what was typed, not a parsed
	// value.
	Value string

	// Error is the validation message, or "".
	Error string

	// Locked renders the control disabled. It is used for the year and the
	// state on the re-date screen, where the value is worth seeing and must
	// not be changed. The handler puts the stored value back regardless, so
	// this is presentation and not the enforcement.
	Locked bool
}

// HintID is the id of the hint paragraph, or "" when there is no hint.
func (f field) HintID() string {
	if f.Hint == "" {
		return ""
	}

	return f.ID + "-hint"
}

// ErrorID is the id of the error paragraph, or "" when there is no error and
// therefore no element to point at.
func (f field) ErrorID() string {
	if f.Error == "" {
		return ""
	}

	return f.ID + "-error"
}

// Invalid reports whether this field carries a message.
func (f field) Invalid() bool {
	return f.Error != ""
}

// AriaInvalid is the aria-invalid attribute value. It renders "false" rather
// than omitting the attribute, so the attribute is always present and always
// current -- a stale aria-invalid="true" is worse than an explicit false.
func (f field) AriaInvalid() string {
	if f.Invalid() {
		return "true"
	}

	return "false"
}

// DescribedBy is the aria-describedby value: the hint, then the error. The
// order is what a screen reader reads, and the hint comes first because it is
// what the field is; the error second because it is what is wrong this time.
func (f field) DescribedBy() string {
	ids := make([]string, 0, 2)
	for _, id := range []string{f.HintID(), f.ErrorID()} {
		if id != "" {
			ids = append(ids, id)
		}
	}

	return strings.Join(ids, " ")
}

// ErrorHref is what the error summary links to.
func (f field) ErrorHref() string {
	return "#" + f.ID
}

// seasonFields is the season form's controls, in the order they appear.
//
// A struct rather than a slice, for templates.submitFields' reason: a year
// input and a datetime input are not the same markup, so they cannot come out
// of a loop, and fields[2] in a template is what breaks when somebody reorders
// the form.
type seasonFields struct {
	Year         field
	Name         field
	SubmitOpens  field
	SubmitCloses field
	VoteCloses   field
	SubmitLimit  field
	State        field
}

// newSeasonFields builds the controls from the form's values and errors.
//
// This is the only place a field name is looked up in FieldErrors, which is
// what the viewmodel.Field* constants are for: the handler records against the
// constant and this reads back with the same constant, so a typo is a compile
// error rather than a message that silently never appears.
func newSeasonFields(p viewmodel.AdminSeasonFormPage) seasonFields {
	form := p.Form

	// A control that is disabled has to SAY it is disabled, in the hint, as
	// well as look it. The styling is the signal somebody notices after they
	// have tried; the sentence is the one they read before.
	locked := func(hint string) string {
		if !p.Editing {
			return hint
		}

		return hint + " It cannot be changed now."
	}

	return seasonFields{
		Year: field{
			ID:     "season-year",
			Name:   viewmodel.FieldSeasonYear,
			Label:  "Year",
			Hint:   locked("The season's identity, and the one thing that cannot be changed afterwards."),
			Value:  form.Year,
			Error:  form.Errors.For(viewmodel.FieldSeasonYear),
			Locked: p.Editing,
		},
		Name: field{
			ID:    "season-name",
			Name:  viewmodel.FieldSeasonName,
			Label: "Display name",
			Hint:  "The heading on every page for a month. \"The 2027 Season\" is fine.",
			Value: form.Name,
			Error: form.Errors.For(viewmodel.FieldSeasonName),
		},
		SubmitOpens: field{
			ID:    "season-submit-opens",
			Name:  viewmodel.FieldSubmitOpensAt,
			Label: "Submissions open (optional)",
			Hint:  "Leave this blank to open the season the moment you create it.",
			Value: form.SubmitOpensAt,
			Error: form.Errors.For(viewmodel.FieldSubmitOpensAt),
		},
		SubmitCloses: field{
			ID:    "season-submit-closes",
			Name:  viewmodel.FieldSubmitClosesAt,
			Label: "Submissions close",
			Hint:  "The board shuts and the slate is final. Required.",
			Value: form.SubmitClosesAt,
			Error: form.Errors.For(viewmodel.FieldSubmitClosesAt),
		},
		VoteCloses: field{
			ID:   "season-vote-closes",
			Name: viewmodel.FieldVoteClosesAt,
			// No voting-START field, and one must not be added: ranking opens
			// on a COUNT of three films, concurrently with submissions
			// (nap-h3l). The hint says so, because an admin looking for the
			// missing date deserves the answer on the page rather than in a
			// ticket.
			Label: "Voting closes",
			Hint:  "Ranking opens by itself once three films are up, so there is no start date to set. Required.",
			Value: form.VoteClosesAt,
			Error: form.Errors.For(viewmodel.FieldVoteClosesAt),
		},
		SubmitLimit: field{
			ID:    "season-submit-limit",
			Name:  viewmodel.FieldSeasonSubmitLimit,
			Label: "Films per person",
			Hint:  "The default everybody gets. You can give one person a different number later.",
			Value: form.SubmitLimit,
			Error: form.Errors.For(viewmodel.FieldSeasonSubmitLimit),
		},
		State: field{
			ID:     "season-state",
			Name:   viewmodel.FieldSeasonState,
			Label:  "Open it now?",
			Hint:   locked("Creating a season is normally the act of opening it. A draft is invisible to everyone but you."),
			Value:  form.State,
			Error:  form.Errors.For(viewmodel.FieldSeasonState),
			Locked: p.Editing,
		},
	}
}

// All is the controls in page order.
func (f seasonFields) All() []field {
	return []field{f.Year, f.Name, f.SubmitOpens, f.SubmitCloses, f.VoteCloses, f.SubmitLimit, f.State}
}

// Invalid is the controls carrying a message, in page order.
//
// The order is the form's own, top to bottom, which is what an error summary
// has to read in (WCAG 3.3.1): the list walks down the form the way the eye
// does. viewmodel.FieldErrors is a slice for the same reason -- nothing here
// has to sort a map to get a stable order.
func (f seasonFields) Invalid() []field {
	bad := make([]field, 0, len(f.All()))
	for _, one := range f.All() {
		if one.Invalid() {
			bad = append(bad, one)
		}
	}

	return bad
}

// stateChecked reports whether a radio option is the selected one. A blank
// stored value selects "open now", matching validateState's default.
func stateChecked(f field, option string) bool {
	if f.Value == "" {
		return option == stateSubmitting
	}

	return f.Value == option
}
