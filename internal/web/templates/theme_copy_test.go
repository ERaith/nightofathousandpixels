package templates_test

import (
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// testPack is a whole theme written in one place, so each test below can assert
// on a string that could only have come from a pack.
//
// It is deliberately not Portal or Elvira. A test that read themes/portal's
// manifest would fail the moment somebody improved a sentence in it, which is
// the one thing a pack author has to be free to do without a Go test in the
// way.
func testPack() viewmodel.Theme {
	return viewmodel.Theme{Copy: viewmodel.Copy{
		viewmodel.KeySiteTitle:              "PACK-TITLE",
		viewmodel.KeySiteTagline:            "PACK-TAGLINE",
		viewmodel.KeySkipLink:               "PACK-SKIP",
		viewmodel.KeyFilmOne:                "PACK-FILM",
		viewmodel.KeyFilmMany:               "PACK-FILMS",
		viewmodel.KeyPickOne:                "PACK-PICK",
		viewmodel.KeyPickMany:               "PACK-PICKS",
		viewmodel.KeySlateCountHeading:      "{count} PACK-ON-THE-BOARD",
		viewmodel.KeySlateEmptyHeading:      "PACK-EMPTY-HEADING",
		viewmodel.KeySlateEmptyBody:         "PACK-EMPTY-BODY",
		viewmodel.KeySlateEmptyCTA:          "PACK-EMPTY-CTA",
		viewmodel.KeySlateEmptySignIn:       "PACK-EMPTY-SIGNIN",
		viewmodel.KeySlateSubmitCTA:         "PACK-SUBMIT-CTA",
		viewmodel.KeyStateSubmittingLabel:   "PACK-STATE-LABEL",
		viewmodel.KeyStateSubmittingSummary: "PACK-STATE-SUMMARY",
		viewmodel.KeyQuotaAtLimit:           "PACK-QUOTA-USED-ALL-{count}",
		viewmodel.KeyQuotaRemaining:         "PACK-QUOTA-{count}-OF-{limit}",
		viewmodel.KeyQuotaHeading:           "PACK-QUOTA-HEADING",
		viewmodel.KeyBlockedAtLimitHeading:  "PACK-CAP-HEADING",
		viewmodel.KeyBlockedAtLimitDated:    "{quota} PACK-CAP-BODY {date}",
		viewmodel.KeyBlockedLockedHeading:   "PACK-LOCKED-HEADING",
		viewmodel.KeyBlockedLockedBody:      "{season} PACK-LOCKED-BODY",
		viewmodel.KeySubmitHeading:          "PACK-SUBMIT-HEADING",
		viewmodel.KeySubmitButton:           "PACK-SUBMIT-BUTTON",
		viewmodel.KeySubmitTitleLabel:       "PACK-TITLE-LABEL",
		viewmodel.KeyMovieSubmittedBy:       "PACK-BY {name}",
		viewmodel.KeyOutcomeWinnerLede:      "PACK-WINNER-LEDE",
		viewmodel.KeyOutcomeWinnerBody:      "{title} PACK-WINNER-BODY",
		viewmodel.KeyError404Heading:        "PACK-404-HEADING",
		viewmodel.KeyErrorBack:              "PACK-ERROR-BACK",
	}}
}

// themed returns a fixture layout wearing the test pack.
func themedLayout() viewmodel.LayoutData {
	d := viewmodel.FixtureLayout()
	d.Theme = testPack()

	return d
}

// The empty board is the most-seen state of the whole site and the single most
// valuable thing a pack can rewrite. Every string on it has to be the pack's.
func TestAPackOwnsTheEmptyBoard(t *testing.T) {
	p := viewmodel.FixtureEmptySlate()
	p.Layout = themedLayout()

	page := render(t, templates.SlatePage(p))

	for _, want := range []string{"PACK-EMPTY-HEADING", "PACK-EMPTY-BODY", "PACK-EMPTY-CTA"} {
		if !strings.Contains(page, want) {
			t.Errorf("the empty board is missing the pack's %q", want)
		}
	}
	if strings.Contains(page, "Nobody has gone first yet") {
		t.Errorf("the base heading survived a pack that replaced it")
	}
}

// The cap refusal: the pack writes the sentence, the view model writes the
// number inside it. A pack that could change the 2 could tell somebody they
// had picks they do not have.
func TestAPackOwnsTheCapRefusalButNotTheCount(t *testing.T) {
	p := viewmodel.FixtureSubmitAtLimit()
	p.Layout = themedLayout()

	page := render(t, templates.SubmitPage(p))

	if !strings.Contains(page, "PACK-CAP-HEADING") {
		t.Errorf("the pack's cap heading is missing")
	}
	// The pack's quota sentence, with the pack's own noun and the view model's
	// own number, dropped whole into the pack's cap sentence.
	if !strings.Contains(page, "PACK-QUOTA-USED-ALL-2 PACK-PICKS PACK-CAP-BODY") {
		t.Errorf("the pack's cap body did not assemble around the real quota:\n%s", mainContent(t, page))
	}
	// A placeholder that arrives inside substituted text is not re-scanned, so
	// nothing a pack writes into one sentence can rewrite the next.
	if strings.Count(page, "PACK-QUOTA-USED-ALL-2") != 1 {
		t.Errorf("the quota sentence was substituted more than once")
	}
	// A page with no form on it must still have no form on it, whatever the
	// pack says.
	if strings.Contains(page, "PACK-SUBMIT-BUTTON") {
		t.Errorf("a blocked submit page rendered a submit button")
	}
}

// The locked season, with the season's own label dropped into the pack's
// sentence rather than concatenated in front of it.
func TestAPackOwnsTheLockedSeason(t *testing.T) {
	p := viewmodel.FixtureSubmit()
	p.Layout = themedLayout()
	p.Season = viewmodel.FixtureSeasonLocked()
	p.CanSubmit = false

	page := render(t, templates.SubmitPage(p))

	if !strings.Contains(page, "PACK-LOCKED-HEADING") {
		t.Errorf("the pack's locked heading is missing")
	}
	if want := p.Season.Label() + " PACK-LOCKED-BODY"; !strings.Contains(page, want) {
		t.Errorf("expected %q in:\n%s", want, mainContent(t, page))
	}
}

// The noun for a film is the most themeable word on the site, and it has to
// pluralise from the pack rather than from a hardcoded "s".
func TestAPackOwnsTheWordForAFilm(t *testing.T) {
	p := viewmodel.FixtureFullSlate()
	p.Layout = themedLayout()

	page := render(t, templates.SlatePage(p))

	if !strings.Contains(page, "6 PACK-FILMS PACK-ON-THE-BOARD") {
		t.Errorf("the count heading did not use the pack's plural noun:\n%s", mainContent(t, page))
	}
}

// The button on the full board, which is the one control most people on this
// site ever press.
//
// This had a test-pack string reserved for it (KeySlateSubmitCTA) and both
// shipped packs wrote one, and the template printed "Put a movie up" anyway —
// hardcoded, for as long as the key had existed. Nothing caught it, because
// every other test on this page renders with no pack and "Put a movie up" is
// also what BaseCopy says, so the literal and the correct answer were the same
// string. It took loading the page under a pack whose voice is nothing like the
// base's to see it.
func TestAPackOwnsTheButtonOnTheFullBoard(t *testing.T) {
	p := viewmodel.FixtureFullSlate()
	p.Layout = themedLayout()

	page := render(t, templates.SlatePage(p))

	if !strings.Contains(page, "PACK-SUBMIT-CTA") {
		t.Errorf("the board's submit button is not the pack's:\n%s", mainContent(t, page))
	}
	if strings.Contains(page, "Put a movie up") {
		t.Errorf("the base label survived a pack that replaced it")
	}
}

// The allowance, in the season strip on the slate and on the submit form.
//
// Theme.QuotaLabel exists for exactly this and carries a long comment saying
// why it was worth a second method — and both templates that print the
// allowance called the unthemed SubmitQuota.Label instead. Only the two blocked
// pages, which assemble the sentence through submit.go, ever went near the
// themed one. So a pack could rewrite the refusal it gets when it says no and
// not the same sentence on the page that says yes.
func TestAPackOwnsTheAllowanceWhereverItIsShown(t *testing.T) {
	slate := viewmodel.FixtureFullSlate()
	slate.Layout = themedLayout()

	submit := viewmodel.FixtureSubmit()
	submit.Layout = themedLayout()

	for _, tc := range []struct {
		name string
		page string
	}{
		{"slate", render(t, templates.SlatePage(slate))},
		{"submit", render(t, templates.SubmitPage(submit))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.page, "PACK-QUOTA-") {
				t.Errorf("the allowance is in the base voice:\n%s", mainContent(t, tc.page))
			}
			if strings.Contains(tc.page, "left of") {
				t.Errorf("the base allowance sentence survived a pack that replaced it")
			}
		})
	}
}

