// Package web mounts the site's HTML pages and its static assets onto the
// router.
//
// It is presentation only. Nothing here talks to the database, and the
// handlers below hold no state beyond the theme and origin they were
// configured with. Real pages (the submit form, the slate, the archive) arrive
// with their own tickets and plug in the same way.
//
// That still holds now that these pages know who is reading them (nap-bus).
// The session and the three queries behind that answer belong to
// internal/signin; what arrives here is Options.Reader, a function returning a
// view model, in the same shape and for the same reason as
// Options.ThemePreview. This package imports neither a database nor a session.
package web

import (
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/ERaith/nightofathousandpixels/internal/theme"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// staticPrefix is the URL space the stylesheets, fonts and images live under.
// base.css and every theme pack are addressed through it.
const staticPrefix = "/static/"

// defaultStaticDir is where those files sit on disk, relative to the working
// directory the server is started from.
const defaultStaticDir = "static"

// defaultSeason is the year the placeholder front page and the footer name.
// Ticket D1 owns the real season window and replaces this with a lookup.
const defaultSeason = 2026

// Options configures the site's pages. The zero value works: it renders the
// default theme with no absolute URLs in the og: tags.
type Options struct {
	// Origin is the site's public scheme://host, from the ORIGIN setting. It
	// is only used to build absolute URLs for link previews.
	Origin string

	// StaticDir is the directory served under /static/. Blank means "static".
	StaticDir string

	// Theme is the theme pack's presentation values, set directly.
	//
	// It is the override rather than the normal path: a caller that has a
	// Themes registry names a pack with Pack and lets the registry build this.
	// A non-zero value here wins, which is what a test wanting one specific
	// Theme and no filesystem needs.
	Theme templates.Theme

	// Themes is every loaded theme pack. Nil is the unthemed site, which is a
	// supported state: base.css's own contrast-checked palette and
	// viewmodel.BaseCopy, as themes/README.md promises.
	Themes *theme.Registry

	// Pack is the theme pack this site renders in, by name —
	// season.theme_pack. "default" and a name the registry does not have both
	// mean the unthemed site.
	//
	// It is one value for the whole Site rather than a per-request lookup
	// because there is one live season at a time and its theme was decided in
	// September. When D1 makes the season a per-request fact, this becomes a
	// field on the page rather than on the Site, and nothing else here moves.
	Pack string

	// ThemePreview decides who may use ?theme= to render a page in another
	// pack. Nil is nobody, which is the right default: the preview is how the
	// 2026 theme gets chosen in late September, and it is an admin's tool.
	//
	// See theme.Authorizer. cmd/server passes the real check; a local gallery
	// passes theme.Anyone.
	ThemePreview theme.Authorizer

	// Season is the year shown on the front page and in the footer. Zero means
	// defaultSeason.
	Season int

	// Nav is the site header's navigation, as a reader who is not a member of
	// the open season sees it. Nil means templates.DefaultNav().
	//
	// It is a parameter rather than a constant because the links that belong
	// in the header depend on what is mounted: a header offering pages this
	// build does not serve would be a dead link on the team's front door.
	Nav []templates.NavItem

	// MemberNav is the whole navigation a signed-in member sees — not the
	// extra items, the complete list (ticket nap-dbu). Nil means members see
	// Nav like everybody else.
	//
	// Two complete lists rather than one plus an append: see viewmodel.NavFor,
	// which does the picking and explains why appending at render time is a
	// race with a silent outcome.
	MemberNav []templates.NavItem

	// SignInHref is where the header's account control sends a visitor who is
	// not signed in. Blank renders no control at all, which is what a build
	// with no authentication wired up wants.
	//
	// It is dropped the moment Reader identifies somebody, so a signed-in
	// person is never offered a second sign-in.
	SignInHref string

	// SignOutHref ends a session, for the account control on a page rendered
	// to somebody Reader identified. Blank renders the name with no way out,
	// which is only right for a build with no sign-out route.
	SignOutHref string

	// Reader answers "who is reading this page", or nil for a visitor who is
	// not signed in (ticket nap-bus). Nil func means nobody is ever known,
	// which is the correct behaviour for a build with no authentication and
	// the reason this package's tests need no database.
	//
	// These four pages -- the front page and the three error pages -- are the
	// only ones on the site whose handler does not otherwise know who is
	// reading them: this package holds no session and touches no database, by
	// design, and that has not changed. The lookup belongs to
	// internal/signin, which already resolves a session to a person for the
	// whitelist gate; signin.Service.ReadAccount is the same walk with the
	// gate taken off, and cmd/server hands it over here.
	//
	// It is a function on Options rather than a middleware writing to the
	// request context, which is what nap-bus proposed. Options.ThemePreview
	// above is the same shape for the same reason, and the reason is the
	// mounting: this package's routes include /static/ and the theme-asset
	// handler, neither of which builds a page, and a middleware wrapped round
	// the router would have had to be kept off both by hand. page() is called
	// exactly once per HTML render and by nothing else, so "HTML pages only"
	// is a property of where it is called from rather than a rule somebody has
	// to remember when adding a route.
	//
	// It must not fail and must not gate. A reader it cannot identify is a
	// signed-out reader; see ReadAccount, which is careful about it.
	Reader func(*http.Request) *viewmodel.Account
}

// Site renders the site's pages. Build one with New and mount it with Routes.
type Site struct {
	opts Options

	// nav and memberNav are the two complete header lists, resolved once here
	// so that no request builds one. See viewmodel.NavFor.
	nav       []templates.NavItem
	memberNav []templates.NavItem
}

// New returns a Site with every unset option filled in.
func New(opts Options) *Site {
	if opts.StaticDir == "" {
		opts.StaticDir = defaultStaticDir
	}
	if opts.Season == 0 {
		opts.Season = defaultSeason
	}
	if opts.Nav == nil {
		opts.Nav = templates.DefaultNav()
	}

	return &Site{opts: opts, nav: opts.Nav, memberNav: opts.MemberNav}
}

// Routes mounts the pages, the static file handler and the theme packs onto r,
// and installs the site's own 404 and 405 pages in place of Go's plain-text
// ones.
//
// Every HTML handler goes through themed, which is what makes ?theme= work.
// The static and theme-asset handlers deliberately do not: a stylesheet is
// fetched by URL and a preview that changed which bytes came back from
// /theme/portal/theme.css would be previewing the wrong thing.
func (s *Site) Routes(r chi.Router) {
	r.Get("/", s.themed(s.handleHome))
	r.Handle(staticPrefix+"*", s.staticHandler())
	r.Handle(theme.URLPrefix+"*", s.opts.Themes.Handler(s.NotFound))

	r.NotFound(s.themed(s.NotFound))
	r.MethodNotAllowed(s.themed(s.methodNotAllowed))
}

// themed wraps an HTML handler in the ?theme= preview middleware.
//
// With no ThemePreview authorizer this is the identity function in all but
// name: the middleware sees a nil Authorizer and passes straight through, so
// the ordinary site pays a closure call and nothing else.
func (s *Site) themed(h http.HandlerFunc) http.HandlerFunc {
	return s.opts.Themes.Preview(s.opts.ThemePreview, h).ServeHTTP
}

// page builds the shell data every page shares.
//
// It is the one place in this package that asks who is reading, and it is
// called once per HTML render and from nowhere else. That is what keeps the
// account lookup off /static/ and off the theme-asset handler: those serve
// bytes and never build a page.
func (s *Site) page(r *http.Request) templates.Page {
	p := templates.Page{
		Theme:      s.theme(r),
		Origin:     s.opts.Origin,
		Path:       r.URL.Path,
		Nav:        s.nav,
		SeasonYear: s.opts.Season,
		SignInHref: s.opts.SignInHref,
	}

	return s.forReader(r, p)
}

// forReader fills in the account half of the shell for a reader Options.Reader
// recognises, and leaves p alone for everybody else.
//
// The two halves are mutually exclusive by construction, exactly as in
// board.withViewer and signin.pageFor: SignInHref goes the moment there is
// somebody to greet, so nobody is offered a second sign-in, and CurrentUser
// stays nil otherwise so the header cannot greet an empty name.
func (s *Site) forReader(r *http.Request, p templates.Page) templates.Page {
	if s.opts.Reader == nil {
		return p
	}

	account := s.opts.Reader(r)
	if !account.SignedIn() {
		return p
	}

	p.CurrentUser = account.User
	p.SignInHref = ""
	p.SignOutHref = s.opts.SignOutHref
	p.Nav = viewmodel.NavFor(s.nav, s.memberNav, account.IsMember)

	return p
}

// theme picks the pack this request renders in.
//
// The preview wins when there is one, including when it is the deliberate
// choice of no pack at all — that is the whole value of ?theme=none, and it is
// why PreviewedTheme returns a bool rather than letting a zero Theme mean
// "nothing was asked for".
func (s *Site) theme(r *http.Request) templates.Theme {
	if t, ok := theme.PreviewedTheme(r.Context()); ok {
		return t
	}
	if s.opts.Theme.IsSet() {
		return s.opts.Theme
	}

	return s.opts.Themes.Theme(s.opts.Pack)
}

func (s *Site) handleHome(w http.ResponseWriter, r *http.Request) {
	p := s.page(r)
	s.render(w, r, http.StatusOK, templates.HomePage(p))
}

// NotFound renders the site's 404. It is exported because a handler that has
// looked something up and not found it should reach for this rather than
// http.NotFound.
func (s *Site) NotFound(w http.ResponseWriter, r *http.Request) {
	p := s.page(r)
	p.Title = templates.NotFoundContent.Heading
	s.render(w, r, http.StatusNotFound, templates.NotFoundPage(p))
}

// ServerError renders the site's 500. The underlying error is the caller's to
// log: it never reaches the page, because a visitor cannot act on it.
func (s *Site) ServerError(w http.ResponseWriter, r *http.Request) {
	p := s.page(r)
	p.Title = templates.ServerErrorContent.Heading
	s.render(w, r, http.StatusInternalServerError, templates.ServerErrorPage(p))
}

func (s *Site) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	p := s.page(r)
	p.Title = templates.MethodNotAllowedContent.Heading
	s.render(w, r, http.StatusMethodNotAllowed, templates.ErrorPage(p, templates.MethodNotAllowedContent))
}

