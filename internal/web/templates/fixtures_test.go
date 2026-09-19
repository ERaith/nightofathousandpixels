package templates_test

import (
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// These tests render the W1 layout against the W2 fixtures, and that pairing
// is the point of them.
//
// templates.Page is an alias for viewmodel.LayoutData, not a copy of it, so a
// field added on one side is visible on the other with nothing to keep in
// step. What an alias cannot catch is a fixture that stops being renderable —
// a nil the shell dereferences, a combination the markup was never tried
// against. Rendering every fixture through the real layout is what catches
// that, and it costs nothing to run: no database, no handler, no server.
//
// If one of these fails after a change to viewmodel, the fixture is telling
// you a real page would have failed the same way.

// Every shell fixture has to produce a whole document. The bare one is the
// case that matters most: zero Theme, no origin, no nav, no user, no season —
// every optional element absent at once, which is what an error page rendered
// before anything is configured actually looks like.
func TestLayoutRendersEveryFixture(t *testing.T) {
	t.Parallel()

	for name, layout := range viewmodel.FixtureLayouts() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page := render(t, templates.Layout(layout))

			for _, want := range []string{
				"<!doctype html>",
				`<html lang="en">`,
				"</html>",
				`class="skip-link" href="#main"`,
				`id="main"`,
				templates.BaseCSS,
			} {
				if !strings.Contains(strings.ToLower(page), strings.ToLower(want)) {
					t.Errorf("fixture %q did not render %s", name, want)
				}
			}
		})
	}
}

// The signed-in fixture is the one carrying every optional element at once: a
// theme pack with a share image, a real origin, four nav links, a season and
// two flashes. It is the fixture a template author builds against, so it is
// the one that has to actually exercise the shell rather than quietly skipping
// half of it.
func TestSignedInFixtureExercisesTheWholeShell(t *testing.T) {
	t.Parallel()

	layout := viewmodel.FixtureLayout()
	page := render(t, templates.Layout(layout))

	for _, want := range []string{
		// The theme pack, and base.css after it.
		layout.Theme.PackCSS,
		templates.BaseCSS,
		// An origin means the absolute-URL tags are all present.
		`property="og:url" content="https://pixels.example.com/2026"`,
		`property="og:image" content="https://pixels.example.com/static/themes/vhs/og-2026.png"`,
		`name="twitter:card" content="summary_large_image"`,
		// Four nav links, with the current one marked.
		`<a href="/2026" aria-current="page">The slate</a>`,
		`<a href="/2026/submit">Submit</a>`,
		// Both flashes, each with its word and its live-region role.
		"Done:",
		"The Thing is on the board.",
		"Note:",
		`role="status"`,
		// The footer names the season.
		"2026 season",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the signed-in fixture did not render %s", want)
		}
	}

	pack := strings.Index(page, layout.Theme.PackCSS)
	if base := strings.Index(page, templates.BaseCSS); pack > base {
		t.Errorf("theme pack linked after base.css (pack at %d, base at %d); "+
			"base must load LAST or a pack can overrule the site's layout", pack, base)
	}
}

