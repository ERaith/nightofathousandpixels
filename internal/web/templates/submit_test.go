package templates_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// These tests render the submit page against every W2 fixture, and each one
// asserts the thing its fixture exists to prove rather than that the page came
// back non-empty.
//
// The pattern throughout is to look inside <main> rather than at the whole
// document. The site header carries a "Submit" nav link on every page, so
// "this page offers no way to submit" is not a claim you can make about the
// document — it is a claim about the page's own content, and mainContent is
// what makes the difference checkable. A test that asserted against the whole
// page would pass or fail on the nav instead, which is exactly the sort of
// vacuous check that looks like coverage.

// mainContent is the page's own content: everything inside the <main> landmark
// the layout renders. It fails the test rather than returning "" when the
// landmark is missing, so an assertion about "what is not in main" can never
// quietly become an assertion about an empty string.
func mainContent(t *testing.T, page string) string {
	t.Helper()

	const (
		openTag  = `<main id="main"`
		closeTag = `</main>`
	)

	start := strings.Index(page, openTag)
	if start < 0 {
		t.Fatalf("rendered page has no <main> landmark; the layout did not run")
	}

	end := strings.Index(page[start:], closeTag)
	if end < 0 {
		t.Fatalf("rendered page has an unclosed <main> landmark")
	}

	content := page[start : start+end]
	if strings.TrimSpace(content) == "" {
		t.Fatalf("rendered page has an empty <main>")
	}

	return content
}

// A blank form is the first thing anybody sees. Every control has to be there,
// labelled, and pointing at the hint that describes it.
func TestSubmitBlankFormIsComplete(t *testing.T) {
	t.Parallel()

	page := render(t, templates.SubmitPage(viewmodel.FixtureSubmit()))
	main := mainContent(t, page)

	for _, want := range []string{
		// One panel rather than a run of controls down the page (nap-luo):
		// the class is what makes four fields and a button read as a single
		// thing to fill in.
		`<form class="panel" method="post" action="/2026/submit">`,
		`<label for="submit-title">Title</label>`,
		`<label for="submit-year">Year</label>`,
		`<label for="submit-trailer-url">Trailer link</label>`,
		`<label for="submit-description">Why this one</label>`,
		`name="title"`,
		`name="year"`,
		`name="trailer_url"`,
		`name="description"`,
		`type="submit"`,
		`href="/2026"`,
	} {
		if !strings.Contains(main, want) {
			t.Errorf("a blank submit form is missing %s", want)
		}
	}

	// Nothing has gone wrong yet, so nothing may look as though it has.
	for _, unwanted := range []string{
		`aria-invalid="true"`,
		`class="field__error`,
		`id="submit-errors"`,
	} {
		if strings.Contains(main, unwanted) {
			t.Errorf("a blank submit form rendered %s with no error to justify it", unwanted)
		}
	}
}

// The field names the form posts are the constants the handler records errors
// against. If the two ever disagree the error simply never appears beside its
// field, and nothing else fails — so this is the check that catches it.
func TestSubmitFieldNamesAreTheViewModelConstants(t *testing.T) {
	t.Parallel()

	main := mainContent(t, render(t, templates.SubmitPage(viewmodel.FixtureSubmit())))

	for _, field := range []string{
		viewmodel.FieldTitle,
		viewmodel.FieldYear,
		viewmodel.FieldTrailerURL,
		viewmodel.FieldDescription,
	} {
		if !strings.Contains(main, `name="`+field+`"`) {
			t.Errorf("no control posts as %q; a handler error against that field would never be shown", field)
		}
	}
}

// A rejected form has to come back carrying what the person typed, including
// the year that is not a year. That is the whole reason SubmitForm.Year is a
// string, and a page that redisplayed it as empty or as 0 would look like the
// site threw the form away.
func TestSubmitErrorsKeepWhatWasTyped(t *testing.T) {
	t.Parallel()

	page := viewmodel.FixtureSubmitWithErrors()
	main := mainContent(t, render(t, templates.SubmitPage(page)))

	for _, want := range []string{
		`value="nineteen eighty four"`,
		`value="youtube.com/watch?v=abc"`,
		"It is the one with the rabbit.",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("the rejected form did not return %s to the box it came from", want)
		}
	}

	// The title was blank and has to come back blank rather than disappearing.
	if !strings.Contains(main, `id="submit-title"`) {
		t.Error("the title control is gone from a rejected form")
	}
}

