package theme_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/theme"
	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// minimalManifest is a pack that satisfies the contract and says nothing else,
// so a test can add exactly the one thing it is about.
func minimalManifest() map[string]any {
	return map[string]any{
		"name":  "testpack",
		"label": "Test Pack",
		"tokens": map[string]string{
			"bg":      "#101010",
			"surface": "#202020",
			"accent":  "#ffc08a",
			"text":    "#f5f5f5",
			"muted":   "#b0b0b0",
		},
	}
}

// writePack writes a pack directory and returns its path.
func writePack(t *testing.T, name string, manifest map[string]any, stylesheet string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if manifest != nil {
		raw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, theme.ManifestName), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if stylesheet != "" {
		if err := os.WriteFile(filepath.Join(dir, theme.StylesheetName), []byte(stylesheet), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestLoadEmitsTokensAsCustomPropertiesOnRoot(t *testing.T) {
	m := minimalManifest()
	m["tokens"] = map[string]string{
		"bg": "#101010", "surface": "#202020", "accent": "#ffc08a",
		"text": "#f5f5f5", "muted": "#b0b0b0",
		"font-display": `"DIN Condensed", sans-serif`,
	}

	pack, err := theme.Load(writePack(t, "testpack", m, ""))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	css := string(pack.CSS)
	if !strings.Contains(css, ":root {") {
		t.Fatalf("tokens were not emitted on :root:\n%s", css)
	}
	for _, want := range []string{
		"--bg: #101010;",
		"--surface: #202020;",
		"--accent: #ffc08a;",
		"--text: #f5f5f5;",
		"--muted: #b0b0b0;",
		`--font-display: "DIN Condensed", sans-serif;`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("generated stylesheet is missing %q:\n%s", want, css)
		}
	}
	// A token the manifest did not set must not appear at all, so that
	// base.css's own var() fallback is what answers for it.
	if strings.Contains(css, "--backdrop") {
		t.Errorf("an unset token was emitted anyway; base.css's fallback can no longer apply:\n%s", css)
	}
}

// The pack's own stylesheet comes after the generated token block, so a pack
// can build a derived value out of a token it just declared.
func TestStylesheetFollowsTheGeneratedTokens(t *testing.T) {
	pack, err := theme.Load(writePack(t, "testpack", minimalManifest(),
		":root {\n  --shadow: 0 2px 4px rgb(0 0 0 / 0.4);\n}\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	css := string(pack.CSS)
	tokens := strings.Index(css, "--accent:")
	sheet := strings.Index(css, "--shadow:")
	if tokens < 0 || sheet < 0 {
		t.Fatalf("expected both halves in the stylesheet:\n%s", css)
	}
	if sheet < tokens {
		t.Errorf("theme.css was emitted before the manifest's tokens; a pack could not derive from them")
	}
}

// A pack with no theme.css is a complete pack. Eight tokens in a manifest is a
// whole theme, and requiring an empty file would be requiring a file that says
// nothing.
func TestStylesheetIsOptional(t *testing.T) {
	pack, err := theme.Load(writePack(t, "testpack", minimalManifest(), ""))
	if err != nil {
		t.Fatalf("a pack with no theme.css should load: %v", err)
	}
	if !strings.Contains(string(pack.CSS), "--accent:") {
		t.Errorf("tokens missing from a manifest-only pack")
	}
}

func TestManifestNameMustMatchTheDirectory(t *testing.T) {
	m := minimalManifest()
	m["name"] = "something-else"

	if _, err := theme.Load(writePack(t, "testpack", m, "")); err == nil {
		t.Fatal("a manifest naming a different pack was accepted")
	}
}

// A misspelled key in hand-written JSON is the likeliest mistake anybody will
// make, and it has to say so rather than loading a pack with no colours.
func TestUnknownManifestFieldIsRejected(t *testing.T) {
	m := minimalManifest()
	m["colours"] = map[string]string{"bg": "#000"}

	_, err := theme.Load(writePack(t, "testpack", m, ""))
	if err == nil {
		t.Fatal("an unknown manifest field was accepted")
	}
	if !strings.Contains(err.Error(), "colours") {
		t.Errorf("the error does not name the offending field: %v", err)
	}
}

func TestRequiredTokensAreRequired(t *testing.T) {
	for _, missing := range theme.RequiredTokens {
		t.Run(missing, func(t *testing.T) {
			m := minimalManifest()
			tokens := m["tokens"].(map[string]string)
			delete(tokens, missing)

			_, err := theme.Load(writePack(t, "testpack", m, ""))
			if err == nil {
				t.Fatalf("a pack with no --%s was accepted", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("the error does not name the missing token: %v", err)
			}
		})
	}
}

// The manifest is the one place a pack could reach a private token by name.
// base.css floors every one of them with max() and would win anyway; this is
// the second lock, and it is the one that explains itself.
func TestManifestCannotSetAPrivateToken(t *testing.T) {
	for _, private := range []string{"_tap", "--_fs-body", "_focus-width"} {
		t.Run(private, func(t *testing.T) {
			m := minimalManifest()
			m["tokens"].(map[string]string)[private] = "0px"

			_, err := theme.Load(writePack(t, "testpack", m, ""))
			if err == nil {
				t.Fatalf("a manifest set %s and was accepted", private)
			}
			if !strings.Contains(err.Error(), "private") {
				t.Errorf("the error does not explain why: %v", err)
			}
		})
	}
}

func TestUnknownTokenIsRejected(t *testing.T) {
	m := minimalManifest()
	m["tokens"].(map[string]string)["accent-alt2"] = "#fff"

	_, err := theme.Load(writePack(t, "testpack", m, ""))
	if err == nil {
		t.Fatal("a typo'd token name was accepted")
	}
	if !strings.Contains(err.Error(), "accent-alt2") {
		t.Errorf("the error does not name the token: %v", err)
	}
}

// A token value that closes its own declaration stops being a value and starts
// being a rule, and that rule would sit on :root with a pack's name on it.
func TestTokenValueCannotEscapeItsDeclaration(t *testing.T) {
	escapes := map[string]string{
		"semicolon and braces": "#000; } html { font-size: 4px } :root { --x: #000",
		"closing brace":        "#000 }",
		"angle bracket":        "#000 </style>",
		"comment":              "#000 /* */",
		"newline":              "#000\n  --_tap: 8px",
		"unbalanced quote":     `"DIN Condensed, sans-serif`,
	}
	for name, value := range escapes {
		t.Run(name, func(t *testing.T) {
			m := minimalManifest()
			m["tokens"].(map[string]string)["accent"] = value

			if _, err := theme.Load(writePack(t, "testpack", m, "")); err == nil {
				t.Fatalf("a token value that escapes its declaration was accepted: %q", value)
			}
		})
	}
}

// A font stack cannot be written without quotes, so quotes have to be allowed.
// They are checked for balance instead.
func TestQuotedFontStacksAreAllowed(t *testing.T) {
	m := minimalManifest()
	m["tokens"].(map[string]string)["font-display"] = `"DIN Alternate", "Arial Narrow", sans-serif`

	pack, err := theme.Load(writePack(t, "testpack", m, ""))
	if err != nil {
		t.Fatalf("a quoted font stack was rejected: %v", err)
	}
	if !strings.Contains(string(pack.CSS), `"DIN Alternate"`) {
		t.Errorf("the font stack did not survive:\n%s", pack.CSS)
	}
}

// Everything below already loses to base.css. Refusing it here is about telling
// the pack author at load, with a line number, instead of letting them spend an
// evening on a rule that was never going to work.
func TestStylesheetRejectsWhatTheContractForbids(t *testing.T) {
	cases := map[string]string{
		"private token":  ":root { --_tap: 8px; }",
		"important":      ".btn { color: red !important; }",
		"focus":          "a:focus-visible { outline: none; }",
		"outline":        ".btn { outline: 0; }",
		"reduced motion": "@media (prefers-reduced-motion: reduce) { * { transition-duration: 2s; } }",
		"layer":          "@layer pack { :root { --bg: #000; } }",
		"import":         `@import url("other.css");`,
	}
	for name, css := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := theme.Load(writePack(t, "testpack", minimalManifest(), css))
			if err == nil {
				t.Fatalf("theme.css containing %q was accepted", css)
			}
			if !strings.Contains(err.Error(), theme.StylesheetName+":") {
				t.Errorf("the error does not point at a line in theme.css: %v", err)
			}
		})
	}
}

// A pack's stylesheet header is exactly where the banned words turn up in
// prose. "the outline is base.css's, not ours" must not be what stops the pack
// loading.
func TestStylesheetCommentsAreNotSearched(t *testing.T) {
	css := `/* This pack does not set an outline, does not use !important,
   and never touches :focus. @layer is base.css's. --_tap is private. */
:root {
  --shadow: none;
}
`
	if _, err := theme.Load(writePack(t, "testpack", minimalManifest(), css)); err != nil {
		t.Fatalf("a pack was refused for something it said in a comment: %v", err)
	}
}

func TestStylesheetErrorNamesTheLine(t *testing.T) {
	css := ":root {\n  --radius: 0;\n}\n\n.btn { outline: none; }\n"

	_, err := theme.Load(writePack(t, "testpack", minimalManifest(), css))
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if !strings.Contains(err.Error(), ":5:") {
		t.Errorf("the error does not point at line 5: %v", err)
	}
}

// Copy comes off the manifest and reaches the view model untouched, with
// everything the pack did not say still answering out of BaseCopy.
func TestCopyOverridesWhatItSetsAndInheritsTheRest(t *testing.T) {
	m := minimalManifest()
	m["copy"] = map[string]string{
		viewmodel.KeySlateEmptyHeading: "Nobody has volunteered yet",
		viewmodel.KeyFilmOne:           "test chamber",
		viewmodel.KeyFilmMany:          "test chambers",
	}

	pack, err := theme.Load(writePack(t, "testpack", m, ""))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got := pack.Theme.Text(viewmodel.KeySlateEmptyHeading); got != "Nobody has volunteered yet" {
		t.Errorf("pack copy did not win: %q", got)
	}
	if got := pack.Theme.Count(6, viewmodel.KeyFilmOne, viewmodel.KeyFilmMany); got != "6 test chambers" {
		t.Errorf("pack noun did not reach Count: %q", got)
	}
	// Not set by this pack, so the base string has to answer.
	if got := pack.Theme.Text(viewmodel.KeyError404Heading); got != viewmodel.BaseCopy[viewmodel.KeyError404Heading] {
		t.Errorf("an unset key did not fall back to BaseCopy: %q", got)
	}
}

// site.title and site.tagline are copy keys, and Resolve has to find them
// there, so that a manifest says each of them once.
func TestSiteTitleAndTaglineComeFromCopy(t *testing.T) {
	m := minimalManifest()
	m["copy"] = map[string]string{
		viewmodel.KeySiteTitle:   "Aperture Screening Initiative",
		viewmodel.KeySiteTagline: "Participation is, technically, voluntary.",
	}

	pack, err := theme.Load(writePack(t, "testpack", m, ""))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if pack.Theme.SiteTitle != "Aperture Screening Initiative" {
		t.Errorf("SiteTitle = %q", pack.Theme.SiteTitle)
	}
	if pack.Theme.Tagline != "Participation is, technically, voluntary." {
		t.Errorf("Tagline = %q", pack.Theme.Tagline)
	}
}

// The share image is the one image on the site guaranteed to be seen with no
// page around it.
func TestShareImageNeedsAltText(t *testing.T) {
	m := minimalManifest()
	m["og_image"] = "og.png"

	if _, err := theme.Load(writePack(t, "testpack", m, "")); err == nil {
		t.Fatal("a share image with no alt text was accepted")
	}
}

func TestShareImageBecomesAServedURL(t *testing.T) {
	m := minimalManifest()
	m["og_image"] = "og.png"
	m["og_image_alt"] = "A test card."

	pack, err := theme.Load(writePack(t, "testpack", m, ""))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if want := "/theme/testpack/assets/og.png"; pack.Theme.OGImage != want {
		t.Errorf("OGImage = %q, want %q", pack.Theme.OGImage, want)
	}
}

func TestPackCSSPointsAtTheGeneratedStylesheet(t *testing.T) {
	pack, err := theme.Load(writePack(t, "testpack", minimalManifest(), ""))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if want := "/theme/testpack/theme.css"; pack.Theme.PackCSS != want {
		t.Errorf("PackCSS = %q, want %q", pack.Theme.PackCSS, want)
	}
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// themesDir writes several packs into one directory, the way themes/ looks.
func themesDir(t *testing.T, packs map[string]map[string]any) string {
	t.Helper()

	root := t.TempDir()
	for name, manifest := range packs {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if manifest == nil {
			continue
		}
		raw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, theme.ManifestName), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

func named(name string) map[string]any {
	m := minimalManifest()
	m["name"] = name

	return m
}

// One bad pack must not take the others down with it, and must not take the
// server down at all. A trailing comma in a JSON file on the morning of
// 1 October is a theme that does not load, not a site that does not start.
func TestABrokenPackDoesNotStopTheOthers(t *testing.T) {
	broken := named("broken")
	broken["tokens"] = map[string]string{"bg": "#000"} // missing the rest

	root := themesDir(t, map[string]map[string]any{
		"good":   named("good"),
		"broken": broken,
	})

	r, err := theme.New(root, nil)
	if err != nil {
		t.Fatalf("New returned an error for one broken pack: %v", err)
	}
	if _, ok := r.Lookup("good"); !ok {
		t.Errorf("the good pack was lost")
	}
	if _, ok := r.Lookup("broken"); ok {
		t.Errorf("the broken pack was served anyway")
	}
}

// A directory with no manifest is not a failed pack, it is not a pack. themes/
// also holds README.md and the contrast checker.
func TestDirectoriesWithoutAManifestAreIgnored(t *testing.T) {
	root := themesDir(t, map[string]map[string]any{
		"good":    named("good"),
		"scratch": nil,
	})

	r, err := theme.New(root, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := r.Names(); len(got) != 1 || got[0] != "good" {
		t.Errorf("Names() = %v, want [good]", got)
	}
}

// No themes directory at all is the unthemed site, which is supported.
func TestAMissingThemesDirectoryIsNotAnError(t *testing.T) {
	r, err := theme.New(filepath.Join(t.TempDir(), "nope"), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(r.Names()) != 0 {
		t.Errorf("Names() = %v, want none", r.Names())
	}
}

// Three ways of asking for no theme, one answer: the zero Theme, which is
// base.css's own palette and BaseCopy.
func TestNoPackDegradesToTheUnthemedSite(t *testing.T) {
	r, err := theme.New(themesDir(t, map[string]map[string]any{"good": named("good")}), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, name := range []string{"", theme.DefaultPackName, "deleted-last-year"} {
		got := r.Theme(name)
		if got.IsSet() {
			t.Errorf("Theme(%q) returned a configured theme: %+v", name, got)
		}
		if heading := got.Text(viewmodel.KeySlateEmptyHeading); heading != viewmodel.BaseCopy[viewmodel.KeySlateEmptyHeading] {
			t.Errorf("Theme(%q) did not fall back to the base copy: %q", name, heading)
		}
	}
}

func TestNilRegistryAnswersLikeAnEmptyOne(t *testing.T) {
	var r *theme.Registry

	if _, ok := r.Lookup("portal"); ok {
		t.Errorf("a nil registry claimed to have a pack")
	}
	if r.Theme("portal").IsSet() {
		t.Errorf("a nil registry returned a configured theme")
	}
	if len(r.Names()) != 0 {
		t.Errorf("a nil registry listed packs")
	}
}

// ---------------------------------------------------------------------------
// The handler
// ---------------------------------------------------------------------------

func testRegistry(t *testing.T) (*theme.Registry, string) {
	t.Helper()

	m := named("good")
	m["og_image"] = "og.png"
	m["og_image_alt"] = "A test card."
	root := themesDir(t, map[string]map[string]any{"good": m})

	if err := os.MkdirAll(filepath.Join(root, "good", theme.AssetsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "good", theme.AssetsDir, "og.png"), []byte("not really a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A file beside the manifest, which must NOT become a public URL.
	if err := os.WriteFile(filepath.Join(root, "good", "notes.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := theme.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}

	return r, root
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

	return w
}

func TestHandlerServesTheGeneratedStylesheet(t *testing.T) {
	r, _ := testRegistry(t)

	w := get(t, r.Handler(nil), "/theme/good/theme.css")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), "--accent:") {
		t.Errorf("body did not contain the tokens:\n%s", w.Body.String())
	}
}

func TestHandlerServesAssets(t *testing.T) {
	r, _ := testRegistry(t)

	w := get(t, r.Handler(nil), "/theme/good/assets/og.png")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

// A pack directory is somewhere a designer drops files. Only assets/ is public.
func TestHandlerServesNothingOutsideAssets(t *testing.T) {
	r, _ := testRegistry(t)

	for _, path := range []string{
		"/theme/good/notes.txt",
		"/theme/good/manifest.json",
		"/theme/good/assets/../notes.txt",
		"/theme/good/assets/../../good/notes.txt",
		"/theme/good/",
		"/theme/good/assets/",
		"/theme/nosuchpack/theme.css",
		"/theme/../static/css/base.css",
	} {
		t.Run(path, func(t *testing.T) {
			if w := get(t, r.Handler(nil), path); w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404\n%s", w.Code, w.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ?theme= preview
// ---------------------------------------------------------------------------

// previewed renders whatever theme the middleware resolved, as a string a test
// can assert on.
func previewed() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if t, ok := theme.PreviewedTheme(r.Context()); ok {
			_, _ = w.Write([]byte("preview:" + t.PackCSS))

			return
		}
		_, _ = w.Write([]byte("no preview"))
	}
}

func TestPreviewIsOffForEverybodyByDefault(t *testing.T) {
	r, _ := testRegistry(t)

	w := get(t, r.Preview(nil, previewed()), "/?theme=good")
	if got := w.Body.String(); got != "no preview" {
		t.Errorf("a nil authorizer let a preview through: %q", got)
	}
}

func TestPreviewSwapsThePackForAnAuthorizedRequest(t *testing.T) {
	r, _ := testRegistry(t)

	w := get(t, r.Preview(theme.Anyone, previewed()), "/?theme=good")
	if want := "preview:/theme/good/theme.css"; w.Body.String() != want {
		t.Errorf("body = %q, want %q", w.Body.String(), want)
	}
}

// ?theme=none is the one preview whose whole value is that it does something:
// it is how you check the page still works with no pack, which is the promise
// themes/README.md makes.
func TestPreviewNoneIsTheUnthemedSiteAndNotNothing(t *testing.T) {
	r, _ := testRegistry(t)

	w := get(t, r.Preview(theme.Anyone, previewed()), "/?theme="+theme.PreviewNone)
	if want := "preview:"; w.Body.String() != want {
		t.Errorf("body = %q, want %q (an empty theme, deliberately previewed)", w.Body.String(), want)
	}
}

// A mistyped pack name shows the page, not a 400. The tool must not break the
// thing it exists to inspect.
func TestPreviewIgnoresAPackItDoesNotHave(t *testing.T) {
	r, _ := testRegistry(t)

	w := get(t, r.Preview(theme.Anyone, previewed()), "/?theme=portul")
	if got := w.Body.String(); got != "no preview" {
		t.Errorf("body = %q, want the page unchanged", got)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestPreviewIsRefusedForAnUnauthorizedRequest(t *testing.T) {
	r, _ := testRegistry(t)
	nobody := func(*http.Request) bool { return false }

	if got := get(t, r.Preview(nobody, previewed()), "/?theme=good").Body.String(); got != "no preview" {
		t.Errorf("an unauthorized request previewed a pack: %q", got)
	}
}

// A previewed page is one person's private view of a moment. Nothing between
// here and the browser should keep it.
func TestAPreviewedPageIsNotCached(t *testing.T) {
	r, _ := testRegistry(t)

	w := get(t, r.Preview(theme.Anyone, previewed()), "/?theme=good")
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestAnOrdinaryRequestIsUntouched(t *testing.T) {
	r, _ := testRegistry(t)

	w := get(t, r.Preview(theme.Anyone, previewed()), "/")
	if got := w.Body.String(); got != "no preview" {
		t.Errorf("body = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "" {
		t.Errorf("an unpreviewed page picked up Cache-Control: %q", got)
	}
}

// ---------------------------------------------------------------------------
// The real packs
// ---------------------------------------------------------------------------

// The two packs this ticket ships have to load, and have to keep loading. They
// are read from the repository rather than from a fixture so that a manifest
// edited in September is checked by this test in October.
func TestTheShippedPacksLoad(t *testing.T) {
	root := filepath.Join("..", "..", "themes")
	r, err := theme.New(root, nil)
	if err != nil {
		t.Fatalf("loading %s: %v", root, err)
	}

	for _, name := range []string{"portal", "elvira"} {
		t.Run(name, func(t *testing.T) {
			pack, ok := r.Lookup(name)
			if !ok {
				t.Fatalf("%s did not load; see the loader's log for why", name)
			}
			if pack.Label == "" {
				t.Errorf("no label")
			}
			// The voice is the point of a pack. One that overrides nothing is
			// a palette, and this ticket is not for palettes.
			if len(pack.Copy) < 20 {
				t.Errorf("%s overrides only %d copy keys; that is a palette, not a theme", name, len(pack.Copy))
			}
		})
	}
}

// Every key a pack sets has to exist in BaseCopy. A key that does not is either
// a typo — which renders as nothing and gives no reason — or a string the
// templates stopped using, which is worth deleting.
//
// This is a lint rather than a load-time error on purpose: a pack outlives the
// template that introduced its keys, and an archived 2026 pack must still
// render in 2029. It fails here, where somebody can fix it, and is inert in
// production, where nobody can.
func TestTheShippedPacksHaveNoUnknownCopyKeys(t *testing.T) {
	r, err := theme.New(filepath.Join("..", "..", "themes"), nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range r.Names() {
		pack, _ := r.Lookup(name)
		for key := range pack.Copy {
			if _, ok := viewmodel.BaseCopy[key]; !ok {
				t.Errorf("%s: copy key %q is not in viewmodel.BaseCopy (typo, or a string no template asks for any more)", name, key)
			}
		}
	}
}
