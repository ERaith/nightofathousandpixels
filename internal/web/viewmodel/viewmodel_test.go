package viewmodel_test

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// The rule this package exists to enforce is that nothing store-shaped crosses
// the seam, and a rule kept by discipline alone lasts until the first hurried
// afternoon. This test makes it a build failure instead.
//
// It parses this package's own source rather than using reflection, because
// the thing to catch is the import — by the time a pgtype has reached a struct
// field, someone has already written the handler that put it there.
func TestPackageImportsNothingFromTheDatabaseSide(t *testing.T) {
	t.Parallel()

	// Any import whose path contains one of these is the seam leaking. uuid is
	// on the list because MovieCard.ID is deliberately a string: a template
	// only ever prints it, and a uuid.UUID here would be the first crack.
	banned := []string{
		"internal/store",
		"internal/auth",
		"jackc/pgx",
		"pgtype",
		"sqlc",
		"database/sql",
		"google/uuid",
		"net/http",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()

	var checked int

	for _, entry := range entries {
		name := entry.Name()

		// Test files are allowed their own imports; they are not part of the
		// contract that ships.
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		checked++

		for _, imp := range file.Imports {
			got := strings.Trim(imp.Path.Value, `"`)

			for _, bad := range banned {
				if strings.Contains(got, bad) {
					t.Errorf("%s imports %q: the view model must stay plain Go, "+
						"or handlers and templates stop being able to move independently",
						name, got)

					// One report per import: several patterns can match the
					// same path, and pgx/v5/pgtype matching twice reads like
					// two problems.
					break
				}
			}
		}
	}

	// Without this the test passes just as cheerfully on zero files, and a
	// guard that cannot fail is worse than no guard: it reads as protection
	// nobody has.
	if checked == 0 {
		t.Fatal("parsed no source files; this test was not actually checking anything")
	}
}

// The three-way distinction is the whole reason SubmitQuota is not an int.
//
// The database answers the limit with
// COALESCE(season_member.submit_limit, season.default_submit_limit), where an
// explicit 0 means "may vote, may not submit" and no row at all means "not a
// member of this season". Those are different answers, and both have Limit 0
// on this side, so only Known keeps them apart.
func TestSubmitQuotaKeepsTheThreeAnswersApart(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		quota        viewmodel.SubmitQuota
		wantHasRoom  bool
		wantBarred   bool
		wantAtLimit  bool
		wantRemains  int
		wantLabelSub string
	}{
		"no quota here: signed out or not a member": {
			quota:       viewmodel.FixtureQuotaUnknown(),
			wantRemains: 0,
			// No quota to describe means no sentence, not a misleading one.
			wantLabelSub: "",
		},
		"explicit zero: may vote, may not submit": {
			quota:        viewmodel.FixtureQuotaBarred(),
			wantBarred:   true,
			wantRemains:  0,
			wantLabelSub: "not put films on the board",
		},
		"room left": {
			quota:        viewmodel.FixtureQuota(),
			wantHasRoom:  true,
			wantRemains:  1,
			wantLabelSub: "1 pick left of 2.",
		},
		"picks used up": {
			quota:        viewmodel.FixtureQuotaAtLimit(),
			wantAtLimit:  true,
			wantRemains:  0,
			wantLabelSub: "used all 2 picks",
		},
		"limit lowered after submitting": {
			quota:        viewmodel.FixtureQuotaOverLimit(),
			wantAtLimit:  true,
			wantRemains:  0,
			wantLabelSub: "used all 2 picks",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tc.quota.HasRoom(); got != tc.wantHasRoom {
				t.Errorf("HasRoom() = %v, want %v", got, tc.wantHasRoom)
			}
			if got := tc.quota.Barred(); got != tc.wantBarred {
				t.Errorf("Barred() = %v, want %v", got, tc.wantBarred)
			}
			if got := tc.quota.AtLimit(); got != tc.wantAtLimit {
				t.Errorf("AtLimit() = %v, want %v", got, tc.wantAtLimit)
			}
			if got := tc.quota.Remaining(); got != tc.wantRemains {
				t.Errorf("Remaining() = %d, want %d", got, tc.wantRemains)
			}

			label := tc.quota.Label()
			if tc.wantLabelSub == "" {
				if label != "" {
					t.Errorf("Label() = %q, want empty", label)
				}
			} else if !strings.Contains(label, tc.wantLabelSub) {
				t.Errorf("Label() = %q, want it to contain %q", label, tc.wantLabelSub)
			}
		})
	}
}