// Each message has to be attached to its own field, not merely printed in red
// somewhere on the page. aria-describedby is what attaches it, and the message
// is text, so colour is never the only signal (WCAG 1.4.1, 3.3.1).
func TestSubmitErrorsAreAttachedToTheirFields(t *testing.T) {
	t.Parallel()

	main := mainContent(t, render(t, templates.SubmitPage(viewmodel.FixtureSubmitWithErrors())))

	cases := map[string]struct {
		id, message string
	}{
		"title":   {"submit-title", "A title is the one thing we cannot guess."},
		"year":    {"submit-year", "Use four digits, like 1984 — or leave it blank."},
		"trailer": {"submit-trailer-url", "Start the link with https:// so it opens."},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(main, `id="`+c.id+`-error"`) {
				t.Errorf("no error element for %s", c.id)
			}
			if !strings.Contains(main, c.message) {
				t.Errorf("the message for %s is not on the page at all", c.id)
			}
			if !strings.Contains(main, `aria-describedby="`+c.id+`-hint `+c.id+`-error"`) {
				t.Errorf("%s does not point at its own error through aria-describedby", c.id)
			}
			if !strings.Contains(main, `aria-invalid="true"`) {
				t.Errorf("%s is not marked invalid", c.id)
			}
		})
	}

	// The description was fine, so it must not be dressed as a failure.
	if !strings.Contains(main, `aria-describedby="submit-description-hint"`) {
		t.Error("the valid field points at something other than its hint alone")
	}
	if strings.Contains(main, `id="submit-description-error"`) {
		t.Error("the valid field has an error element")
	}
}

// The summary above the form is what makes a rejected form recoverable for
// somebody who cannot see the red. It has to name each field, link to it, and
// read down the form in the form's own order.
func TestSubmitErrorSummaryLinksEachFieldInFormOrder(t *testing.T) {
	t.Parallel()

	main := mainContent(t, render(t, templates.SubmitPage(viewmodel.FixtureSubmitWithErrors())))

	if !strings.Contains(main, `id="submit-errors"`) {
		t.Fatal("a rejected form rendered no error summary")
	}
	if !strings.Contains(main, "3 things to fix") {
		t.Error("the summary does not say how many things are wrong")
	}

	order := []string{`href="#submit-title"`, `href="#submit-year"`, `href="#submit-trailer-url"`}

	at := -1
	for _, link := range order {
		found := strings.Index(main, link)
		if found < 0 {
			t.Fatalf("the summary does not link to %s", link)
		}
		if found < at {
			t.Errorf("%s is out of form order in the summary", link)
		}

		at = found
	}

	// The summary sits above the form it is about, not below it.
	if strings.Index(main, `id="submit-errors"`) > strings.Index(main, "<form") {
		t.Error("the error summary is rendered after the form")
	}
}

// Every aria-describedby on the page has to point at an element that exists.
// A dangling one is silent: the message is on screen, correctly coloured, and
// never announced. This walks the rendered document rather than trusting the
// four hand-written pairs above.
func TestSubmitDescribedByTargetsAllExist(t *testing.T) {
	t.Parallel()

	var (
		describedBy = regexp.MustCompile(`aria-describedby="([^"]*)"`)
		ids         = regexp.MustCompile(`id="([^"]*)"`)
	)

	for name, page := range viewmodel.FixtureSubmitPages() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rendered := render(t, templates.SubmitPage(page))

			present := map[string]bool{}
			for _, match := range ids.FindAllStringSubmatch(rendered, -1) {
				present[match[1]] = true
			}

			refs := describedBy.FindAllStringSubmatch(rendered, -1)
			if page.CanSubmit && len(refs) == 0 {
				t.Fatal("a page with a form on it described none of its controls")
			}

			for _, match := range refs {
				if strings.TrimSpace(match[1]) == "" {
					t.Error("an aria-describedby is empty, which describes nothing")
				}

				for _, id := range strings.Fields(match[1]) {
					if !present[id] {
						t.Errorf("aria-describedby points at %q, which is not on the page", id)
					}
				}
			}
		})
	}
}

// A viewer with a pick left is told how many, in the place they are about to
// spend one.
func TestSubmitShowsPicksRemaining(t *testing.T) {
	t.Parallel()

	main := mainContent(t, render(t, templates.SubmitPage(viewmodel.FixtureSubmit())))

	if want := viewmodel.FixtureQuota().Label(); !strings.Contains(main, want) {
		t.Errorf("the form does not say %q", want)
	}
}

