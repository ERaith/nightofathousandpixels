// Package viewmodel is the contract between the handlers that gather data and
// the templates that render it.
//
// Everything here is a plain Go value: string, int, bool, time.Time. No
// database row reaches a template. That is the whole point rather than a style
// preference — a template that accepted a store row would have to know which
// columns are nullable, what a pgtype.Timestamptz is, and that
// season_member.submit_limit means something different from
// season.default_submit_limit. Those are the store's problems, and they get
// answered before anything arrives here.
//
// The dependency runs one way. Handlers import this package and fill these
// structs; templates import this package and render them. This package imports
// neither, and it must not import internal/store or internal/auth — the moment
// it does, the seam it exists to create is gone and the two halves of the site
// stop being able to move independently.
//
// The methods in this package format and describe. None of them decides
// anything. A question like "may this person submit a movie right now" depends
// on the season window, on membership and on authorisation; a handler answers
// it and carries the answer here as a bool. The rule for adding a method is
// that it must be answerable from the struct's own fields with no clock, no
// request and no database.
//
// If a page needs a new field, it is added here first, and both sides see it.
//
// See fixtures.go for realistic sample data covering every struct, including
// the cases that actually break layouts. A frontend ticket can render real
// pages against those with no backend running at all.
package viewmodel

import "strconv"

// Defaults used whenever a caller leaves a Theme field blank. They are enough
// to render a complete, readable page with no theme pack installed at all.
const (
	DefaultSiteTitle = "Night of a Thousand Pixels"
	DefaultTagline   = "Thirty friends, two picks each, one movie. Every year, archived."
	DefaultPackCSS   = "/static/css/pack.css"
)

// Theme is the slice of a theme pack that the HTML shell needs: where the
// pack's stylesheet lives, and the handful of strings that end up in <head>.
//
// The zero value is usable — Resolve fills every blank with a default — so a
// handler that has no theme yet can pass Theme{} and still get a whole page.
type Theme struct {
	// PackCSS is the href of the theme pack stylesheet. It is linked BEFORE
	// the site's own base.css; see the comment above Layout in
	// internal/web/templates/layout.templ, which is where that order is
	// enforced and explained.
	//
	// The default path 404s until ticket F4 builds a pack, and that is a
	// supported state rather than a bug: themes/README.md guarantees that a
	// missing pack degrades to base.css's own contrast-checked fallback
	// palette, not to unstyled HTML.
	PackCSS string

	// SiteTitle is the wordmark in the header and the tail of every <title>.
	SiteTitle string

	// Tagline is the fallback description for pages that supply none of their
	// own, and the default og:description.
	Tagline string

	// OGImage is the share image these links get when someone pastes them into
	// the group chat, as a root-relative path ("/static/img/og-2026.png"). It
	// is turned into an absolute URL with LayoutData.Origin, because Open
	// Graph requires one. Blank omits the image tags entirely rather than
	// pointing a preview scraper at a 404.
	OGImage string

	// OGImageAlt describes OGImage for people whose reader announces it.
	OGImageAlt string

	// Copy is the pack's wording, keyed by the Key* constants in copy.go. Nil
	// is the normal state for an unthemed site and renders every string out of
	// BaseCopy, so nothing has to check it. See Copy.
	Copy Copy
}

// Resolve returns a copy of t with every blank field filled in.
//
// SiteTitle and Tagline are filled from the pack's own copy before the base
// defaults, so a pack that sets site.title in its manifest does not also have
// to say it twice. They stay as fields rather than becoming copy keys outright
// because they are the two strings the <head> needs before any page exists,
// and because Theme{SiteTitle: "..."} is how every existing caller and fixture
// already says it.
func (t Theme) Resolve() Theme {
	if t.PackCSS == "" {
		t.PackCSS = DefaultPackCSS
	}
	if t.SiteTitle == "" {
		t.SiteTitle = t.Copy.Text(KeySiteTitle)
	}
	if t.Tagline == "" {
		t.Tagline = t.Copy.Text(KeySiteTagline)
	}

	return t
}

// IsSet reports whether anything at all has been put on this theme.
//
// It exists because Theme stopped being comparable the moment it carried a Copy
// map, and `t == Theme{}` no longer builds. A caller that wants "did somebody
// configure a theme, or should I go and find one" asks this.
func (t Theme) IsSet() bool {
	return t.PackCSS != "" ||
		t.SiteTitle != "" ||
		t.Tagline != "" ||
		t.OGImage != "" ||
		t.OGImageAlt != "" ||
		len(t.Copy) > 0
}

// NavItem is one link in the site header.
type NavItem struct {
	Label string
	Href  string
}