// The bare fixture is every optional element absent. Each one has to be absent
// rather than present and empty: an empty <nav> is a landmark a screen reader
// announces and then finds nothing in, and a half-formed og:url is worse for a
// preview scraper than no og:url.
func TestBareFixtureOmitsRatherThanEmpties(t *testing.T) {
	t.Parallel()

	page := render(t, templates.Layout(viewmodel.FixtureLayoutBare()))

	for _, unwanted := range []string{
		"<nav",
		"og:image",
		"og:url",
		"<link rel=\"canonical\"",
		`class="notice`,
	} {
		if strings.Contains(page, unwanted) {
			t.Errorf("the bare fixture rendered %s, which has no value to fill it", unwanted)
		}
	}

	// What must survive: the defaults are enough for a complete page.
	for _, want := range []string{
		viewmodel.DefaultPackCSS,
		viewmodel.DefaultSiteTitle,
		viewmodel.DefaultTagline,
		`name="twitter:card" content="summary"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the bare fixture is missing %s, which is a default and not optional", want)
		}
	}

	// No season means no footer season line, not "0 season".
	if strings.Contains(page, "0 season") {
		t.Error(`the bare fixture rendered "0 season"; an unset season must omit the line`)
	}
}

// An apostrophe and a non-ASCII character are ordinary data here — half the
// group has one in their name and a good third of the films have one in the
// title. The apostrophe has to be escaped in the source and the accented
// character has to survive as itself.
func TestApostrophesAndAccentsSurviveTheShell(t *testing.T) {
	t.Parallel()

	layout := viewmodel.FixtureLayoutBare()
	layout.Title = viewmodel.FixtureNotOnWhitelist().Layout.Title
	layout.Flashes = []viewmodel.Flash{
		{Level: viewmodel.FlashInfo, Message: viewmodel.FixtureApostropheMovie().Title +
			" was submitted by " + viewmodel.FixtureAdminUser().Label() + "."},
	}

	page := render(t, templates.Layout(layout))

	// templ escapes the apostrophe on the way out, so a bare ' in the output
	// would mean it reached the browser unescaped.
	if strings.Contains(page, "Rosemary's Baby") {
		t.Error("an apostrophe was emitted raw; templ should have escaped it")
	}
	if !strings.Contains(page, "Rosemary&#39;s Baby") {
		t.Error("the escaped apostrophe is missing; the title did not render at all")
	}

	// Non-ASCII is not escaped and must not be mangled.
	if !strings.Contains(page, "Siobhán O&#39;Doherty") {
		t.Error("the accented character did not survive rendering")
	}

	// The same applies in an attribute and in <title>.
	if !strings.Contains(page, "Not on this year&#39;s list") {
		t.Error("the apostrophe in the page title did not survive rendering")
	}
}

// Rendering a whole page fixture's Layout field is what a page template will
// actually do, so it is worth proving the field is renderable on its own
// rather than only the standalone shell fixtures.
func TestPageFixtureLayoutsRender(t *testing.T) {
	t.Parallel()

	layouts := map[string]viewmodel.LayoutData{
		"full slate":        viewmodel.FixtureFullSlate().Layout,
		"empty slate":       viewmodel.FixtureEmptySlate().Layout,
		"locked slate":      viewmodel.FixtureLockedSlate().Layout,
		"submit":            viewmodel.FixtureSubmit().Layout,
		"submit errors":     viewmodel.FixtureSubmitWithErrors().Layout,
		"sign in":           viewmodel.FixtureSignIn().Layout,
		"not on whitelist":  viewmodel.FixtureNotOnWhitelist().Layout,
		"admin slate":       viewmodel.FixtureAdminSlate().Layout,
		"slate signed out":  viewmodel.FixtureSlateSignedOut().Layout,
		"submit at limit":   viewmodel.FixtureSubmitAtLimit().Layout,
		"submit barred":     viewmodel.FixtureSubmitBarred().Layout,
		"locked slate tied": viewmodel.FixtureLockedSlateTied().Layout,
	}

	for name, layout := range layouts {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page := render(t, templates.Layout(layout))

			if !strings.Contains(page, "</html>") {
				t.Errorf("fixture %q rendered an incomplete document", name)
			}
			if title := layout.DocumentTitle(); title == "" {
				t.Errorf("fixture %q has no document title", name)
			}
		})
	}
}

// The sign-in and sign-out hrefs are mutually exclusive: a page offering both
// is a page that does not know whether anybody is signed in.
//
// The shell does not render either control yet — the header account control
// belongs to Epic C — but the fixtures already carry the fields, and a fixture
// that contradicts itself will be copied into the handler that fills the same
// struct. This is the check that keeps them honest until there is markup to
// hold them to.
func TestFixtureAuthStateIsCoherent(t *testing.T) {
	t.Parallel()

	for name, layout := range viewmodel.FixtureLayouts() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if layout.SignInHref != "" && layout.SignOutHref != "" {
				t.Error("fixture offers both sign-in and sign-out")
			}
			if layout.SignedIn() && layout.SignInHref != "" {
				t.Error("a signed-in fixture offers sign-in")
			}
			if !layout.SignedIn() && layout.SignOutHref != "" {
				t.Error("a signed-out fixture offers sign-out")
			}
			if layout.SignedIn() && layout.CurrentUser.Label() == "" {
				t.Error("a signed-in fixture has no name and no email to fall back to")
			}
		})
	}
}
