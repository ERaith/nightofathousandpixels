// Command themegallery serves every page of the site, in every state, under
// whichever theme pack you name — with no database, no OIDC provider and no
// migrations.
//
//	go run ./cmd/themegallery -addr :8240
//	open http://localhost:8240/gallery
//
// It exists because a theme is decided by looking at it. Ticket F4 builds two
// packs and somebody picks one in late September; that decision needs the empty
// slate, the full slate, a refused submission and a 404 side by side, at 320px,
// under both packs, in about a minute. Reading two manifests does not do it.
//
// Every page is rendered from viewmodel's own fixtures, which are the same
// values the template tests use — including the ones that actually break
// layouts: the film with no trailer, the 90-character title, the apostrophes,
// the admin list with a withdrawn entry in it. A pack that survives this
// gallery has been seen against real content.
//
// ?theme= works on every URL here, including ?theme=none, which is the one
// worth remembering: it is how you check that the page still works with no pack
// at all, which is the promise themes/README.md makes and the one a pack author
// cannot verify from inside their pack.
//
// It is a development tool. It serves fixture data at fixed URLs with the
// preview gate wide open (theme.Anyone), so it does not belong anywhere a
// person outside this project can reach it. That is why it is its own command
// rather than a flag on the server.
package main

import (
	"flag"
	"fmt"
	"html"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/ERaith/nightofathousandpixels/internal/theme"
	"github.com/ERaith/nightofathousandpixels/internal/web"
	"github.com/ERaith/nightofathousandpixels/internal/web/templates"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

func main() {
	addr := flag.String("addr", ":8240", "address to listen on")
	themesDir := flag.String("themes", "themes", "directory of theme packs")
	staticDir := flag.String("static", "static", "directory served under /static/")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	packs, err := theme.New(*themesDir, logger)
	if err != nil {
		log.Fatalf("themegallery: load %s: %v", *themesDir, err)
	}

	site := web.New(web.Options{
		StaticDir: *staticDir,
		Themes:    packs,
		Nav:       galleryNav(),
		// Anyone, because there is nobody else here. The real server wires
		// this to season_member.is_admin.
		ThemePreview: theme.Anyone,
	})

	g := &gallery{packs: packs}

	r := chi.NewRouter()
	g.mount(r)
	site.Routes(r)

	fmt.Fprintf(os.Stderr, "themegallery on http://localhost%s/gallery  (packs: %v)\n", *addr, packs.Names())

	server := &http.Server{Addr: *addr, Handler: r, ReadHeaderTimeout: 0}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("themegallery: %v", err)
	}
}

// gallery mounts one URL per page state.
type gallery struct {
	packs *theme.Registry
}

// shell is the per-request bits every fixture page needs on its LayoutData: the
// theme this request resolved to, and where it is.
type shell struct {
	theme viewmodel.Theme
	path  string
}

// apply puts the request's theme and path onto a fixture's layout, leaving
// everything else the fixture chose — the signed-in user, the flashes, the
// season year — exactly as it is. The fixtures are the interesting content;
// only the pack changes between two views of the same page.
func (s shell) apply(d *viewmodel.LayoutData) {
	d.Theme = s.theme
	d.Path = s.path
	d.Nav = galleryNav()
}

// entry is one row of the gallery: a URL, what it shows, and how to render it.
type entry struct {
	path   string
	label  string
	render func(shell) templ.Component
}

func slatePage(f func() viewmodel.SlatePage) func(shell) templ.Component {
	return func(s shell) templ.Component {
		p := f()
		s.apply(&p.Layout)

		return templates.SlatePage(p)
	}
}

func submitPage(f func() viewmodel.SubmitPage) func(shell) templ.Component {
	return func(s shell) templ.Component {
		p := f()
		s.apply(&p.Layout)

		return templates.SubmitPage(p)
	}
}

func shellPage(f func(templates.Page) templ.Component) func(shell) templ.Component {
	return func(s shell) templ.Component {
		p := viewmodel.FixtureLayout()
		s.apply(&p)

		return f(p)
	}
}

