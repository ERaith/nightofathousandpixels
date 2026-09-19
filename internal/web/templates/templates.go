// Package templates holds every HTML page and fragment the site renders.
//
// Nothing in here reaches a database or makes a decision. A handler gathers
// what a page needs, fills a Page, and renders. Theme values arrive the same
// way — as plain data — so the manifest loader (F2) and the theme copy strings
// (F3) can supply real ones without any template changing shape.
package templates

import "strconv"

// BaseCSS is the site's own stylesheet: layout, forms, components, and every
// accessibility floor. It is deliberately a constant rather than a field on
// Theme, because a caller must not be able to move it, swap it, or drop it.
// See the comment in layout.templ for why its position in <head> matters.
const BaseCSS = "/static/css/base.css"

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
	// BaseCSS; see layout.templ.
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
	// is turned into an absolute URL with Page.Origin, because Open Graph
	// requires one. Blank omits the image tags entirely rather than pointing a
	// preview scraper at a 404.
	OGImage string

	// OGImageAlt describes OGImage for people whose reader announces it.
	OGImageAlt string
}

// Resolve returns a copy of t with every blank field filled in.
func (t Theme) Resolve() Theme {
	if t.PackCSS == "" {
		t.PackCSS = DefaultPackCSS
	}
	if t.SiteTitle == "" {
		t.SiteTitle = DefaultSiteTitle
	}
	if t.Tagline == "" {
		t.Tagline = DefaultTagline
	}
	return t
}

// NavItem is one link in the site header.
type NavItem struct {
	Label string
	Href  string
}

// Page is everything the shell needs, independent of what the page itself
// says. Handlers build one of these and hand it to a page template.
type Page struct {
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

	// Season is the year this page belongs to, shown in the footer. Zero omits
	// it.
	Season int
}

// DocumentTitle is the <title>: "Submit a movie — Night of a Thousand Pixels",
// or just the site title on the front page.
func (p Page) DocumentTitle() string {
	site := p.Theme.Resolve().SiteTitle
	if p.Title == "" || p.Title == site {
		return site
	}

	return p.Title + " — " + site
}

// MetaDescription is the page's own description, falling back to the theme
// tagline.
func (p Page) MetaDescription() string {
	if p.Description != "" {
		return p.Description
	}

	return p.Theme.Resolve().Tagline
}

// AbsoluteURL turns a root-relative path into a full URL for the og: tags. It
// returns "" — and so omits the tag — when either half is missing, because a
// half-formed og:url is worse than none.
func (p Page) AbsoluteURL(path string) string {
	if p.Origin == "" || path == "" {
		return ""
	}

	return p.Origin + path
}

// SeasonLabel is the footer's season line, or "" when no season is set.
func (p Page) SeasonLabel() string {
	if p.Season <= 0 {
		return ""
	}

	return strconv.Itoa(p.Season) + " season"
}

// isCurrent reports whether a nav item points at the page being rendered.
func (p Page) isCurrent(item NavItem) bool {
	return item.Href != "" && item.Href == p.Path
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
// coloured edge on each notice, but colour cannot be the only signal (WCAG
// 1.4.1), so every flash also says what it is.
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

// noticeClass is the base.css modifier for this level. Info gets none: a plain
// .notice is already the neutral one.
func (f Flash) noticeClass() string {
	switch f.Level {
	case FlashSuccess:
		return "notice notice--ok"
	case FlashWarning:
		return "notice notice--warn"
	case FlashError:
		return "notice notice--error"
	case FlashInfo:
		return "notice"
	default:
		return "notice"
	}
}

// ErrorContent is the copy on an error page. Handlers use NotFoundContent and
// ServerErrorContent rather than building their own, so every error on the
// site reads the same way.
type ErrorContent struct {
	Status  int
	Heading string
	Body    string
}

// StatusText renders the status code for the badge on an error page.
func (e ErrorContent) StatusText() string {
	return strconv.Itoa(e.Status)
}

// The two errors a visitor can actually see.
var (
	// NotFoundContent is the 404 page's copy.
	NotFoundContent = ErrorContent{
		Status:  404,
		Heading: "That page isn't here",
		Body: "The link may be out of date, or a season that hasn't opened yet. " +
			"Nothing is broken — there is just nothing at this address.",
	}

	// ServerErrorContent is the 500 page's copy. It does not show the error:
	// the visitor cannot act on it, and it goes to the logs instead.
	ServerErrorContent = ErrorContent{
		Status:  500,
		Heading: "Something went wrong on our end",
		Body: "The server hit an error and gave up on this page. It has been logged. " +
			"Try again in a minute, and say something in the group chat if it keeps happening.",
	}
)

// MethodNotAllowedContent is the 405. It exists so that a wrong verb on a real
// route still lands on a page that looks like the site.
var MethodNotAllowedContent = ErrorContent{
	Status:  405,
	Heading: "That isn't something you can do here",
	Body: "The page exists, but not for the kind of request that reached it. " +
		"This usually means a stale form or a reloaded submission.",
}
