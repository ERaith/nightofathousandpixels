package admin

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// The season form's validation, which is the half of ticket D3 that has no
// database in it. Every case here is a sentence an admin reads, and the
// failures the ticket names by hand -- the two dates in the wrong order, a
// date in the past, a missing deadline -- have to be refused with words rather
// than with a constraint violation.

// testLocation is a fixed non-UTC zone, deliberately.
//
// A test that ran in UTC would pass whether or not the parser used
// ParseInLocation, because "2027-10-24T20:00" read as UTC and read as UTC are
// the same instant. The whole point of Options.Location is the case where they
// differ.
var testLocation = time.FixedZone("Test/Minus6", -6*60*60)

// testNow is the clock every case below is judged against, so "in the past"
// means something fixed rather than something that changes in January.
var testNow = time.Date(2026, time.September, 20, 12, 0, 0, 0, testLocation)

func goodForm() viewmodel.AdminSeasonForm {
	return viewmodel.AdminSeasonForm{
		Year:           "2027",
		Name:           "The 2027 Season",
		SubmitClosesAt: "2027-10-24T20:00",
		VoteClosesAt:   "2027-10-31T20:00",
		SubmitLimit:    "2",
		State:          stateSubmitting,
	}
}

func TestAGoodFormValidates(t *testing.T) {
	d, errs := validateSeason(goodForm(), testLocation, testNow, true)
	if errs.Any() {
		t.Fatalf("a good form was refused: %v", errs)
	}

	switch {
	case d.year != 2027:
		t.Errorf("year = %d, want 2027", d.year)
	case d.name != "The 2027 Season":
		t.Errorf("name = %q", d.name)
	case d.submitLimit != 2:
		t.Errorf("submit limit = %d, want 2", d.submitLimit)
	case d.state != stateSubmitting:
		t.Errorf("state = %q, want %q", d.state, stateSubmitting)
	}

	// The dates were read in the site's timezone, not as UTC. 20:00 at -06:00
	// is 02:00 the next day in UTC, and getting this wrong closes the board on
	// the wrong evening.
	want := time.Date(2027, time.October, 24, 20, 0, 0, 0, testLocation)
	if !d.submitClosesAt.Valid || !d.submitClosesAt.Time.Equal(want) {
		t.Errorf("submissions close = %v, want %v", d.submitClosesAt.Time, want)
	}
}

