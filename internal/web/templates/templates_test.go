package templates_test

import (
	"bytes"
	"context"
	"html"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()

	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}

	return buf.String()
}

// The load order is the theme system's whole contract, and a comment in
// layout.templ is not enough to keep it: someone tidying the <head> would only
// have to move one line. This test is what actually holds it.
//
// base.css must come second, because it is the file that owns layout and the
// accessibility floors, and at equal specificity the later stylesheet wins.
func TestThemePackIsLinkedBeforeBaseCSS(t *testing.T) {
	t.Parallel()

	cases := map[string]templates.Theme{
		"default theme": {},
		"pack supplied": {PackCSS: "/static/themes/portal/pack.css"},
	}

	for name, theme := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page := render(t, templates.HomePage(templates.Page{Theme: theme}))

			packHref := theme.Resolve().PackCSS
			pack := strings.Index(page, packHref)
			base := strings.Index(page, templates.BaseCSS)

			if pack < 0 {
				t.Fatalf("theme pack stylesheet %q is not linked at all", packHref)
			}
			if base < 0 {
				t.Fatalf("base stylesheet %q is not linked at all", templates.BaseCSS)
			}
			if pack > base {
				t.Errorf("theme pack is linked after base.css (pack at %d, base at %d); "+
					"base must load LAST or a pack can overrule the site's layout", pack, base)
			}
		})
	}
}