// entries is the whole matrix. The order is the order somebody judging a pack
// should walk it: the two states everyone sees first, then the refusals, then
// the ends of the year, then the pages nobody wants to reach.
func entries() []entry {
	return []entry{
		{"/gallery/slate/empty", "Slate — empty. The morning of 1 October, and the state most people see first.", slatePage(viewmodel.FixtureEmptySlate)},
		{"/gallery/slate", "Slate — full, signed in, room to submit.", slatePage(viewmodel.FixtureFullSlate)},
		{"/gallery/slate/signed-out", "Slate — signed out. Public to read, not to write.", slatePage(viewmodel.FixtureSlateSignedOut)},
		{"/gallery/slate/at-limit", "Slate — both picks used.", slatePage(viewmodel.FixtureSlateAtLimit)},
		{"/gallery/slate/barred", "Slate — a member who votes but cannot submit.", slatePage(viewmodel.FixtureSlateBarred)},
		{"/gallery/slate/locked", "Slate — locked season with a winner.", slatePage(viewmodel.FixtureLockedSlate)},
		{"/gallery/slate/tied", "Slate — locked season that tied.", slatePage(viewmodel.FixtureLockedSlateTied)},
		{"/gallery/slate/admin", "Slate — admin view, including a withdrawn film.", slatePage(viewmodel.FixtureAdminSlate)},

		{"/gallery/submit", "Submit — the blank form.", submitPage(viewmodel.FixtureSubmit)},
		{"/gallery/submit/errors", "Submit — rejected, with the error summary.", submitPage(viewmodel.FixtureSubmitWithErrors)},
		{"/gallery/submit/at-limit", "Submit — blocked. The cap refusal.", submitPage(viewmodel.FixtureSubmitAtLimit)},
		{"/gallery/submit/barred", "Submit — blocked. Voting only.", submitPage(viewmodel.FixtureSubmitBarred)},
		{"/gallery/submit/closed", "Submit — blocked. The window has closed.", submitPage(viewmodel.FixtureSubmitClosed)},

		{"/gallery/error/404", "404.", shellPage(templates.NotFoundPage)},
		{"/gallery/error/405", "405.", shellPage(func(p templates.Page) templ.Component {
			return templates.ErrorPage(p, templates.MethodNotAllowedContent)
		})},
		{"/gallery/error/500", "500.", shellPage(templates.ServerErrorPage)},
	}
}

func (g *gallery) mount(r chi.Router) {
	r.Get("/gallery", g.preview(g.index))
	for _, e := range entries() {
		r.Get(e.path, g.preview(g.render(e.render)))
	}
}

// preview wraps a handler in the ?theme= middleware, gate wide open.
func (g *gallery) preview(h http.HandlerFunc) http.HandlerFunc {
	return g.packs.Preview(theme.Anyone, h).ServeHTTP
}

// render turns a fixture renderer into a handler.
//
// With no ?theme=, the shell gets the zero Theme — the unthemed site — rather
// than a default pack. The gallery has no season and therefore no theme of its
// own, and defaulting to one of the two packs under judgement would put a thumb
// on the scale.
func (g *gallery) render(f func(shell) templ.Component) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		s := shell{path: req.URL.Path}
		if t, ok := theme.PreviewedTheme(req.Context()); ok {
			s.theme = t
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := f(s).Render(req.Context(), w); err != nil {
			log.Printf("themegallery: render %s: %v", req.URL.Path, err)
		}
	}
}

// index is the contact sheet: every page, every pack, one click apart.
//
// It is written out by hand rather than through a templ component because it is
// the one page in this binary that is not part of the site, and giving it the
// site's shell would make it harder to tell at a glance whether you are looking
// at the gallery or at the thing being judged.
func (g *gallery) index(w http.ResponseWriter, req *http.Request) {
	packs := append([]string{theme.PreviewNone}, g.packs.Names()...)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// This is a local dev page; a write error means the browser went away, and
	// there is nothing useful to do about it beyond stopping.
	out := func(format string, args ...any) {
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			panic(http.ErrAbortHandler)
		}
	}

	out(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Theme gallery</title>
<style>
 :root { color-scheme: dark }
 body { background:#0f1115; color:#e8ebf0; font:16px/1.6 ui-sans-serif, system-ui, sans-serif; margin:0; padding:2rem 1rem }
 main { max-width:60rem; margin:0 auto }
 h1 { font-size:1.6rem; margin:0 0 .25rem }
 p.note { color:#a8b0bd; margin:0 0 2rem }
 ul { list-style:none; padding:0; margin:0 }
 li { padding:.75rem 0; border-bottom:1px solid #2a2f3a }
 .label { display:block; color:#c9d0da }
 a { color:#7cc4ff; display:inline-flex; min-height:44px; align-items:center; padding-inline:.5rem; margin-inline-start:-.5rem }
 .packs { display:flex; flex-wrap:wrap; gap:.25rem 1rem }
</style></head><body><main>
<h1>Theme gallery</h1>
<p class="note">Every page, every state, every pack. Fixture data, no database.
Open one at 320px wide as well as on a desktop, and tab through it.</p>
<ul>
`)
	for _, e := range entries() {
		out("<li><span class=\"label\">%s</span><span class=\"packs\">", html.EscapeString(e.label))
		for _, name := range packs {
			out(`<a href="%s?theme=%s">%s</a>`,
				html.EscapeString(e.path), html.EscapeString(name), html.EscapeString(name))
		}
		out("%s", "</span></li>\n")
	}
	out("%s", "</ul></main></body></html>\n")
}

func galleryNav() []templates.NavItem {
	return []templates.NavItem{
		{Label: "Home", Href: "/"},
		{Label: "Gallery", Href: "/gallery"},
	}
}
