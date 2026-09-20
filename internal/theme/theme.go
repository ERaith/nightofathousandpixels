// Package theme loads theme packs off disk and serves them.
//
// A pack is a directory under themes/, named by season.theme_pack:
//
//	themes/portal/
//	  manifest.json      colours, fonts, copy, share image
//	  theme.css          custom properties only, optional
//	  assets/            fonts, images, icons
//
// What this package produces for a page is one stylesheet URL and one Copy
// map. The stylesheet is generated: a :root block built from the manifest's
// tokens, followed by the pack's own theme.css. That is deliberately the only
// way a pack's CSS reaches the browser, because it means every declaration the
// site emits on the pack's behalf went through the validator below.
//
// # THE CONTRACT THIS PACKAGE ENFORCES, AND THE ONE IT DOES NOT
//
// static/css/base.css is the real enforcement. It loads after the pack, holds
// every accessibility floor inside @layer nap-enforce, and writes each floor as
// max(<literal>, var(--token)) so that a pack winning the cascade with a
// poisoned token wins nothing. None of that depends on this package: a pack
// hand-written into static/ by somebody who never called this code still loses
// to base.css, and that is the property that matters.
//
// What this package adds is a second lock on the cheapest attack. It refuses to
// serve a pack that names a --_private token, writes !important, touches :focus
// or opens a prefers-reduced-motion block — not because base.css cannot survive
// those, but because a pack containing them is a pack whose author has
// misunderstood the deal, and the useful moment to say so is at load, by name,
// with a line number, rather than in a browser six weeks later.
//
// The two locks fail differently on purpose. This one fails loudly and early
// and can be wrong; base.css fails silently and always and cannot be. A
// validation gap here is a missed warning. A gap there is a broken page.
//
// # DEGRADING
//
// A pack that does not load is not an error the site propagates. Registry.Load
// reports what it could not read and the site runs with the packs it got, which
// for the unloadable one means the base palette and the base copy — a plain,
// readable, fully usable site, as themes/README.md promises. Nothing here can
// take the site down, which is the point: a theme is decoration, and decoration
// that can 500 a voting page is not decoration.
package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ERaith/nightofathousandpixels/internal/web/viewmodel"
)

// ViewTheme is viewmodel.Theme under a shorter name, because this package
// hands one back from four places and "theme.ViewTheme" reads better at those
// call sites than "viewmodel.Theme" does. It is an alias, not a copy: there is
// one Theme type in this codebase and this is it.
type ViewTheme = viewmodel.Theme

// ManifestName and StylesheetName are the two files a pack directory may hold
// at its root. AssetsDir is where everything else goes.
const (
	ManifestName   = "manifest.json"
	StylesheetName = "theme.css"
	AssetsDir      = "assets"
)

// DefaultPackName is what season.theme_pack holds when nobody has chosen a
// theme: migration 00002 defaults the column to 'default'. It names no
// directory on purpose. Asking for it is asking for the unthemed site, which is
// base.css's own contrast-checked palette and viewmodel.BaseCopy.
const DefaultPackName = "default"

// PublicTokens is the whole of what a manifest may set, and the order they are
// emitted in.
//
// It is a list rather than a check for a "--_" prefix because a deny-list is
// the wrong shape here: the question is not "is this one of the private tokens
// base.css uses today" but "is this one of the eight things themes/README.md
// says a pack owns". A token base.css adds next year is private by default
// under this rule, with nobody having to remember to add it to a list.
var PublicTokens = []string{
	// The required palette and type.
	"bg", "surface", "accent", "accent-alt", "text", "muted",
	"font-display", "font-body",
	// Optional, decorative only.
	"border", "radius", "shadow", "backdrop", "color-scheme",
}

// RequiredTokens are the ones with no sensible half-state. A pack that sets an
// accent and no background is not a theme, it is a bug that happens to parse,
// and the contrast floor cannot even be checked without both sides of a pair.
var RequiredTokens = []string{"bg", "surface", "accent", "text", "muted"}