// Barred and AtLimit must never both be true, and they must never be confused
// for each other: one is a setting somebody chose, the other is progress the
// viewer made, and they need different copy.
func TestBarredAndAtLimitAreDifferentStates(t *testing.T) {
	t.Parallel()

	barred := viewmodel.FixtureQuotaBarred()
	atLimit := viewmodel.FixtureQuotaAtLimit()

	if barred.AtLimit() {
		t.Error("a barred member reads as having used their picks; they never had any")
	}
	if atLimit.Barred() {
		t.Error("a member who used their picks reads as barred; they were not")
	}
	if barred.Label() == atLimit.Label() {
		t.Errorf("both states produce the same sentence %q; they need different copy", barred.Label())
	}
}

// The zero value has to be the safe one. A struct nobody populated must read
// as "no allowance", so that forgetting to fill it in hides a submit button
// rather than showing one the handler then refuses.
func TestZeroQuotaIsTheSafeOne(t *testing.T) {
	t.Parallel()

	var zero viewmodel.SubmitQuota

	if zero.HasRoom() {
		t.Error("an unpopulated quota reports room to submit")
	}
	if zero.Barred() {
		t.Error("an unpopulated quota reports a deliberate ban; it is simply unknown")
	}
	if zero.Label() != "" {
		t.Errorf("an unpopulated quota produced the sentence %q", zero.Label())
	}
}

// An unscheduled window is a real state: all four columns in migration 00002
// are nullable, so a season can exist before its dates are decided. Every
// deadline line has to disappear rather than print a label with nothing after
// it.
func TestUnscheduledWindowsRenderNothing(t *testing.T) {
	t.Parallel()

	season := viewmodel.FixtureSeasonUnscheduled()

	for label, got := range map[string]string{
		"submit opens": season.SubmitOpensLabel(),
		"vote opens":   season.VoteOpensLabel(),
		"vote closes":  season.VoteClosesLabel(),
		"locked":       season.LockedLabel(),
	} {
		if got != "" {
			t.Errorf("%s rendered %q for an unset date; it must render nothing", label, got)
		}
	}

	if season.Label() == "" {
		t.Error("an unscheduled season still has a name and must still have a heading")
	}
}

// FormatDeadline reads the time it is handed and never the clock, which is
// what makes it safe to call from a template and deterministic in a test.
func TestFormatDeadlineIsDeterministic(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 2, 21, 0, 0, 0, time.UTC)

	const want = "Friday 2 October, 9:00 PM"
	if got := viewmodel.FormatDeadline(at); got != want {
		t.Errorf("FormatDeadline() = %q, want %q", got, want)
	}

	if got := viewmodel.FormatDeadline(time.Time{}); got != "" {
		t.Errorf("the zero time rendered %q; it means the date is not set", got)
	}
}

// The fallbacks are not decoration. A whitelisted person who has never signed
// in has a blank display_name, and "submitted by" with nothing after it reads
// like a bug.
func TestBlankNamesFallBack(t *testing.T) {
	t.Parallel()

	if got := viewmodel.FixtureNamelessUser().Label(); got == "" {
		t.Error("a user with no display name has no label at all")
	}

	var nilUser *viewmodel.CurrentUser
	if got := nilUser.Label(); got != "" {
		t.Errorf("a nil user labelled itself %q", got)
	}

	nameless := viewmodel.FixtureLongTitleMovie()
	if nameless.SubmittedBy != "" {
		t.Fatal("this fixture is supposed to have no submitter name")
	}
	if got := nameless.SubmitterLabel(); got == "" {
		t.Error("a movie with no submitter name credits nobody at all")
	}
	if nameless.HasYear() {
		t.Error("this fixture is supposed to have no release year")
	}
	if got := nameless.TitleLine(); strings.Contains(got, "(") {
		t.Errorf("TitleLine() = %q; an unknown year must not render empty parentheses", got)
	}
}

