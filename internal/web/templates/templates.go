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

// The page shapes, named locally for the templates that render them. Aliases
// again, not copies.
//
// The two page types are the one place the naming has to bend. templ compiles
// a component into a function, so a component called SlatePage and a type
// called SlatePage cannot both exist in this package. The component keeps the
// name — SlatePage is what a handler calls and what every other page template
// here is named like — and the type is shortened, which is why these read
// `templ SlatePage(p Slate)`.
type (
	// Slate is the slate page's data. See viewmodel.SlatePage.
	Slate = viewmodel.SlatePage

	// Submit is the submit page's data. See viewmodel.SubmitPage.
	Submit = viewmodel.SubmitPage

	// MovieCard is one submission as a card. See viewmodel.MovieCard.
	MovieCard = viewmodel.MovieCard

	// SubmitForm is the form's values and errors. See viewmodel.SubmitForm.
	SubmitForm = viewmodel.SubmitForm

	// SubmitQuota is a viewer's allowance. See viewmodel.SubmitQuota.
	SubmitQuota = viewmodel.SubmitQuota

	// SeasonSummary is the season a page is about. See viewmodel.SeasonSummary.
	SeasonSummary = viewmodel.SeasonSummary

	// SeasonResult is a locked season's outcome. See viewmodel.SeasonResult.
	SeasonResult = viewmodel.SeasonResult

	// FieldErrors is a form's validation messages. See viewmodel.FieldErrors.
	FieldErrors = viewmodel.FieldErrors
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
//
// Heading and Body are still plain strings, and still the unthemed copy, so
// that a caller holding an ErrorContent can print one without a theme in hand —
// internal/signin does exactly that for a page title. Themed swaps in the
// pack's version where there is one. The two have to agree, so the strings
// below and their entries in viewmodel.BaseCopy are the same strings, and
// TestErrorContentMatchesBaseCopy says so out loud.
type ErrorContent struct {
	Status  int
	Heading string
	Body    string

	// HeadingKey and BodyKey are the copy keys a pack overrides. Blank on an
	// ErrorContent somebody built by hand, which then simply renders as
	// written — an error page that a pack has nothing to say about is still an
	// error page.
	HeadingKey string
	BodyKey    string
}

// StatusText renders the status code for the badge on an error page.
func (e ErrorContent) StatusText() string {
	return strconv.Itoa(e.Status)
}

// Themed returns e with the theme's wording where the theme has some.
//
// An unthemed site, a pack that says nothing about errors, and an ErrorContent
// with no keys all come back unchanged, which is why the template can call this
// unconditionally.
func (e ErrorContent) Themed(t Theme) ErrorContent {
	if e.HeadingKey != "" {
		e.Heading = t.Text(e.HeadingKey)
	}
	if e.BodyKey != "" {
		e.Body = t.Text(e.BodyKey)
	}

	return e
}

// The two errors a visitor can actually see.
var (
	// NotFoundContent is the 404 page's copy.
	NotFoundContent = ErrorContent{
		Status:     404,
		Heading:    viewmodel.BaseCopy[viewmodel.KeyError404Heading],
		Body:       viewmodel.BaseCopy[viewmodel.KeyError404Body],
		HeadingKey: viewmodel.KeyError404Heading,
		BodyKey:    viewmodel.KeyError404Body,
	}

	// ServerErrorContent is the 500 page's copy. It does not show the error:
	// the visitor cannot act on it, and it goes to the logs instead.
	ServerErrorContent = ErrorContent{
		Status:     500,
		Heading:    viewmodel.BaseCopy[viewmodel.KeyError500Heading],
		Body:       viewmodel.BaseCopy[viewmodel.KeyError500Body],
		HeadingKey: viewmodel.KeyError500Heading,
		BodyKey:    viewmodel.KeyError500Body,
	}
)

// MethodNotAllowedContent is the 405. It exists so that a wrong verb on a real
// route still lands on a page that looks like the site.
var MethodNotAllowedContent = ErrorContent{
	Status:     405,
	Heading:    viewmodel.BaseCopy[viewmodel.KeyError405Heading],
	Body:       viewmodel.BaseCopy[viewmodel.KeyError405Body],
	HeadingKey: viewmodel.KeyError405Heading,
	BodyKey:    viewmodel.KeyError405Body,
}