// Manifest is a pack's manifest.json.
//
// Unknown fields are rejected. A manifest is hand-written JSON with no schema
// in front of it and no compiler behind it, so "colours" instead of "tokens"
// or "copys" instead of "copy" is the likeliest mistake anybody will make, and
// silently ignoring it means a pack that loads, renders wrong and gives no
// reason.
type Manifest struct {
	// Name must match the directory. It is checked rather than derived so that
	// a copied-and-renamed pack says so instead of quietly answering to the
	// old theme's name.
	Name string `json:"name"`

	// Label is the pack's human name for an admin picking between them:
	// "Portal", "Elvira". It never reaches a visitor's page.
	Label string `json:"label"`

	// Description is a sentence for the same admin. Also never rendered.
	Description string `json:"description"`

	// Tokens are the CSS custom properties, without the leading "--". See
	// PublicTokens for the entire permitted set.
	Tokens map[string]string `json:"tokens"`

	// Copy is the pack's wording, keyed by the viewmodel.Key* constants. Every
	// key is optional; what a pack omits it inherits.
	Copy map[string]string `json:"copy"`

	// OGImage is a file inside the pack's assets/ directory, given relative to
	// it ("og.png"). It becomes a served URL. Blank means the pack has no
	// share image and the link preview falls back to a text card, which is
	// what layout.templ already does.
	OGImage string `json:"og_image"`

	// OGImageAlt describes that image. Required when OGImage is set: a share
	// image with no alt text is the one image on the whole site guaranteed to
	// be seen out of context.
	OGImageAlt string `json:"og_image_alt"`
}

// Pack is a loaded, validated theme pack.
type Pack struct {
	// Name is the directory name, which is also season.theme_pack.
	Name string

	// Label and Description are the manifest's, for an admin's pack picker.
	Label       string
	Description string

	// CSS is the whole generated stylesheet: the :root token block, then the
	// pack's own theme.css. Held in memory because it is a few kilobytes and
	// re-reading two files per request to save that is not a trade.
	CSS []byte

	// Copy is the pack's wording as the view model consumes it.
	Copy viewmodel.Copy

	// Theme is what a handler puts on a page: the pack stylesheet's URL, the
	// site title and tagline from the pack's own copy, and the share image.
	Theme viewmodel.Theme

	// Dir is where the pack lives on disk, for serving assets/.
	Dir string
}

// Load reads and validates one pack directory.
//
// The returned error names the file and, for a stylesheet problem, the line.
// Somebody is going to read this at eleven at night in late September with two
// packs open and a decision to make, and "themes/portal/theme.css:14: a pack
// may not set a private --_ token" is the difference between a fix and a
// rewrite.
func Load(dir string) (Pack, error) {
	name := filepath.Base(dir)

	raw, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return Pack{}, fmt.Errorf("theme %q: %w", name, err)
	}

	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Pack{}, fmt.Errorf("%s: %w", filepath.Join(dir, ManifestName), err)
	}

	if m.Name != name {
		return Pack{}, fmt.Errorf("%s: name is %q but the directory is %q; they have to match",
			filepath.Join(dir, ManifestName), m.Name, name)
	}
	if err := validateName(name); err != nil {
		return Pack{}, err
	}
	if err := validateTokens(m.Tokens); err != nil {
		return Pack{}, fmt.Errorf("%s: %w", filepath.Join(dir, ManifestName), err)
	}
	if m.OGImage != "" && m.OGImageAlt == "" {
		return Pack{}, fmt.Errorf("%s: og_image is set but og_image_alt is empty; "+
			"the share image is the one image on this site that is always seen out of context",
			filepath.Join(dir, ManifestName))
	}

	sheet, err := readStylesheet(dir)
	if err != nil {
		return Pack{}, err
	}

	pack := Pack{
		Name:        name,
		Label:       orName(m.Label, name),
		Description: m.Description,
		CSS:         buildCSS(m, sheet),
		Copy:        viewmodel.Copy(m.Copy),
		Dir:         dir,
	}
	pack.Theme = viewmodel.Theme{
		PackCSS:    StylesheetPath(name),
		Copy:       pack.Copy,
		OGImage:    ogImagePath(name, m.OGImage),
		OGImageAlt: m.OGImageAlt,
	}
	// SiteTitle and Tagline come out of the pack's own copy, so a manifest says
	// them once under site.title / site.tagline and both the <head> and the
	// wordmark get them. Resolve fills in the base strings when it does not.
	pack.Theme = pack.Theme.Resolve()

	return pack, nil
}

// readStylesheet reads theme.css, which is optional: a pack whose entire look
// is eight tokens in a manifest needs no stylesheet, and requiring an empty
// file would be requiring a file that says nothing.
func readStylesheet(dir string) ([]byte, error) {
	p := filepath.Join(dir, StylesheetName)

	sheet, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("theme %q: %w", filepath.Base(dir), err)
	}
	if err := validateStylesheet(sheet); err != nil {
		return nil, fmt.Errorf("%s%w", p, err)
	}

	return sheet, nil
}