// TestCreatingOpensTheSeasonNow: a season created as 'submitting' opened when
// it was created, and recording that is a fact rather than a guess. A draft
// has not opened, so its column stays NULL.
func TestCreatingOpensTheSeasonNow(t *testing.T) {
	d, errs := validateSeason(goodForm(), testLocation, testNow, true)
	if errs.Any() {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if !d.submitOpensAt.Valid || !d.submitOpensAt.Time.Equal(testNow) {
		t.Errorf("submit_opens_at = %v (valid=%v), want %v", d.submitOpensAt.Time, d.submitOpensAt.Valid, testNow)
	}

	form := goodForm()
	form.State = stateDraft
	d, errs = validateSeason(form, testLocation, testNow, true)
	if errs.Any() {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if d.submitOpensAt.Valid {
		t.Error("a draft season recorded an opening time; a draft has not opened")
	}
}

// TestTheTwoDeadlinesMustBeInOrder is the pair check the ticket names.
func TestTheTwoDeadlinesMustBeInOrder(t *testing.T) {
	form := goodForm()
	form.SubmitClosesAt = "2027-10-31T20:00"
	form.VoteClosesAt = "2027-10-24T20:00"

	_, errs := validateSeason(form, testLocation, testNow, true)
	if msg := errs.For(viewmodel.FieldVoteClosesAt); msg == "" {
		t.Fatal("a season whose voting closes before its board shuts was accepted")
	}
}

// Equal is not "in order" either: a board that shuts at the same instant
// voting closes gives nobody a chance to rank what went up.
func TestTheTwoDeadlinesMayNotBeTheSameMoment(t *testing.T) {
	form := goodForm()
	form.VoteClosesAt = form.SubmitClosesAt

	_, errs := validateSeason(form, testLocation, testNow, true)
	if msg := errs.For(viewmodel.FieldVoteClosesAt); msg == "" {
		t.Error("two identical deadlines were accepted")
	}
}

// TestBothDeadlinesMustBeInTheFutureAtCreation, and the message has to be one
// a person can act on rather than a constraint name.
func TestBothDeadlinesMustBeInTheFutureAtCreation(t *testing.T) {
	form := goodForm()
	form.SubmitClosesAt = "2020-10-24T20:00"
	form.VoteClosesAt = "2020-10-31T20:00"

	_, errs := validateSeason(form, testLocation, testNow, true)
	for _, field := range []string{viewmodel.FieldSubmitClosesAt, viewmodel.FieldVoteClosesAt} {
		if errs.For(field) == "" {
			t.Errorf("a past %s was accepted at creation", field)
		}
	}
}

// TestEditingAllowsADeadlineThatHasPassed is the asymmetry, and it is the
// whole reason requireFuture is a parameter.
//
// A season being re-dated in the voting week has a submission deadline behind
// it by definition. Refusing the edit because of it would make this screen
// useless exactly when an admin needs it.
func TestEditingAllowsADeadlineThatHasPassed(t *testing.T) {
	form := goodForm()
	form.SubmitClosesAt = "2026-09-01T20:00" // already gone, relative to testNow
	form.VoteClosesAt = "2026-09-30T20:00"

	if _, errs := validateSeason(form, testLocation, testNow, true); !errs.Any() {
		t.Fatal("creation accepted a deadline in the past; this test's premise is wrong")
	}
	if _, errs := validateSeason(form, testLocation, testNow, false); errs.Any() {
		t.Errorf("editing refused a deadline that has already passed: %v", errs)
	}
}

// Ordering still holds when editing. The calendar moving on is not a reason to
// let the board shut after the ballots close.
func TestEditingStillEnforcesTheOrdering(t *testing.T) {
	form := goodForm()
	form.SubmitClosesAt = "2026-09-30T20:00"
	form.VoteClosesAt = "2026-09-01T20:00"

	if _, errs := validateSeason(form, testLocation, testNow, false); errs.For(viewmodel.FieldVoteClosesAt) == "" {
		t.Error("editing accepted deadlines in the wrong order")
	}
}

func TestBothDeadlinesAreRequired(t *testing.T) {
	for _, field := range []string{viewmodel.FieldSubmitClosesAt, viewmodel.FieldVoteClosesAt} {
		t.Run(field, func(t *testing.T) {
			form := goodForm()
			switch field {
			case viewmodel.FieldSubmitClosesAt:
				form.SubmitClosesAt = ""
			case viewmodel.FieldVoteClosesAt:
				form.VoteClosesAt = ""
			}

			_, errs := validateSeason(form, testLocation, testNow, true)
			if errs.For(field) == "" {
				t.Errorf("%s was allowed to be blank; the ticket makes both first-class required fields", field)
			}
		})
	}
}

// TestSubmissionsOpenIsOptional: blank is not an error, and it is how an admin
// says "open it now".
func TestSubmissionsOpenIsOptional(t *testing.T) {
	form := goodForm()
	form.SubmitOpensAt = ""

	if _, errs := validateSeason(form, testLocation, testNow, true); errs.For(viewmodel.FieldSubmitOpensAt) != "" {
		t.Errorf("a blank optional opening date was refused: %v", errs)
	}
}

func TestSubmissionsCannotOpenAfterTheyClose(t *testing.T) {
	form := goodForm()
	form.SubmitOpensAt = "2027-11-01T20:00"

	if _, errs := validateSeason(form, testLocation, testNow, true); errs.For(viewmodel.FieldSubmitOpensAt) == "" {
		t.Error("submissions were allowed to open after they close")
	}
}

func TestTheYearIsCheckedAgainstTheSchemasRange(t *testing.T) {
	cases := map[string]string{
		"blank":      "",
		"words":      "twenty twenty seven",
		"too early":  "1600",
		"too late":   "3000",
		"a fragment": "20x7",
	}
	for name, year := range cases {
		t.Run(name, func(t *testing.T) {
			form := goodForm()
			form.Year = year

			if _, errs := validateSeason(form, testLocation, testNow, true); errs.For(viewmodel.FieldSeasonYear) == "" {
				t.Errorf("year %q was accepted", year)
			}
		})
	}
}

func TestTheNameIsRequired(t *testing.T) {
	form := goodForm()
	form.Name = "   "

	if _, errs := validateSeason(form, testLocation, testNow, true); errs.For(viewmodel.FieldSeasonName) == "" {
		t.Error("a blank name was accepted; season_name_not_blank would have refused it as a 500")
	}
}

// TestAZeroSubmitLimitIsAllowed: it is unusual and it is a real choice -- a
// season where nobody submits. The schema allows it (>= 0), so the form must
// not be stricter than the column for no reason.
func TestAZeroSubmitLimitIsAllowed(t *testing.T) {
	form := goodForm()
	form.SubmitLimit = "0"

	d, errs := validateSeason(form, testLocation, testNow, true)
	if errs.Any() {
		t.Fatalf("a limit of 0 was refused: %v", errs)
	}
	if d.submitLimit != 0 {
		t.Errorf("submit limit = %d, want 0", d.submitLimit)
	}
}

func TestTheSubmitLimitIsChecked(t *testing.T) {
	for _, limit := range []string{"", "two", "-1", "999"} {
		t.Run(limit, func(t *testing.T) {
			form := goodForm()
			form.SubmitLimit = limit

			if _, errs := validateSeason(form, testLocation, testNow, true); errs.For(viewmodel.FieldSeasonSubmitLimit) == "" {
				t.Errorf("submit limit %q was accepted", limit)
			}
		})
	}
}

// TestOnlyDraftAndSubmittingAreOffered is the guard that keeps the form from
// producing a state the CHECKs refuse.
//
// 'locked' is the case that matters: season_state_valid would accept it, and
// season_locked_has_timestamp would then refuse the row for having no
// locked_at -- as a 500, on a form, with no sentence attached.
func TestOnlyDraftAndSubmittingAreOffered(t *testing.T) {
	for _, state := range []string{"locked", "voting", "banana"} {
		t.Run(state, func(t *testing.T) {
			form := goodForm()
			form.State = state

			if _, errs := validateSeason(form, testLocation, testNow, true); errs.For(viewmodel.FieldSeasonState) == "" {
				t.Errorf("state %q was accepted", state)
			}
		})
	}
}

// A blank state is the markup and this function disagreeing, not somebody
// choosing nothing: the control is a pair of radios with one pre-selected. The
// ordinary case is the right default.
func TestABlankStateDefaultsToOpeningTheSeason(t *testing.T) {
	form := goodForm()
	form.State = ""

	d, errs := validateSeason(form, testLocation, testNow, true)
	if errs.Any() {
		t.Fatalf("a blank state was refused: %v", errs)
	}
	if d.state != stateSubmitting {
		t.Errorf("state = %q, want %q", d.state, stateSubmitting)
	}
}

// TestThereIsNoVotingStartField is nap-h3l written down as a test.
//
// Voting opens on a COUNT -- three films -- concurrently with submissions
// still coming in. A voting-start date would be a second, contradictory answer
// to a question the model already answers, and the two would disagree in the
// one week of the year anybody is looking.
//
// It walks the form's fields by reflection rather than asserting on
// behaviour, because the thing to prevent is somebody helpfully ADDING the
// field. A test of what the form does could not notice that.
func TestThereIsNoVotingStartField(t *testing.T) {
	banned := []string{"voteopens", "votingopens", "votestarts", "votingstarts", "ballotopens"}

	form := reflect.TypeOf(viewmodel.AdminSeasonForm{})
	for i := range form.NumField() {
		name := strings.ToLower(form.Field(i).Name)
		for _, bad := range banned {
			if strings.Contains(name, bad) {
				t.Errorf("AdminSeasonForm has a field %q, which is a voting-START date. "+
					"Read nap-h3l: ranking opens once three films exist, not on a date an admin sets.",
					form.Field(i).Name)
			}
		}
	}

	// The same for the posted field names, since a hand-built form could carry
	// one even if the struct did not.
	for _, field := range []string{
		viewmodel.FieldSeasonYear, viewmodel.FieldSeasonName,
		viewmodel.FieldSubmitOpensAt, viewmodel.FieldSubmitClosesAt,
		viewmodel.FieldVoteClosesAt, viewmodel.FieldSeasonSubmitLimit,
		viewmodel.FieldSeasonState,
	} {
		flat := strings.ReplaceAll(field, "_", "")
		for _, bad := range banned {
			if strings.Contains(flat, bad) {
				t.Errorf("a posted field is named %q, which reads as a voting-START date", field)
			}
		}
	}
}

// TestParsingAcceptsTheSecondsForm: a browser may include seconds when the
// control has a step, and rejecting that as unparseable would be a form that
// works in one browser.
func TestParsingAcceptsTheSecondsForm(t *testing.T) {
	form := goodForm()
	form.SubmitClosesAt = "2027-10-24T20:00:00"

	if _, errs := validateSeason(form, testLocation, testNow, true); errs.For(viewmodel.FieldSubmitClosesAt) != "" {
		t.Errorf("the seconds form was refused: %v", errs)
	}
}

func TestAnUnparseableDateIsAFormErrorNotACrash(t *testing.T) {
	form := goodForm()
	form.VoteClosesAt = "next Tuesday"

	if _, errs := validateSeason(form, testLocation, testNow, true); errs.For(viewmodel.FieldVoteClosesAt) == "" {
		t.Error("an unparseable date was accepted")
	}
}

// TestTheErrorSummaryReadsDownTheForm: WCAG 3.3.1 wants the summary in the
// form's own order, and viewmodel.FieldErrors is a slice so that order is
// stable. This asserts the page order rather than the order the checks happen
// to run in.
func TestTheErrorSummaryReadsDownTheForm(t *testing.T) {
	form := viewmodel.AdminSeasonForm{} // everything wrong at once

	_, errs := validateSeason(form, testLocation, testNow, true)
	form.Errors = errs

	fields := newSeasonFields(viewmodel.AdminSeasonFormPage{Form: form})
	bad := fields.Invalid()
	if len(bad) < 4 {
		t.Fatalf("expected an empty form to fail several fields, got %d", len(bad))
	}

	want := []string{
		viewmodel.FieldSeasonYear,
		viewmodel.FieldSeasonName,
		viewmodel.FieldSubmitClosesAt,
		viewmodel.FieldVoteClosesAt,
		viewmodel.FieldSeasonSubmitLimit,
	}
	var got []string
	for _, f := range bad {
		got = append(got, f.Name)
	}

	// got must be want with nothing reordered; extra entries are fine.
	i := 0
	for _, name := range got {
		if i < len(want) && name == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Errorf("the summary is not in page order: got %v, expected these in this order: %v", got, want)
	}
}

// TestALockedControlStillShowsItsValueAndSaysWhy. A disabled field that looks
// identical to an enabled one is one people type into.
func TestALockedControlStillShowsItsValueAndSaysWhy(t *testing.T) {
	page := viewmodel.AdminSeasonFormPage{Form: goodForm(), Editing: true}
	fields := newSeasonFields(page)

	if !fields.Year.Locked {
		t.Error("the year is editable on the re-date screen")
	}
	if fields.Year.Value != "2027" {
		t.Errorf("the locked year lost its value: %q", fields.Year.Value)
	}
	if !strings.Contains(fields.Year.Hint, "cannot be changed now") {
		t.Errorf("the locked year's hint does not say it is locked: %q", fields.Year.Hint)
	}
	if !fields.State.Locked {
		t.Error("the state is editable on the re-date screen; moving a season through its phases is a different operation")
	}

	// And not locked when creating.
	fresh := newSeasonFields(viewmodel.AdminSeasonFormPage{Form: goodForm()})
	if fresh.Year.Locked || fresh.State.Locked {
		t.Error("the create screen locked a control it should not have")
	}
}

// TestEveryFieldPointsAtItsOwnIds. The label, the hint, the error and the
// summary link all have to agree, and when they do not the failure is silent:
// the message is on screen, correctly styled, and never announced.
func TestEveryFieldPointsAtItsOwnIds(t *testing.T) {
	form := goodForm()
	form.Errors.Add(viewmodel.FieldSeasonName, "no")

	fields := newSeasonFields(viewmodel.AdminSeasonFormPage{Form: form})

	name := fields.Name
	switch {
	case name.HintID() != name.ID+"-hint":
		t.Errorf("hint id = %q", name.HintID())
	case name.ErrorID() != name.ID+"-error":
		t.Errorf("error id = %q", name.ErrorID())
	case name.DescribedBy() != name.HintID()+" "+name.ErrorID():
		t.Errorf("aria-describedby = %q, want the hint then the error", name.DescribedBy())
	case name.ErrorHref() != "#"+name.ID:
		t.Errorf("summary link = %q", name.ErrorHref())
	case name.AriaInvalid() != "true":
		t.Errorf("aria-invalid = %q, want true", name.AriaInvalid())
	}

	// A clean field says false out loud rather than omitting the attribute.
	if fields.Year.AriaInvalid() != "false" {
		t.Errorf("a clean field's aria-invalid = %q, want false", fields.Year.AriaInvalid())
	}
	if fields.Year.ErrorID() != "" {
		t.Error("a clean field points at an error element that is not rendered")
	}
}