// Somebody who has spent both picks gets progress language, and their films.
func TestSubmitAtLimitSaysTheyUsedThem(t *testing.T) {
	t.Parallel()

	main := mainContent(t, render(t, templates.SubmitPage(viewmodel.FixtureSubmitAtLimit())))

	if !strings.Contains(main, "That is both your picks") {
		t.Error("the at-limit page does not say the picks are spent")
	}
	if want := viewmodel.FixtureQuotaAtLimit().Label(); !strings.Contains(main, want) {
		t.Errorf("the at-limit page does not carry the quota sentence %q", want)
	}

	// Both their films are on the page, which is what makes the count mean
	// something concrete.
	for _, title := range []string{"The Thing (1982)", "Rosemary&#39;s Baby (1968)"} {
		if !strings.Contains(main, title) {
			t.Errorf("the at-limit page does not show %s, which is one of the picks it is counting", title)
		}
	}
}

// A member whose limit is an explicit 0 is a different person from one who
// used their picks up, and the page has to say a different thing. This is the
// distinction SubmitQuota.Barred and SubmitQuota.AtLimit exist to keep apart,
// so it is asserted in both directions: the barred copy present, the at-limit
// copy absent.
func TestSubmitBarredReadsAsASettingNotAsProgress(t *testing.T) {
	t.Parallel()

	main := mainContent(t, render(t, templates.SubmitPage(viewmodel.FixtureSubmitBarred())))

	if want := viewmodel.FixtureQuotaBarred().Label(); !strings.Contains(main, want) {
		t.Errorf("the barred page does not say %q", want)
	}
	if !strings.Contains(main, "You are on the list to vote") {
		t.Error("the barred page does not name the situation")
	}

	// The wrong copy. "You have used all 0 picks" is both untrue and
	// unanswerable for somebody who was never given any.
	for _, wrong := range []string{"used all", "both your picks", "picks left"} {
		if strings.Contains(main, wrong) {
			t.Errorf("the barred page says %q, which is the used-up-your-picks copy", wrong)
		}
	}
}

// The window shutting overrules the quota. This viewer still has a pick, and
// telling them so on a page with no form on it is how a site gets a reputation
// for being broken.
func TestSubmitClosedWindowOverrulesTheQuota(t *testing.T) {
	t.Parallel()

	main := mainContent(t, render(t, templates.SubmitPage(viewmodel.FixtureSubmitClosed())))

	if !strings.Contains(main, "The board is closed") {
		t.Error("the closed-window page does not say the window is what is wrong")
	}
	if leftover := viewmodel.FixtureQuota().Label(); strings.Contains(main, leftover) {
		t.Errorf("the closed-window page still advertises %q next to no form", leftover)
	}
}

// The rule the whole page is built around: when the handler said no, there is
// nothing on the page to say yes with. Not a disabled button, not a form that
// posts and gets refused — nothing.
func TestSubmitOffersNoControlWhenItCannotBeUsed(t *testing.T) {
	t.Parallel()

	for name, page := range viewmodel.FixtureSubmitPages() {
		if page.CanSubmit {
			continue
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			main := mainContent(t, render(t, templates.SubmitPage(page)))

			for _, unwanted := range []string{"<form", `type="submit"`, "<input", "<textarea"} {
				if strings.Contains(main, unwanted) {
					t.Errorf("a page that cannot submit still rendered %s", unwanted)
				}
			}

			// Not even the address. The nav link lives outside <main>, which
			// is why this can be asserted at all.
			if page.Action != "" && strings.Contains(main, page.Action) {
				t.Errorf("a page that cannot submit still points at %s", page.Action)
			}

			// It is a dead end with a way out, not just a dead end.
			if page.CancelHref != "" && !strings.Contains(main, page.CancelHref) {
				t.Error("a page that cannot submit offers no way back to the slate")
			}
		})
	}
}

// Every submit fixture has to produce a whole document with a heading, whatever
// state it is in.
func TestSubmitRendersEveryFixture(t *testing.T) {
	t.Parallel()

	for name, page := range viewmodel.FixtureSubmitPages() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rendered := render(t, templates.SubmitPage(page))

			if !strings.Contains(rendered, "</html>") {
				t.Errorf("fixture %q rendered an incomplete document", name)
			}
			if !strings.Contains(rendered, "<h1>Put a movie on the board</h1>") {
				t.Errorf("fixture %q has no page heading", name)
			}
		})
	}
}
