package templates_test

import (
	"html"
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// These render templates.Layout with no children, so the whole document is the
// shell and nothing a page put inside <main> can satisfy an assertion by
// accident. That matters here more than elsewhere: the slate says "Sign in to
// add yours" in its own content, so a test that looked at the whole slate page
// for "Sign in" would pass whether or not the header rendered anything at all.

// nap-1j5: the header said "Sign in" on every page, to everybody, because
// sign-in was an ordinary nav item and a nav item is a constant. The data was
// already on LayoutData and no template read it.
func TestHeaderNamesTheSignedInViewer(t *testing.T) {
	t.Parallel()

	layout := viewmodel.FixtureLayout()
	page := html.UnescapeString(render(t, templates.Layout(layout)))

	for _, want := range []string{
		// Who they are, and said as a fact rather than left to be inferred
		// from a name floating in the chrome.
		"Signed in as",
		layout.CurrentUser.Label(),
		// The initial disc, hidden from the screen reader that has just been
		// given the whole name.
		`class="account__avatar" aria-hidden="true"`,
		// And the way out.
		`method="post" action="` + layout.SignOutHref + `"`,
		"Sign out",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the header of a signed-in page is missing %s", want)
		}
	}

	if strings.Contains(page, ">Sign in<") {
		t.Error("the header offered sign-in to somebody who is already signed in; this is nap-1j5")
	}
}

// Sign-out is a POST form and must stay one. A GET /auth/logout is followed by
// anything that prefetches -- a browser, a chat client building a link
// preview, a scanner -- and this control is now on every page of the site, so
// the number of things in a position to do that went up.
func TestHeaderSignOutIsAPostNotALink(t *testing.T) {
	t.Parallel()

	layout := viewmodel.FixtureLayout()
	page := render(t, templates.Layout(layout))

	if strings.Contains(page, `<a class="btn btn--quiet" href="`+layout.SignOutHref) {
		t.Fatal("sign-out is a link; anything that prefetches would silently end the session")
	}

	form := strings.Index(page, `action="`+layout.SignOutHref+`"`)
	if form < 0 {
		t.Fatal("no sign-out form in the header")
	}
	if !strings.Contains(page[:form], `method="post"`) {
		t.Error("the sign-out form has no method=post, so it would submit as a GET")
	}
}

// Signed out is the other half, and it has to be the other half rather than an
// extra: a visitor gets the way in and nothing that implies a session.
func TestHeaderOffersSignInToAVisitor(t *testing.T) {
	t.Parallel()

	layout := viewmodel.FixtureLayoutSignedOut()
	page := render(t, templates.Layout(layout))

	if !strings.Contains(page, `href="`+layout.SignInHref+`"`) {
		t.Error("a signed-out visitor is offered no way in")
	}
	for _, unwanted := range []string{"Signed in as", "Sign out", "account__avatar"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("a signed-out page rendered %s", unwanted)
		}
	}
}

// Neither half known is a real state, not a bug: an error page rendered before
// authentication is configured has no viewer and nowhere to send one. It gets
// no control at all rather than an empty one or a dead link.
func TestHeaderRendersNoAccountControlWhenNothingIsKnown(t *testing.T) {
	t.Parallel()

	page := render(t, templates.Layout(viewmodel.FixtureLayoutBare()))

	if strings.Contains(page, `class="account"`) {
		t.Error("an account control was rendered for a page that knows neither the viewer nor the way in")
	}
}

// The account control is deliberately NOT a nav item, which is the whole shape
// of the fix: a nav item is a constant handed to the layout by a caller, and a
// constant cannot know who is reading it.
func TestAccountControlIsNotANavItem(t *testing.T) {
	t.Parallel()

	for name, layout := range map[string]viewmodel.LayoutData{
		"signed in":  viewmodel.FixtureLayout(),
		"signed out": viewmodel.FixtureLayoutSignedOut(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page := render(t, templates.Layout(layout))

			nav := strings.Index(page, "<nav")
			end := strings.Index(page, "</nav>")
			if nav < 0 || end < 0 {
				t.Fatal("the fixture has nav links but no <nav> rendered")
			}
			if strings.Contains(page[nav:end], "account") {
				t.Error("the account control is inside the nav landmark")
			}
		})
	}

	// DefaultNav is what a build with nothing else configured gets, and
	// sign-in coming back to it is the exact regression.
	for _, item := range templates.DefaultNav() {
		if strings.Contains(strings.ToLower(item.Label), "sign in") {
			t.Errorf("DefaultNav lists %q; sign-in is the account control's, not the nav's", item.Label)
		}
	}
}

// A whitelisted person whose provider gave us no display name is ordinary --
// person.display_name defaults to '' and nothing in the schema forbids it --
// so the header falls back to the address rather than greeting nobody, and the
// initial disc follows whatever the header actually printed.
func TestHeaderFallsBackToTheAddressWhenThereIsNoName(t *testing.T) {
	t.Parallel()

	layout := viewmodel.FixtureLayout()
	layout.CurrentUser = viewmodel.FixtureNamelessUser()

	page := render(t, templates.Layout(layout))

	if !strings.Contains(page, layout.CurrentUser.Email) {
		t.Error("a viewer with no display name is not named at all in the header")
	}
	if !strings.Contains(page, `aria-hidden="true">N</span>`) {
		t.Errorf("the initial disc does not follow the label it sits beside; want N for %q",
			layout.CurrentUser.Label())
	}
}

// The initial is the first character of the label, not its first byte. The
// admin fixture is called Siobhan O'Doherty with an accent on the a, which
// also has to survive escaping without turning into &#39; on screen.
func TestHeaderInitialIsARuneAndTheNameSurvivesEscaping(t *testing.T) {
	t.Parallel()

	layout := viewmodel.FixtureLayout()
	layout.CurrentUser = viewmodel.FixtureAdminUser()

	raw := render(t, templates.Layout(layout))

	if !strings.Contains(raw, `aria-hidden="true">S</span>`) {
		t.Error("the initial disc is not the first letter of the display name")
	}
	if !strings.Contains(html.UnescapeString(raw), layout.CurrentUser.DisplayName) {
		t.Errorf("the header did not render %q whole", layout.CurrentUser.DisplayName)
	}
}