// Nothing in the shell is optional: a missing viewport tag is a broken phone
// layout, and a missing skip link is a keyboard user tabbing through the whole
// header on every page.
func TestLayoutHasTheNonNegotiableHead(t *testing.T) {
	t.Parallel()

	page := render(t, templates.HomePage(templates.Page{}))

	for _, want := range []string{
		`<meta charset="utf-8">`,
		`name="viewport"`,
		`content="width=device-width, initial-scale=1"`,
		`class="skip-link" href="#main"`,
		`id="main"`,
		`tabindex="-1"`,
		`<html lang="en">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("rendered page is missing %s", want)
		}
	}
}

// These links get pasted into a group chat, so the preview card is part of the
// product. og:image needs an absolute URL, which is the one thing easy to get
// wrong.
func TestOpenGraphTags(t *testing.T) {
	t.Parallel()

	t.Run("absolute image url when origin is set", func(t *testing.T) {
		t.Parallel()

		page := render(t, templates.HomePage(templates.Page{
			Origin: "https://pixels.example",
			Path:   "/",
			Theme: templates.Theme{
				OGImage:    "/static/img/og.png",
				OGImageAlt: "A wall of pixels",
			},
		}))

		for _, want := range []string{
			`property="og:image" content="https://pixels.example/static/img/og.png"`,
			`property="og:image:alt" content="A wall of pixels"`,
			`property="og:url" content="https://pixels.example/"`,
			`property="og:description"`,
			`name="twitter:card" content="summary_large_image"`,
		} {
			if !strings.Contains(page, want) {
				t.Errorf("rendered page is missing %s", want)
			}
		}
	})

	t.Run("no image tags without an origin", func(t *testing.T) {
		t.Parallel()

		page := render(t, templates.HomePage(templates.Page{
			Theme: templates.Theme{OGImage: "/static/img/og.png"},
		}))

		if strings.Contains(page, "og:image") {
			t.Error("og:image was emitted with no origin to make it absolute")
		}
		if !strings.Contains(page, `property="og:description"`) {
			t.Error("og:description should still be present without an origin")
		}
	})
}

// base.css draws a coloured edge per level, but colour is never allowed to be
// the only signal (WCAG 1.4.1). Every flash also has to say what it is in
// words and carry a live-region role.
func TestFlashesCarryWordsAndRolesNotJustColour(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level    templates.FlashLevel
		wantWord string
		wantRole string
		wantCSS  string
	}{
		{templates.FlashSuccess, "Done:", "status", "notice--ok"},
		{templates.FlashInfo, "Note:", "status", "notice"},
		{templates.FlashWarning, "Heads up:", "alert", "notice--warn"},
		{templates.FlashError, "Error:", "alert", "notice--error"},
	}

	for _, tc := range cases {
		t.Run(string(tc.level), func(t *testing.T) {
			t.Parallel()

			flash := templates.Flash{Level: tc.level, Message: "Your movie is on the board."}
			page := render(t, templates.FlashMessage(flash))

			if !strings.Contains(page, tc.wantWord) {
				t.Errorf("flash does not carry the word %q", tc.wantWord)
			}
			if !strings.Contains(page, `role="`+tc.wantRole+`"`) {
				t.Errorf("flash does not carry role=%q", tc.wantRole)
			}
			if !strings.Contains(page, tc.wantCSS) {
				t.Errorf("flash is missing the %q class", tc.wantCSS)
			}
			if !strings.Contains(page, flash.Message) {
				t.Error("flash does not contain its own message")
			}
		})
	}
}

func TestFlashRegionRendersNothingWhenEmpty(t *testing.T) {
	t.Parallel()

	if page := render(t, templates.FlashRegion(nil)); page != "" {
		t.Errorf("empty flash region rendered %q; it must emit nothing, "+
			"or the page picks up a stray 2rem gap", page)
	}
}

// An error page is still a page on this site: header, wordmark, stylesheets
// and a way back, not Go's plain text on a white background.
func TestErrorPagesRenderInsideTheSiteShell(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		component templ.Component
		want      templates.ErrorContent
	}{
		"404": {templates.NotFoundPage(templates.Page{}), templates.NotFoundContent},
		"500": {templates.ServerErrorPage(templates.Page{}), templates.ServerErrorContent},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page := render(t, tc.component)

			// templ escapes the copy, so the expectation has to be escaped too.
			if !strings.Contains(page, html.EscapeString(tc.want.Heading)) {
				t.Errorf("%s page is missing its heading", name)
			}
			if !strings.Contains(page, tc.want.StatusText()) {
				t.Errorf("%s page does not say its status code", name)
			}
			if !strings.Contains(page, templates.BaseCSS) {
				t.Errorf("%s page is not inside the site shell", name)
			}
			if !strings.Contains(page, `href="/"`) {
				t.Errorf("%s page gives no way back to the site", name)
			}
		})
	}
}

func TestDocumentTitle(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		page templates.Page
		want string
	}{
		"blank title is the site alone": {
			templates.Page{},
			templates.DefaultSiteTitle,
		},
		"page title is prefixed": {
			templates.Page{Title: "Submit a movie"},
			"Submit a movie — " + templates.DefaultSiteTitle,
		},
		"a title equal to the site is not doubled": {
			templates.Page{Title: templates.DefaultSiteTitle},
			templates.DefaultSiteTitle,
		},
		"a themed site title wins": {
			templates.Page{Title: "Slate", Theme: templates.Theme{SiteTitle: "Aperture Cinema"}},
			"Slate — Aperture Cinema",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tc.page.DocumentTitle(); got != tc.want {
				t.Errorf("DocumentTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The nav is a placeholder today, but the aria-current wiring is what the
// tickets adding real links will rely on.
func TestCurrentNavItemIsMarked(t *testing.T) {
	t.Parallel()

	page := render(t, templates.HomePage(templates.Page{
		Path: "/slate",
		Nav: []templates.NavItem{
			{Label: "Home", Href: "/"},
			{Label: "Slate", Href: "/slate"},
		},
	}))

	if !strings.Contains(page, `<a href="/slate" aria-current="page">Slate</a>`) {
		t.Error("the nav item matching the request path is not marked aria-current")
	}
	if strings.Contains(page, `<a href="/" aria-current="page">`) {
		t.Error("a nav item that is not the current page was marked aria-current")
	}
}

func TestNoNavElementWhenThereAreNoLinks(t *testing.T) {
	t.Parallel()

	page := render(t, templates.HomePage(templates.Page{}))

	if strings.Contains(page, "<nav") {
		t.Error("an empty nav landmark was rendered; a screen reader would announce it and find nothing")
	}
}