// A trailer link and an embeddable trailer are different things. Most links
// cannot be embedded, so a card with a link and no embed is the common case,
// and it has to offer the link rather than an empty player.
func TestTrailerLinkAndEmbedAreSeparate(t *testing.T) {
	t.Parallel()

	embeddable := viewmodel.FixtureMovie()
	if !embeddable.HasTrailer() || !embeddable.CanEmbedTrailer() {
		t.Error("the ordinary fixture should both link and embed its trailer")
	}

	linkOnly := viewmodel.FixtureUnembeddableTrailerMovie()
	if !linkOnly.HasTrailer() {
		t.Error("this fixture has a trailer link and should report one")
	}
	if linkOnly.CanEmbedTrailer() {
		t.Error("a Vimeo page is not embeddable; the card would render an empty player")
	}

	none := viewmodel.FixtureNoTrailerMovie()
	if none.HasTrailer() || none.CanEmbedTrailer() {
		t.Error("a movie with no trailer reports one")
	}
	if none.HasDescription() {
		t.Error("this fixture is supposed to carry the minimum a movie row can")
	}
}

// The empty slate is the most-seen state of the whole site and the easiest to
// ship broken, because it is the one nobody builds against. It has to be a
// season that is open and inviting, not a page that failed to load.
func TestEmptySlateIsInvitingRatherThanBroken(t *testing.T) {
	t.Parallel()

	page := viewmodel.FixtureEmptySlate()

	if page.HasMovies() || page.MovieCount() != 0 {
		t.Fatal("the empty slate fixture is not empty")
	}
	if !page.CanSubmit {
		t.Error("the empty slate must offer the submit control; it is the whole call to action")
	}
	if page.SubmitHref == "" {
		t.Error("the empty slate offers a submit control that points nowhere")
	}
	if page.Season.State != viewmodel.SeasonSubmitting {
		t.Error("the empty slate should be a season in its submission window")
	}
	if page.Season.State.Summary() == "" {
		t.Error("the empty slate has no line of copy saying what to do")
	}
	if page.IsFinished() {
		t.Error("the empty slate reads as a finished season")
	}
}

// A locked season is immutable in the database — migration 00008 enforces it
// with triggers — so the page must not offer what the database will refuse.
func TestLockedSeasonOffersNothingActionable(t *testing.T) {
	t.Parallel()

	for name, page := range map[string]viewmodel.SlatePage{
		"locked":      viewmodel.FixtureLockedSlate(),
		"locked tied": viewmodel.FixtureLockedSlateTied(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if !page.IsFinished() {
				t.Error("a locked season does not report itself finished")
			}
			if page.CanSubmit {
				t.Error("a locked season offers a submit control the database will refuse")
			}
			if page.Result == nil {
				t.Fatal("a tallied locked season has no result to show")
			}
			if page.Result.TalliedLabel() == "" {
				t.Error("the result does not say when it was counted")
			}
		})
	}

	// A tie means result.winner_movie_id is NULL: no winning card to badge.
	tied := viewmodel.FixtureLockedSlateTied()
	if !tied.Result.Tied || tied.Result.WinnerTitle != "" {
		t.Error("the tied fixture names a winner")
	}

	for _, movie := range tied.Movies {
		if movie.IsWinner {
			t.Error("a tied season badges a winning card")
		}
	}

	won := viewmodel.FixtureLockedSlate()
	if won.Result.Tied || won.Result.WinnerTitle == "" {
		t.Error("the won fixture does not name a winner")
	}
}