// StylesheetPath is the URL a pack's generated stylesheet is served at. It is
// what goes into Theme.PackCSS and therefore into the <link> that base.css
// loads after.
func StylesheetPath(name string) string {
	return path.Join(URLPrefix, name, StylesheetName)
}

// AssetPath is the URL for a file inside a pack's assets/ directory.
func AssetPath(name, rel string) string {
	return path.Join(URLPrefix, name, AssetsDir, rel)
}

func ogImagePath(name, rel string) string {
	if rel == "" {
		return ""
	}

	return AssetPath(name, rel)
}

func orName(label, name string) string {
	if label != "" {
		return label
	}

	return name
}

// buildCSS assembles the served stylesheet.
//
// Tokens first, in PublicTokens order rather than map order, because a
// stylesheet that reshuffles itself between server restarts makes a diff of
// what actually shipped useless. Then theme.css verbatim, so a pack can build
// derived values (a gradient out of its own accent, say) on top of what the
// manifest just declared.
func buildCSS(m Manifest, sheet []byte) []byte {
	var b strings.Builder

	b.WriteString("/* Generated from ")
	b.WriteString(m.Name)
	b.WriteString("/")
	b.WriteString(ManifestName)
	b.WriteString(". Do not edit: edit the manifest. */\n:root {\n")
	for _, token := range PublicTokens {
		value, ok := m.Tokens[token]
		if !ok {
			continue
		}
		b.WriteString("  --")
		b.WriteString(token)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString(";\n")
	}
	b.WriteString("}\n")

	if len(sheet) > 0 {
		b.WriteString("\n/* ")
		b.WriteString(m.Name)
		b.WriteString("/")
		b.WriteString(StylesheetName)
		b.WriteString(" */\n")
		b.Write(sheet)
		if sheet[len(sheet)-1] != '\n' {
			b.WriteString("\n")
		}
	}

	return []byte(b.String())
}

// validateName keeps a pack name to something that is safe as one path segment
// and as one URL segment, so that neither StylesheetPath nor the assets handler
// has to defend itself against a name from the database.
func validateName(name string) error {
	if name == "" {
		return errors.New("theme: a pack name cannot be empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("theme %q: pack name is longer than 64 characters", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return fmt.Errorf("theme %q: pack names are lowercase letters, digits, - and _ "+
				"(they are a directory name and a URL segment)", name)
		}
	}

	return nil
}