// The heading over it, which was a literal in slate.templ.
func TestAPackOwnsTheAllowanceHeading(t *testing.T) {
	p := viewmodel.FixtureFullSlate()
	p.Layout = themedLayout()

	page := render(t, templates.SlatePage(p))

	if !strings.Contains(page, "PACK-QUOTA-HEADING") {
		t.Errorf("the allowance heading is not the pack's:\n%s", mainContent(t, page))
	}
	if strings.Contains(page, "Your picks") {
		t.Errorf("the base allowance heading survived a pack that replaced it")
	}
}

func TestAPackOwnsTheShellAndTheSeasonState(t *testing.T) {
	p := viewmodel.FixtureFullSlate()
	p.Layout = themedLayout()

	page := render(t, templates.SlatePage(p))

	for _, want := range []string{"PACK-TITLE", "PACK-TAGLINE", "PACK-SKIP", "PACK-STATE-LABEL", "PACK-STATE-SUMMARY"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page is missing the pack's %q", want)
		}
	}
}

func TestAPackOwnsTheFormLabelsAndTheErrorPages(t *testing.T) {
	sp := viewmodel.FixtureSubmit()
	sp.Layout = themedLayout()

	form := render(t, templates.SubmitPage(sp))
	if !strings.Contains(form, "PACK-TITLE-LABEL") {
		t.Errorf("the form did not take the pack's field label")
	}
	if !strings.Contains(form, "PACK-SUBMIT-BUTTON") {
		t.Errorf("the form did not take the pack's button")
	}

	errPage := render(t, templates.NotFoundPage(themedLayout()))
	if !strings.Contains(errPage, "PACK-404-HEADING") {
		t.Errorf("the 404 did not take the pack's heading")
	}
	if !strings.Contains(errPage, "PACK-ERROR-BACK") {
		t.Errorf("the 404 did not take the pack's way back")
	}
}

