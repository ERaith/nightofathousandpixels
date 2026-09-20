package viewmodel_test

import (
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// The zero Theme is what a handler passes before anything is configured, and it
// has to render the whole site.
func TestTheZeroThemeAnswersEveryKey(t *testing.T) {
	var zero viewmodel.Theme

	for key, want := range viewmodel.BaseCopy {
		if strings.Contains(want, "{") {
			// Placeholder strings are checked in their own test; here we only
			// care that something came back.
			if got := zero.Text(key); got == "" {
				t.Errorf("%s rendered empty", key)
			}

			continue
		}
		if got := zero.Text(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

// A pack sets what it cares about and inherits the rest. There is no such thing
// as a missing string.
func TestAPackInheritsWhatItDoesNotSet(t *testing.T) {
	th := viewmodel.Theme{Copy: viewmodel.Copy{
		viewmodel.KeySlateEmptyHeading: "Nobody has volunteered yet",
	}}

	if got := th.Text(viewmodel.KeySlateEmptyHeading); got != "Nobody has volunteered yet" {
		t.Errorf("the pack's own string lost: %q", got)
	}
	if got, want := th.Text(viewmodel.KeySlateEmptyCTA), viewmodel.BaseCopy[viewmodel.KeySlateEmptyCTA]; got != want {
		t.Errorf("an unset key did not fall back: %q, want %q", got, want)
	}
}

// A pack string that is present but empty is a choice the pack made. Reviving
// the base string would make it impossible to suppress anything.
func TestAnEmptyPackStringIsAChoiceNotAnAbsence(t *testing.T) {
	th := viewmodel.Theme{Copy: viewmodel.Copy{viewmodel.KeySlateHiddenLede: ""}}

	if got := th.Text(viewmodel.KeySlateHiddenLede); got != "" {
		t.Errorf("an empty override was overruled: %q", got)
	}
}

// A key the catalogue does not know renders as the key. A key name on the page
// is ugly and findable; an empty heading is invisible and ships.
func TestAnUnknownKeyRendersAsItself(t *testing.T) {
	var zero viewmodel.Theme

	if got := zero.Text("slate.empty.heading.typo"); got != "slate.empty.heading.typo" {
		t.Errorf("Text = %q", got)
	}
}

func TestPlaceholdersAreSubstitutedByName(t *testing.T) {
	th := viewmodel.Theme{Copy: viewmodel.Copy{
		"x": "{season} is filed. Evaluation ran {date}.",
	}}

	got := th.Text("x", viewmodel.PlaceholderDate, "18 October", viewmodel.PlaceholderSeason, "2026")
	if want := "2026 is filed. Evaluation ran 18 October."; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
}

// A pack author writing {seasons} gets {seasons} on the page: visible, and
// findable. A sentence with a hole in it is neither.
func TestAnUnknownPlaceholderIsLeftAlone(t *testing.T) {
	th := viewmodel.Theme{Copy: viewmodel.Copy{"x": "{seasons} is filed."}}

	if got := th.Text("x", viewmodel.PlaceholderSeason, "2026"); got != "{seasons} is filed." {
		t.Errorf("Text = %q", got)
	}
}

// This runs during a render. A half-written call is not worth a 500 on a page
// that would otherwise be fine.
func TestAnOddTrailingArgumentIsIgnoredRatherThanPanicking(t *testing.T) {
	th := viewmodel.Theme{Copy: viewmodel.Copy{"x": "{season} is filed."}}

	if got := th.Text("x", viewmodel.PlaceholderSeason, "2026", viewmodel.PlaceholderDate); got != "2026 is filed." {
		t.Errorf("Text = %q", got)
	}
}

func TestCountUsesThePacksNoun(t *testing.T) {
	th := viewmodel.Theme{Copy: viewmodel.Copy{
		viewmodel.KeyFilmOne:  "test chamber",
		viewmodel.KeyFilmMany: "test chambers",
	}}

	cases := map[int]string{0: "0 test chambers", 1: "1 test chamber", 6: "6 test chambers"}
	for n, want := range cases {
		if got := th.Count(n, viewmodel.KeyFilmOne, viewmodel.KeyFilmMany); got != want {
			t.Errorf("Count(%d) = %q, want %q", n, got, want)
		}
	}
}

// QuotaLabel with no pack has to be exactly SubmitQuota.Label, because Label is
// what every existing caller and test uses and the two must not drift.
func TestQuotaLabelWithNoPackIsTheViewModelSentence(t *testing.T) {
	quotas := []viewmodel.SubmitQuota{
		viewmodel.FixtureQuota(),
		viewmodel.FixtureQuotaAtLimit(),
		viewmodel.FixtureQuotaBarred(),
		viewmodel.FixtureQuotaUnknown(),
		viewmodel.FixtureQuotaOverLimit(),
		{},
	}

	var zero viewmodel.Theme
	for _, q := range quotas {
		if got, want := zero.QuotaLabel(q), q.Label(); got != want {
			t.Errorf("QuotaLabel = %q, Label = %q; they have to agree with no pack loaded", got, want)
		}
	}
}

// The cap refusal is one of the few sentences on this site somebody reads
// carefully, because it is the one telling them no. A pack owns the sentence;
// the view model owns the number in it.
func TestAPackRewritesTheCapRefusalButNotTheNumber(t *testing.T) {
	th := viewmodel.Theme{Copy: viewmodel.Copy{
		viewmodel.KeyQuotaAtLimit: "You have entered all {count} permitted this cycle.",
		viewmodel.KeyPickOne:      "proposal",
		viewmodel.KeyPickMany:     "proposals",
	}}

	q := viewmodel.FixtureQuotaAtLimit()
	got := th.QuotaLabel(q)
	if want := "You have entered all 2 proposals permitted this cycle."; got != want {
		t.Errorf("QuotaLabel = %q, want %q", got, want)
	}
	if !strings.Contains(got, "2") {
		t.Errorf("the real limit did not survive the pack's sentence: %q", got)
	}
}

// The badge and the line under the slate's heading are the top of the page the
// group-chat link opens. A pack that cannot touch them stops at the fold.
func TestAPackRewritesTheSeasonState(t *testing.T) {
	th := viewmodel.Theme{Copy: viewmodel.Copy{
		viewmodel.KeyStateSubmittingLabel:   "Intake open",
		viewmodel.KeyStateSubmittingSummary: "Enter your candidates.",
	}}

	if got := th.SeasonStateLabel(viewmodel.SeasonSubmitting); got != "Intake open" {
		t.Errorf("label = %q", got)
	}
	if got := th.SeasonStateSummary(viewmodel.SeasonSubmitting); got != "Enter your candidates." {
		t.Errorf("summary = %q", got)
	}
	// Not overridden by this pack.
	if got, want := th.SeasonStateLabel(viewmodel.SeasonLocked), viewmodel.BaseCopy[viewmodel.KeyStateLockedLabel]; got != want {
		t.Errorf("locked label = %q, want %q", got, want)
	}
}

func TestSeasonStateWithNoPackMatchesTheViewModelMethods(t *testing.T) {
	var zero viewmodel.Theme

	states := []viewmodel.SeasonState{
		viewmodel.SeasonDraft, viewmodel.SeasonSubmitting,
		viewmodel.SeasonVoting, viewmodel.SeasonLocked,
		viewmodel.SeasonState("something_new"),
	}
	for _, s := range states {
		if got, want := zero.SeasonStateLabel(s), s.Label(); got != want {
			t.Errorf("%s: label %q != %q", s, got, want)
		}
		if got, want := zero.SeasonStateSummary(s), s.Summary(); got != want {
			t.Errorf("%s: summary %q != %q", s, got, want)
		}
	}
}

// Resolve prefers the pack's copy for the two strings that are also fields, so
// a manifest says each of them once.
func TestResolvePrefersPackCopyForTheTitleAndTagline(t *testing.T) {
	th := viewmodel.Theme{Copy: viewmodel.Copy{
		viewmodel.KeySiteTitle:   "A Thousand Pixels After Dark",
		viewmodel.KeySiteTagline: "Unpleasant dreams.",
	}}.Resolve()

	if th.SiteTitle != "A Thousand Pixels After Dark" {
		t.Errorf("SiteTitle = %q", th.SiteTitle)
	}
	if th.Tagline != "Unpleasant dreams." {
		t.Errorf("Tagline = %q", th.Tagline)
	}
}

// An explicit field still wins over the copy key: a caller that says
// Theme{SiteTitle: "..."} meant it.
func TestAnExplicitFieldBeatsThePacksCopy(t *testing.T) {
	th := viewmodel.Theme{
		SiteTitle: "Set by the caller",
		Copy:      viewmodel.Copy{viewmodel.KeySiteTitle: "Set by the pack"},
	}.Resolve()

	if th.SiteTitle != "Set by the caller" {
		t.Errorf("SiteTitle = %q", th.SiteTitle)
	}
}

func TestResolveWithNothingSetIsTheUnthemedSite(t *testing.T) {
	th := viewmodel.Theme{}.Resolve()

	if th.SiteTitle != viewmodel.DefaultSiteTitle {
		t.Errorf("SiteTitle = %q", th.SiteTitle)
	}
	if th.Tagline != viewmodel.DefaultTagline {
		t.Errorf("Tagline = %q", th.Tagline)
	}
	if th.PackCSS != viewmodel.DefaultPackCSS {
		t.Errorf("PackCSS = %q", th.PackCSS)
	}
}

func TestIsSet(t *testing.T) {
	if (viewmodel.Theme{}).IsSet() {
		t.Errorf("the zero Theme reported itself as configured")
	}
	if !(viewmodel.Theme{Copy: viewmodel.Copy{"a": "b"}}).IsSet() {
		t.Errorf("a theme with copy reported itself as unconfigured")
	}
	if !(viewmodel.Theme{SiteTitle: "x"}).IsSet() {
		t.Errorf("a theme with a title reported itself as unconfigured")
	}
}

// Every placeholder a base string contains has to be one the code knows about.
// A {seson} in BaseCopy would render the braces on a real page.
func TestEveryPlaceholderInBaseCopyIsAKnownOne(t *testing.T) {
	known := map[string]bool{
		viewmodel.PlaceholderSeason: true,
		viewmodel.PlaceholderDate:   true,
		viewmodel.PlaceholderQuota:  true,
		viewmodel.PlaceholderStatus: true,
		viewmodel.PlaceholderCount:  true,
		viewmodel.PlaceholderTitle:  true,
		viewmodel.PlaceholderName:   true,
		viewmodel.PlaceholderLimit:  true,
		viewmodel.PlaceholderQuery:  true,
	}

	for key, s := range viewmodel.BaseCopy {
		rest := s
		for {
			open := strings.Index(rest, "{")
			if open < 0 {
				break
			}
			close := strings.Index(rest[open:], "}")
			if close < 0 {
				t.Errorf("%s: unclosed placeholder in %q", key, s)

				break
			}
			name := rest[open+1 : open+close]
			if !known[name] {
				t.Errorf("%s: unknown placeholder {%s} in %q", key, name, s)
			}
			rest = rest[open+close:]
		}
	}
}