// validateTokens holds a manifest to the eight-plus-five list in
// themes/README.md.
//
// The unknown-token case is an error rather than a shrug because the two ways
// to get there are both worth stopping: a typo ("accent-alt2"), which renders
// nothing and gives no reason, and a deliberate reach at a private token, which
// is the thing the whole load-order contract exists to prevent.
func validateTokens(tokens map[string]string) error {
	if len(tokens) == 0 {
		return errors.New("tokens is empty; a pack with no colours is not a pack")
	}

	allowed := make(map[string]bool, len(PublicTokens))
	for _, t := range PublicTokens {
		allowed[t] = true
	}

	unknown := make([]string, 0, 2)
	for name, value := range tokens {
		clean := strings.TrimPrefix(name, "--")
		if strings.HasPrefix(clean, "_") {
			return fmt.Errorf("token %q is private to base.css; a pack may not set a --_ token "+
				"(base.css floors every one of them with max(), so this would not have worked either)", name)
		}
		if !allowed[clean] {
			unknown = append(unknown, name)

			continue
		}
		if err := validateTokenValue(clean, value); err != nil {
			return err
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)

		return fmt.Errorf("unknown token(s) %s; a pack may set %s",
			strings.Join(unknown, ", "), strings.Join(PublicTokens, ", "))
	}

	missing := make([]string, 0, len(RequiredTokens))
	for _, t := range RequiredTokens {
		if _, ok := tokens[t]; !ok {
			if _, ok := tokens["--"+t]; !ok {
				missing = append(missing, t)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required token(s) %s", strings.Join(missing, ", "))
	}

	return nil
}

// cssValueBanned are the characters that would let a token value stop being a
// value.
//
// This is the one genuine injection surface in a manifest. A token written as
// `#000; } html { font-size: 4px } :root { --x:` closes the declaration and the
// :root block and writes whatever it likes — layout, a 4px root font size, an
// outline: none. The floors in base.css would still hold, so the page would
// stay usable, but everything below the floors would be the pack's, and that is
// not the deal.
//
// Escaping would be the alternative and is worse: CSS escaping is subtle, and a
// rule that silently rewrites a value gives a pack author a stylesheet that is
// not what they wrote. Refusing at load says so.
//
// Quotes are NOT on this list, because a font stack cannot be written without
// them — "DIN Condensed" is two words and CSS needs to be told so. They are
// checked for balance instead, further down: a quote cannot terminate a
// declaration, but a dangling one changes where the next one ends.
const cssValueBanned = ";{}<>\\`\n\r"

// validateTokenValue rejects a value that could break out of its declaration.
//
// It is intentionally not a CSS parser. Everything a pack legitimately writes
// is a colour, a length, a font stack, a box-shadow or a background — none of
// which needs a semicolon, a brace, an angle bracket or a backslash. A value
// that does need one of those is a value that is doing something other than
// being a value.
func validateTokenValue(name, value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return fmt.Errorf("token %q is empty; omit it instead and let base.css's fallback stand", name)
	}
	if len(v) > 512 {
		return fmt.Errorf("token %q is longer than 512 characters", name)
	}
	if i := strings.IndexAny(v, cssValueBanned); i >= 0 {
		return fmt.Errorf("token %q contains %q, which cannot appear in a custom property value "+
			"(a value that needs one is not a value, it is a rule)", name, v[i])
	}
	if strings.Contains(v, "/*") || strings.Contains(v, "*/") {
		return fmt.Errorf("token %q contains a CSS comment", name)
	}
	if strings.Contains(strings.ToLower(v), "@import") {
		return fmt.Errorf("token %q contains @import", name)
	}
	if strings.Count(v, `"`)%2 != 0 || strings.Count(v, "'")%2 != 0 {
		return fmt.Errorf("token %q has an unbalanced quote", name)
	}

	return nil
}

// bannedInStylesheet is what a pack's own theme.css may not contain, with the
// reason attached so the error can say it.
//
// Every one of these already loses to base.css — see the package comment — so
// none of this is what keeps the site accessible. It is what keeps a pack
// author from spending an evening on a rule that was never going to work and
// concluding the stylesheet is broken.
var bannedInStylesheet = []struct {
	needle string
	why    string
}{
	{"--_", "a pack may not set a private --_ token; base.css redefines them after the pack " +
		"and floors each one with max(), so the only effect is that the pack and the page disagree"},
	{"!important", "a pack may not use !important; @layer nap-enforce in base.css beats an " +
		"unlayered important at any specificity, so this loses and reads as if it should not"},
	{":focus", "a pack may not style focus; the ring already takes --accent, which is the " +
		"only customisation it needs"},
	{"outline", "a pack may not set an outline; see :focus"},
	{"prefers-reduced-motion", "base.css owns that block and switches a pack's animations off " +
		"inside it; a pack's own copy of it cannot re-enable them"},
	{"@layer", "a pack may not open a layer; layering is how base.css's floors win, and a " +
		"pack that layers is a pack trying to join that fight"},
	{"@import", "a pack may not @import; everything it loads belongs in its own assets/"},
}

// validateStylesheet checks theme.css and points at the line.
//
// Comments are stripped first, and across line boundaries, because these packs
// carry long explanatory headers and the header is exactly where the word
// "outline" turns up — "the outline is base.css's, not ours" must not be the
// thing that refuses to load the pack that says it. Stripping preserves
// newlines so the reported line number is still the line in the file.
//
// It returns an error whose text begins with ":<line>: " so the caller can
// prefix the file path and get an editor-clickable message.
func validateStylesheet(sheet []byte) error {
	for i, line := range strings.Split(stripCSSComments(string(sheet)), "\n") {
		lower := strings.ToLower(line)
		for _, banned := range bannedInStylesheet {
			if strings.Contains(lower, banned.needle) {
				return fmt.Errorf(":%d: %s", i+1, banned.why)
			}
		}
	}

	return nil
}

// stripCSSComments blanks every /* ... */ while keeping the newlines inside it,
// so offsets by line survive. An unterminated comment swallows the rest of the
// file, which is what a browser does with one too.
func stripCSSComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "/*") {
			end := strings.Index(s[i+2:], "*/")
			var comment string
			if end < 0 {
				comment, i = s[i:], len(s)
			} else {
				comment, i = s[i:i+2+end+2], i+2+end+2
			}
			b.WriteString(strings.Repeat("\n", strings.Count(comment, "\n")))

			continue
		}
		b.WriteByte(s[i])
		i++
	}

	return b.String()
}