// CurrentUser is the signed-in viewer, as the page needs to talk about them.
//
// It is deliberately thin. It carries no person id, no email verification
// state and no session token, because nothing a template renders needs any of
// those, and a view model is a bad place to keep something that must not be
// printed by accident.
//
// IsAdmin is per season, matching season_member.is_admin. There is no global
// admin in this project, so a page that shows admin controls has to have been
// told which season it is talking about.
type CurrentUser struct {
	// DisplayName is what Google gave us, and may be empty: person.display_name
	// defaults to '' and nothing in the schema forbids a blank one. Use Label
	// rather than this field when rendering.
	DisplayName string

	// Email is the address they signed in with. It is shown back to them on
	// the sign-in and not-on-whitelist pages, where "you are signed in as
	// someone else" is the most likely explanation for what they are seeing.
	Email string

	// IsAdmin is admin for the season this page is about, not for the site.
	IsAdmin bool
}

// Label is the name to print for this viewer, falling back to their email when
// the provider gave us no display name. The nil receiver is handled so that a
// template can call it on LayoutData.CurrentUser without a guard.
func (u *CurrentUser) Label() string {
	if u == nil {
		return ""
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}

	return u.Email
}

// LayoutData is everything the site shell needs, independent of what the page
// inside it says. Every page view model embeds one as its Layout field.
type LayoutData struct {
	Theme Theme

	// Title is the page's own name. Blank renders the site title alone, which
	// is what the front page wants.
	Title string

	// Description overrides Theme.Tagline for this page's meta description and
	// og:description.
	Description string

	// Path is the request path. It marks the current nav item and builds
	// og:url. It is a path, not a URL: Origin supplies the rest.
	Path string

	// Origin is the site's scheme://host, from the ORIGIN setting. Blank omits
	// the absolute-URL meta tags rather than emitting relative ones, which
	// preview scrapers do not follow.
	Origin string

	// Nav is the header navigation. Empty renders no <nav> at all.
	Nav []NavItem

	// Flashes are the one-shot messages shown above the page content.
	Flashes []Flash

	// SeasonYear is the year this page belongs to, shown in the footer. Zero
	// omits it. It is the bare year rather than a SeasonSummary because the
	// footer appears on pages that have no season at all — the sign-in page,
	// an error page — and giving those a whole season shape would mean
	// inventing one.
	SeasonYear int

	// CurrentUser is the signed-in viewer, or nil when signed out. Nil is the
	// signed-out state rather than a zero-valued struct, so that a template
	// cannot accidentally greet an empty name.
	CurrentUser *CurrentUser

	// SignInHref and SignOutHref are where the header's account control
	// points. Blank hides that control, which is what an error page rendered
	// before auth is configured wants.
	SignInHref  string
	SignOutHref string
}

// SignedIn reports whether there is a viewer to talk about.
func (d LayoutData) SignedIn() bool {
	return d.CurrentUser != nil
}

// DocumentTitle is the <title>: "Submit a movie — Night of a Thousand Pixels",
// or just the site title on the front page.
func (d LayoutData) DocumentTitle() string {
	site := d.Theme.Resolve().SiteTitle
	if d.Title == "" || d.Title == site {
		return site
	}

	return d.Title + " — " + site
}

// MetaDescription is the page's own description, falling back to the theme
// tagline.
func (d LayoutData) MetaDescription() string {
	if d.Description != "" {
		return d.Description
	}

	return d.Theme.Resolve().Tagline
}

// AbsoluteURL turns a root-relative path into a full URL for the og: tags. It
// returns "" — and so omits the tag — when either half is missing, because a
// half-formed og:url is worse than none.
func (d LayoutData) AbsoluteURL(path string) string {
	if d.Origin == "" || path == "" {
		return ""
	}

	return d.Origin + path
}

// SeasonLabel is the footer's season line, or "" when no season is set.
func (d LayoutData) SeasonLabel() string {
	if d.SeasonYear <= 0 {
		return ""
	}

	return strconv.Itoa(d.SeasonYear) + " season"
}

// FlashLevel is how loud a Flash is. It picks the notice colour, but colour is
// only ever the second signal: see Flash.Prefix.
type FlashLevel string

// The four levels a handler can raise.
const (
	FlashSuccess FlashLevel = "success"
	FlashInfo    FlashLevel = "info"
	FlashWarning FlashLevel = "warning"
	FlashError   FlashLevel = "error"
)

// Flash is a single message shown at the top of a page: "Saved.", "You have
// already submitted two movies."
type Flash struct {
	Level   FlashLevel
	Message string
}

// Prefix is the word that carries the meaning in text. base.css draws a
// coloured edge on each notice, but colour cannot be the only signal
// (WCAG 1.4.1), so every flash also says what it is.
func (f Flash) Prefix() string {
	switch f.Level {
	case FlashSuccess:
		return "Done:"
	case FlashWarning:
		return "Heads up:"
	case FlashError:
		return "Error:"
	case FlashInfo:
		return "Note:"
	default:
		return "Note:"
	}
}

// Role is the ARIA live-region role. Problems interrupt; confirmations wait
// their turn.
func (f Flash) Role() string {
	switch f.Level {
	case FlashWarning, FlashError:
		return "alert"
	case FlashSuccess, FlashInfo:
		return "status"
	default:
		return "status"
	}
}