// render writes a component as a complete HTML response.
//
// The status and the Content-Type go out before rendering starts, so a
// template that fails halfway leaves a truncated page rather than a second set
// of headers. There is nothing better available at that point — the response
// has already begun — so the error is logged and the connection is left to
// close.
func (s *Site) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if err := c.Render(r.Context(), w); err != nil {
		log.Printf("render %s: %v", r.URL.Path, err)
	}
}

// staticHandler serves /static/ from disk.
//
// http.Dir already refuses to escape its root, so path traversal is covered.
// What it does not do is refuse to list a directory, so a request for
// /static/css/ would otherwise print an index of the stylesheets. This wrapper
// turns those into the site's own 404.
func (s *Site) staticHandler() http.Handler {
	files := http.FileServer(http.Dir(s.opts.StaticDir))

	return http.StripPrefix(staticPrefix, dirListingsOff(files, s.NotFound))
}

func dirListingsOff(next http.Handler, notFound http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A trailing slash, or a bare "." after cleaning, is a directory
		// request. Anything with a filename falls through to the file server,
		// which 404s on its own if the file is not there.
		if strings.HasSuffix(r.URL.Path, "/") || path.Base(r.URL.Path) == "." {
			notFound(w, r)

			return
		}

		next.ServeHTTP(w, r)
	})
}
