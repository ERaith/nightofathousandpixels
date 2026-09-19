package theme

import (
	"context"
	"net/http"
)

// PreviewParam is the query parameter that switches the pack for one request:
//
//	/?theme=portal
//	/?theme=elvira
//
// It exists because the 2026 theme gets decided in late September by looking at
// it, not by reading a manifest. Two people arguing about Portal versus Elvira
// need to see both against the real slate, on a phone, thirty seconds apart —
// and the alternative to this is a deploy per opinion.
const PreviewParam = "theme"

// PreviewNone is the value that previews no pack at all: ?theme=none.
//
// This is the one that actually catches things. "Does the page still work with
// no pack?" is the question a pack author cannot answer from inside their pack,
// and themes/README.md promises the answer is yes. A named value makes checking
// it a URL rather than an editing session.
const PreviewNone = "none"

// Authorizer reports whether this request may preview a theme.
//
// It is a func rather than a hard-coded check on a session because internal/web
// does not know what a season admin is and must not learn: it renders pages and
// holds no database. cmd/server supplies the real one, which is
// season_member.is_admin for the current season.
//
// A nil Authorizer means nobody, which is the right default for a parameter
// that changes what a page looks like. Previewing is not dangerous — the pack a
// visitor previews only changes their own view of their own request — but it is
// an admin tool, and an admin tool that is on by default is a thing people find
// by accident and then report as a bug.
type Authorizer func(*http.Request) bool

// Anyone allows every request to preview. It is for local development and for
// the fixture gallery, never for a deployment where the list of people who can
// see the site is longer than the list of people who decide what it looks like.
func Anyone(*http.Request) bool { return true }

type previewKey struct{}

// preview is what the middleware puts in the context: the chosen pack, or the
// explicit choice of no pack.
//
// The two are different and the difference matters. Absent means "nobody asked,
// use the season's theme"; present-with-no-pack means "somebody asked for the
// unthemed site", and collapsing them would make ?theme=none silently do
// nothing, which is the one preview whose whole value is that it does something.
type preview struct {
	pack Pack
	set  bool
}

// Preview wraps h so that an authorized request carrying ?theme=<pack> renders
// in that pack.
//
// What it does NOT do is redirect, set a cookie or remember the choice. A
// preview lasts exactly one request and lives entirely in a URL, so it can be
// pasted into the group chat, opened on somebody else's phone, and compared
// side by side in two tabs. A sticky preview would mean an admin who forgot
// about it spends October looking at a pack nobody else can see, which is the
// failure mode this feature is most likely to have.
//
// An unknown or unauthorized ?theme= is ignored rather than refused: the page
// is the point, and a 400 in place of the slate because somebody mistyped a
// pack name would be the tool breaking the thing it exists to inspect.
func (r *Registry) Preview(auth Authorizer, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := req.URL.Query().Get(PreviewParam)
		if name == "" || auth == nil || !auth(req) {
			h.ServeHTTP(w, req)

			return
		}

		// A previewed page is one person's private view of a moment, and the
		// pack it shows is not the pack the next request gets. Nothing between
		// here and the browser should keep it.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Add("Vary", "Cookie")

		if name == PreviewNone {
			h.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), previewKey{}, preview{set: true})))

			return
		}

		pack, ok := r.Lookup(name)
		if !ok {
			h.ServeHTTP(w, req)

			return
		}

		h.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), previewKey{}, preview{pack: pack, set: true})))
	})
}

// PreviewedTheme returns the theme this request is previewing, and whether it
// is previewing one at all.
//
// A handler calls this and prefers the answer over the season's own theme. The
// zero Theme with true is ?theme=none — the unthemed site, asked for on
// purpose — which is why the bool is not redundant with the value.
func PreviewedTheme(ctx context.Context) (ViewTheme, bool) {
	p, ok := ctx.Value(previewKey{}).(preview)
	if !ok || !p.set {
		return ViewTheme{}, false
	}

	return p.pack.Theme, true
}

// PreviewedPack is the same answer as a whole pack, for anything that wants the
// pack's name or label — an admin bar saying which theme is on screen, say.
// Previewing no pack returns the zero Pack and true.
func PreviewedPack(ctx context.Context) (Pack, bool) {
	p, ok := ctx.Value(previewKey{}).(preview)
	if !ok || !p.set {
		return Pack{}, false
	}

	return p.pack, true
}
