package theme

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// URLPrefix is the URL space theme packs are served under. It is separate from
// /static/ because /static/ is a directory on disk served verbatim, and a pack
// is not: its stylesheet is generated from a manifest, and only its assets/
// subdirectory is a file tree.
const URLPrefix = "/theme/"

// Registry is every pack the site knows about, loaded once at startup.
//
// It is read-only after New returns, so it needs no lock and can be shared by
// every handler. Reloading is a restart. That is not laziness: a theme changing
// under a running server is a thing nobody wants to debug at the exact moment
// thirty people are on the site, and the whole point of a pack is that it is
// decided in September and then left alone.
type Registry struct {
	packs map[string]Pack
	names []string
}

// New loads every pack directory under dir.
//
// A pack that fails to load is reported and skipped; it does not stop the other
// packs or the server. The site's promise is that a broken pack degrades to the
// unthemed site, and a registry that refused to start would break that promise
// in the loudest possible way — on the morning of 1 October, on a deploy nobody
// would connect to a trailing comma in a JSON file.
//
// logger may be nil, for tests and for cmd/seed.
func New(dir string, logger *slog.Logger) (*Registry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		// No themes directory at all is the unthemed site, which is a
		// supported state and not an error.
		return &Registry{packs: map[string]Pack{}}, nil
	}
	if err != nil {
		return nil, err
	}

	r := &Registry{packs: make(map[string]Pack, len(entries))}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sub := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(filepath.Join(sub, ManifestName)); err != nil {
			// A directory under themes/ with no manifest is not a failed pack,
			// it is not a pack. themes/ also holds README.md and the contrast
			// checker, and a future one might hold a scratch directory.
			continue
		}

		pack, err := Load(sub)
		if err != nil {
			logAttr(logger).Error("theme pack not loaded; falling back to the base palette for it",
				slog.String("pack", entry.Name()),
				slog.Any("error", err),
			)

			continue
		}
		r.packs[pack.Name] = pack
		r.names = append(r.names, pack.Name)
	}
	sort.Strings(r.names)

	return r, nil
}

// NewFromPacks builds a registry from already-loaded packs. Tests use it; so
// would anything that wants a registry without a filesystem.
func NewFromPacks(packs ...Pack) *Registry {
	r := &Registry{packs: make(map[string]Pack, len(packs))}
	for _, p := range packs {
		r.packs[p.Name] = p
		r.names = append(r.names, p.Name)
	}
	sort.Strings(r.names)

	return r
}

// Names is every loaded pack name, sorted. It is what an admin's theme picker
// lists.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}

	return append([]string(nil), r.names...)
}

// Lookup returns a pack by name.
//
// The zero-value receiver and the unknown name answer the same way — no pack,
// false — because every caller wants the same thing in both cases: the unthemed
// site. A season whose theme_pack names a directory that was deleted renders in
// base.css's palette with base.css's copy, which is a whole, readable site.
func (r *Registry) Lookup(name string) (Pack, bool) {
	if r == nil || name == "" || name == DefaultPackName {
		return Pack{}, false
	}
	p, ok := r.packs[name]

	return p, ok
}

// Theme is the view-model theme for a pack name, or the zero Theme when there
// is no such pack. Handlers call this rather than Lookup: they want a Theme and
// have nothing useful to do with the difference.
func (r *Registry) Theme(name string) ViewTheme {
	p, ok := r.Lookup(name)
	if !ok {
		return ViewTheme{}
	}

	return p.Theme
}

// Handler serves the packs: one generated stylesheet and one assets tree each.
//
//	GET /theme/portal/theme.css          the generated stylesheet
//	GET /theme/portal/assets/og.png      anything in themes/portal/assets/
//
// Anything else under /theme/ is a 404 through notFound, which is the site's
// own 404 page rather than Go's plain text, so a mistyped asset path in a
// template still lands somewhere that looks like the site.
//
// notFound may be nil, in which case http.NotFound is used.
func (r *Registry) Handler(notFound http.HandlerFunc) http.Handler {
	if notFound == nil {
		notFound = http.NotFound
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name, rest, ok := splitPackPath(strings.TrimPrefix(req.URL.Path, URLPrefix))
		if !ok {
			notFound(w, req)

			return
		}
		pack, ok := r.Lookup(name)
		if !ok {
			notFound(w, req)

			return
		}

		switch {
		case rest == StylesheetName:
			serveStylesheet(w, req, pack)
		case strings.HasPrefix(rest, AssetsDir+"/"):
			serveAsset(w, req, pack, strings.TrimPrefix(rest, AssetsDir+"/"), notFound)
		default:
			notFound(w, req)
		}
	})
}

// splitPackPath splits "portal/assets/og.png" into "portal" and
// "assets/og.png".
//
// path.Clean normalises away "." and ".." before the split, so a request for
// /theme/portal/assets/../../etc/passwd has already become something that
// cannot name a pack by the time it gets here. http.Dir refuses the same thing
// again underneath; this is the first of the two.
func splitPackPath(p string) (name, rest string, ok bool) {
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	name, rest, found := strings.Cut(p, "/")
	if !found || name == "" || rest == "" {
		return "", "", false
	}
	if validateName(name) != nil {
		return "", "", false
	}

	return name, rest, true
}

// serveStylesheet writes the generated CSS.
//
// It goes out with a short max-age and an ETag rather than the immutable,
// year-long cache a fingerprinted asset gets. The URL is /theme/portal/theme.css
// on every deploy, so a long cache would mean a palette fix in the last week of
// September not reaching the people who had already loaded the page — which is
// everybody, because the link went into the group chat in the first week.
func serveStylesheet(w http.ResponseWriter, req *http.Request, pack Pack) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, req, StylesheetName, time.Time{}, strings.NewReader(string(pack.CSS)))
}

// serveAsset serves one file out of the pack's assets/ directory.
//
// Only assets/ is served, never the pack root: manifest.json and theme.css are
// the site's inputs, not its outputs, and a pack directory is somewhere a
// designer drops files. A stray .psd or a notes.txt next to the manifest should
// not become a public URL because somebody guessed its name.
func serveAsset(w http.ResponseWriter, req *http.Request, pack Pack, rel string, notFound http.HandlerFunc) {
	if rel == "" || strings.HasSuffix(rel, "/") {
		notFound(w, req)

		return
	}

	root := http.Dir(filepath.Join(pack.Dir, AssetsDir))
	f, err := root.Open("/" + rel)
	if err != nil {
		notFound(w, req)

		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		notFound(w, req)

		return
	}

	// Assets are fonts and images that change when the pack does, which is
	// never, once the season starts. A day is long enough to matter on a phone
	// and short enough that a late swap lands the next morning.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, req, info.Name(), info.ModTime(), f)
}

func logAttr(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.New(slog.DiscardHandler)
	}

	return logger
}