func TestAPackOwnsTheWinnerAnnouncement(t *testing.T) {
	p := viewmodel.FixtureLockedSlate()
	p.Layout = themedLayout()

	page := render(t, templates.SlatePage(p))

	if !strings.Contains(page, "PACK-WINNER-LEDE") {
		t.Errorf("the pack's winner lede is missing")
	}
	if p.Result == nil {
		t.Fatal("the locked fixture has no result")
	}
	if want := p.Result.WinnerTitle + " PACK-WINNER-BODY"; !strings.Contains(page, want) {
		t.Errorf("expected %q in:\n%s", want, mainContent(t, page))
	}
}

// Half a pack renders a whole site. There is no key that a pack has to set.
func TestAPackThatSetsOneStringStillRendersEveryPage(t *testing.T) {
	half := viewmodel.LayoutData{Theme: viewmodel.Theme{
		Copy: viewmodel.Copy{viewmodel.KeySlateEmptyHeading: "HALF-PACK"},
	}}

	p := viewmodel.FixtureEmptySlate()
	p.Layout = half

	page := render(t, templates.SlatePage(p))
	if !strings.Contains(page, "HALF-PACK") {
		t.Errorf("the one string the pack set did not appear")
	}
	// Everything else is the base voice, complete and readable.
	if !strings.Contains(page, viewmodel.BaseCopy[viewmodel.KeySlateEmptyBody]) {
		t.Errorf("an unset key did not fall back to the base copy")
	}
	if !strings.Contains(page, viewmodel.DefaultSiteTitle) {
		t.Errorf("the site title fell over for a pack that did not set one")
	}
}

// A pack's copy is text. templ escapes it at render time, so a manifest cannot
// smuggle markup onto a page — not by accident and not on purpose.
func TestPackCopyIsEscapedNotInjected(t *testing.T) {
	d := viewmodel.LayoutData{Theme: viewmodel.Theme{Copy: viewmodel.Copy{
		viewmodel.KeySlateEmptyHeading: `<script>alert(1)</script>`,
	}}}

	p := viewmodel.FixtureEmptySlate()
	p.Layout = d

	page := render(t, templates.SlatePage(p))
	if strings.Contains(page, "<script>alert(1)</script>") {
		t.Errorf("a pack injected markup through a copy string")
	}
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Errorf("the string was not rendered at all; expected it escaped:\n%s", mainContent(t, page))
	}
}

// The three ErrorContent values print their own Heading without a theme in
// hand — internal/signin does exactly that for a page title — so those strings
// and their entries in BaseCopy have to be the same strings.
func TestErrorContentMatchesBaseCopy(t *testing.T) {
	cases := []struct {
		content             templates.ErrorContent
		headingKey, bodyKey string
	}{
		{templates.NotFoundContent, viewmodel.KeyError404Heading, viewmodel.KeyError404Body},
		{templates.ServerErrorContent, viewmodel.KeyError500Heading, viewmodel.KeyError500Body},
		{templates.MethodNotAllowedContent, viewmodel.KeyError405Heading, viewmodel.KeyError405Body},
	}
	for _, tc := range cases {
		if tc.content.Heading != viewmodel.BaseCopy[tc.headingKey] {
			t.Errorf("%d: Heading %q != BaseCopy[%s] %q",
				tc.content.Status, tc.content.Heading, tc.headingKey, viewmodel.BaseCopy[tc.headingKey])
		}
		if tc.content.Body != viewmodel.BaseCopy[tc.bodyKey] {
			t.Errorf("%d: Body does not match BaseCopy[%s]", tc.content.Status, tc.bodyKey)
		}
	}
}