// A rejected form has to come back carrying exactly what the person typed,
// including a year that is not a number. That is why SubmitForm.Year is a
// string: an int would have nowhere to put "nineteen eighty four" and the box
// would redisplay empty, which looks like the site ate the input.
func TestRejectedFormCarriesBackWhatWasTyped(t *testing.T) {
	t.Parallel()

	form := viewmodel.FixtureSubmitWithErrors().Form

	if !form.Errors.Any() {
		t.Fatal("the rejected-form fixture carries no errors")
	}
	if form.Year == "" {
		t.Error("the year the person typed was not carried back")
	}
	if form.Description == "" {
		t.Error("the description survived validation and must be carried back too")
	}

	// Errors sit against the field they belong to, so each one can render
	// beside its own input and drive aria-invalid.
	for _, field := range []string{viewmodel.FieldTitle, viewmodel.FieldYear, viewmodel.FieldTrailerURL} {
		if !form.Errors.Has(field) {
			t.Errorf("no error recorded against %q", field)
		}
		if form.Errors.For(field) == "" {
			t.Errorf("the error against %q has no message", field)
		}
	}

	if form.Errors.Has(viewmodel.FieldDescription) {
		t.Error("an error was recorded against a field that was fine")
	}

	// The order is stable, which a map would not be: an accessible error
	// summary links to each bad field and has to read in the form's own order
	// every time.
	if form.Errors[0].Field != viewmodel.FieldTitle {
		t.Error("the error summary does not lead with the first bad field")
	}

	// One field carries one message; a second Add replaces rather than appends.
	errs := form.Errors
	before := len(errs)
	errs.Add(viewmodel.FieldTitle, "Try again.")

	if len(errs) != before {
		t.Errorf("Add on an existing field grew the list from %d to %d", before, len(errs))
	}
	if errs.For(viewmodel.FieldTitle) != "Try again." {
		t.Error("Add did not replace the message already against the field")
	}
}

// Every fixture has to be a fresh value. They are functions rather than
// package-level variables precisely so that a test which edits one cannot
// poison the next test that asks for it.
func TestFixturesAreNotShared(t *testing.T) {
	t.Parallel()

	first := viewmodel.FixtureMovies()
	first[0].Title = "clobbered"
	first = append(first, viewmodel.FixtureHiddenMovie())
	_ = first

	second := viewmodel.FixtureMovies()
	if second[0].Title == "clobbered" {
		t.Error("editing a fixture changed the next one; they share backing state")
	}
	if len(second) != len(viewmodel.FixtureMovies()) {
		t.Error("appending to a fixture changed its length for the next caller")
	}

	slate := viewmodel.FixtureFullSlate()
	slate.Layout.Flashes = nil
	slate.Movies[0].IsWinner = true

	if viewmodel.FixtureFullSlate().Layout.Flashes == nil {
		t.Error("clearing one slate's flashes cleared them for every caller")
	}
	if viewmodel.FixtureFullSlate().Movies[0].IsWinner {
		t.Error("badging one slate's winner badged it for every caller")
	}
}

// A public slate never receives a hidden movie: ListVisibleMoviesForSeason
// filters them out. Only the admin view sees them, and it has to say so.
func TestHiddenMoviesOnlyReachTheAdminView(t *testing.T) {
	t.Parallel()

	for name, page := range viewmodel.FixtureSlates() {
		if name == "admin" {
			continue
		}

		for _, movie := range page.Movies {
			if movie.Hidden {
				t.Errorf("the %q slate carries a hidden movie %q", name, movie.Title)
			}
		}

		if page.ShowingHidden {
			t.Errorf("the %q slate claims to be showing hidden submissions", name)
		}
	}

	admin := viewmodel.FixtureAdminSlate()
	if !admin.ShowingHidden {
		t.Error("the admin slate does not announce that it includes withdrawn submissions")
	}

	var found bool

	for _, movie := range admin.Movies {
		if movie.Hidden {
			found = true
		}
	}

	if !found {
		t.Error("the admin slate has no hidden submission to show")
	}
}

// Every season state needs a badge and a line of copy. A fifth value appearing
// in the database would otherwise render as a blank label rather than failing
// loudly.
func TestEverySeasonStateHasCopy(t *testing.T) {
	t.Parallel()

	for _, state := range []viewmodel.SeasonState{
		viewmodel.SeasonDraft,
		viewmodel.SeasonSubmitting,
		viewmodel.SeasonVoting,
		viewmodel.SeasonLocked,
	} {
		if state.Label() == "" {
			t.Errorf("state %q has no badge label", state)
		}
		if state.Summary() == "" {
			t.Errorf("state %q has no summary copy", state)
		}
	}

	if !viewmodel.SeasonLocked.IsLocked() {
		t.Error("the locked state does not report itself locked")
	}

	for _, live := range []viewmodel.SeasonState{
		viewmodel.SeasonDraft,
		viewmodel.SeasonSubmitting,
		viewmodel.SeasonVoting,
	} {
		if live.IsLocked() {
			t.Errorf("state %q reports itself locked", live)
		}
	}
}
