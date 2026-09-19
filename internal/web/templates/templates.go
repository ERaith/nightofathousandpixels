// Package templates holds every HTML page and fragment the site renders.
//
// Nothing in here reaches a database or makes a decision. A handler gathers
// what a page needs, fills a view model, and renders.
//
// The shapes a template takes live in internal/web/viewmodel, not here. That
// is the seam: a handler ticket and a template ticket both depend on the view
// model and neither depends on the other, so a page can be built against
// viewmodel.FixtureFullSlate with no database running, and the handler that
// eventually fills the same struct changes no markup. A page that needs a new
// field gets it added there first, and both sides see it.
//
// This package keeps two things of its own, and both are deliberately here
// rather than in the view model:
//
//   - BaseCSS, because it is a fact about this site's markup and a caller must
//     not be able to move it.
//   - noticeClass and isCurrent, because they map data onto base.css class
//     names and to the current request path. Which CSS class a warning gets is
//     template knowledge; that the flash is a warning is view-model knowledge.
package templates

import (
	"strconv"

	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// BaseCSS is the site's own stylesheet: layout, forms, components, and every
// accessibility floor. It is deliberately a constant rather than a field on
// Theme, because a caller must not be able to move it, swap it, or drop it.
// See the comment in layout.templ for why its position in <head> matters.
const BaseCSS = "/static/css/base.css"

// The shell types, named locally for the templates that render them. These are
// aliases, not copies: templates.Page and viewmodel.LayoutData are one type, so
// there is exactly one definition to change when a page needs a new field.
//
// Page keeps its shorter name because that is what reads well in a template
// signature — templ SomePage(p Page) — and because every .templ file in this
// package already says it.
type (
	// Page is the site shell's data. See viewmodel.LayoutData.
	Page = viewmodel.LayoutData

	// Theme is a theme pack's presentation values. See viewmodel.Theme.
	Theme = viewmodel.Theme

	// NavItem is one link in the site header. See viewmodel.NavItem.
	NavItem = viewmodel.NavItem

	// Flash is a one-shot message. See viewmodel.Flash.
	Flash = viewmodel.Flash

	// FlashLevel is how loud a Flash is. See viewmodel.FlashLevel.
	FlashLevel = viewmodel.FlashLevel
)

// The theme defaults, re-exported so a caller reaching for templates.Theme
// does not have to import two packages to fill it in.
const (
	DefaultSiteTitle = viewmodel.DefaultSiteTitle
	DefaultTagline   = viewmodel.DefaultTagline
	DefaultPackCSS   = viewmodel.DefaultPackCSS
)

// The four flash levels, re-exported for the same reason.
const (
	FlashSuccess = viewmodel.FlashSuccess
	FlashInfo    = viewmodel.FlashInfo
	FlashWarning = viewmodel.FlashWarning
	FlashError   = viewmodel.FlashError
)

// isCurrent reports whether a nav item points at the page being rendered.
func isCurrent(p Page, item NavItem) bool {
	return item.Href != "" && item.Href == p.Path
}

// noticeClass is the base.css modifier for a flash level. Info gets none: a
// plain .notice is already the neutral one.
//
// This is a function here rather than a method on Flash because it is the one
// piece of flash rendering that is about this site's stylesheet. Flash.Prefix
// and Flash.Role stay on the type: a prefix word and an ARIA role are true of
// the message wherever it is rendered.
func noticeClass(f Flash) string {
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
//
// It stays in this package rather than moving to the view model because it is
// fixed copy that no handler assembles — the three values below are the whole
// set, and a handler picks one rather than filling one in.
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
