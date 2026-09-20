# Theme packs

Night of a Thousand Pixels gets a new look every year. 2025 was ALIEN; 2026
will be Portal or Elvira, decided in late September. The point of a theme
*pack* is that swapping that look is a content change, not a rewrite.

A pack owns **colours, fonts, copy and assets**. That is the whole list.
Layout and accessibility live in `static/css/base.css` and are not
negotiable, because the 2025 site hardcoded its theme and we are not doing
that again.

Ticket F4 builds the Portal and Elvira packs. This file is the contract
they have to satisfy.

---

## The load order

```html
<link rel="stylesheet" href="/static/css/pack.css">   <!-- your pack FIRST -->
<link rel="stylesheet" href="/static/css/base.css">   <!-- base LAST      -->
```

Base loads **after** the pack. Not by accident, and not something to
"clean up" later — it is the enforcement mechanism. Everything base cares
about is either written against private `--_tokens` a pack cannot reach, or
flagged `!important` in the accessibility sections. A pack that writes
`outline: none`, shrinks the body text, shrinks a button or animates the
page for someone who asked for less motion simply loses.

If you want to see that happen, open `themes/preview.html`. The stand-in
pack at the top of that file tries all four of those things on purpose.

---

## What a pack may set

Eight custom properties, on `:root`. Set them on `:root` specifically —
base resolves them once at the document root, so tokens set on `body` or on
a wrapper div will not be picked up.

| Token | What it is | Requirement |
|---|---|---|
| `--bg` | Page background | — |
| `--surface` | Raised background: cards, inputs, table heads | — |
| `--accent` | Primary action colour: links, primary buttons, focus rings, vote bars | **4.5:1 against both `--bg` and `--surface`** |
| `--accent-alt` | Secondary accent, non-text decoration only | **3:1 against `--bg`** |
| `--text` | Body text | **4.5:1 against both `--bg` and `--surface`** |
| `--muted` | De-emphasised text: metadata, hints, footer | **4.5:1 against both `--bg` and `--surface`** |
| `--font-display` | Headings and the wordmark | — |
| `--font-body` | Everything else | — |

### Optional, decorative only

| Token | What it is | Notes |
|---|---|---|
| `--border` | Colour of rules, card edges and table lines | Aim for 3:1 against `--bg`. Form-control and button borders ignore this and derive their own colour, so a subtle value here cannot make an input disappear. |
| `--radius` | Corner radius | A single length. `0` for something brutalist, `999px` for something soft. |
| `--shadow` | Card shadow | Any valid `box-shadow`, or `none`. |
| `--backdrop` | A decorative background layer painted behind the page | Any valid `background` value. It is `position: fixed`, `pointer-events: none`, `z-index: -1`, cannot create scroll, and is hidden in print. |
| `--color-scheme` | `dark`, `light`, or `light dark` | Drives native scrollbars, form widgets and the caret. Set it to match your palette or the browser's own chrome will fight you. |

## What a pack must never do

- **Never set a `--_`-prefixed token.** Those are private to `base.css`. It
  ignores a smaller value where a floor exists (`max(44px, var(--_tap))`), and
  the ones carrying a guarantee no floor can express — `--_focus`,
  `--_focus-width`, `--_focus-offset`, `--_ok`, `--_warn`, `--_danger`,
  `--_on-accent` — are redeclared on every element inside
  `@layer nap-enforce`, so setting them has no effect anywhere. This is
  checked: `themes/test_enforce_floors.py` fails if a private token is read
  inside the enforce layer with neither a `max()` floor nor a seal.

  It is worth knowing what that stops, because it was live until recently.
  Three lines, no `!important`, no `@layer` —
  `html body { --_focus-width: 0px; --_focus-offset: 0px; --_focus: transparent }`
  — removed all 22 focus rings on `preview.html` while the text and
  tap-target floors held. Base won the cascade and painted a 0px transparent
  ring.
- **Never write layout.** No `display`, `grid-template-*`, `flex`,
  `position`, `width`, `max-width`, `margin` or `padding` on base's
  classes. If a theme genuinely needs a different arrangement, that is a
  change to `base.css` with a review, not a pack override.
- **Never touch focus.** No `:focus`, no `:focus-visible`, no `outline`.
  The ring already takes your `--accent`, which is the only customisation
  it needs — and the only one it will accept.
- **Never write `@media (prefers-reduced-motion: ...)`.** Base owns that
  block and switches your animations off inside it.
- **Never set a font-size below `1rem`.** 16px is the floor, everywhere,
  including captions and badges.
- **Never shrink an interactive element below 44×44px.**

Assets (fonts, background images, icons) belong in the pack's own
directory: `themes/portal/`, `themes/elvira/`.

---

## Why `--accent` needs 4.5:1 against `--bg`

Because it buys two things with one rule.

Base draws links as `--accent` on `--bg`. It draws primary buttons the
other way round: `--bg` text on an `--accent` background. Contrast is
symmetric, so a single guarantee — accent against bg — makes both pairings
readable. That is why the contract asks for the one ratio instead of
asking you to nominate a separate "text on accent" colour and hope you got
it right.

The same reasoning is why `--accent` also has to clear 4.5:1 against
`--surface`: links and focus rings appear on cards too.

## Checking it

`themes/check-contrast.py` is stdlib Python, no dependencies, and exits
non-zero on failure so it can sit in CI:

```console
$ ./themes/check-contrast.py themes/portal/manifest.json
$ ./themes/check-contrast.py --pair '#ffb454' '#0d1117'
$ ./themes/check-contrast.py --defaults      # base.css's own fallback palette
```

It reads either `{"tokens": {...}}` or a flat object, and accepts keys with
or without the leading `--`.

Ratios to aim for, not just scrape past: body text above 7:1 is
comfortable on a phone in a dark room, which is where most of these thirty
people will actually be reading the slate.

---

## If no pack loads at all

The site still works. Every token in `base.css` is declared as
`var(--token, <fallback>)`, and the fallback palette is a neutral dark
theme that passes the same contrast floor a pack has to pass — verified,
not assumed:

```console
$ ./themes/check-contrast.py --defaults
All pairs pass.
```

So a broken manifest, a 404 on the pack stylesheet or a half-finished pack
degrades to a plain, readable, fully usable site rather than to unstyled
HTML or to white-on-white.

---

## Building a pack

1. Copy the token block out of `themes/preview.html` as a starting point.
2. Put it in `themes/<name>/pack.css` under a single `:root { ... }`.
3. Run `./themes/check-contrast.py` against it until everything passes.
4. Point `themes/preview.html` at your pack (swap the stand-in `<style>`
   block for a `<link>`, keeping `base.css` last) and look at it at 320px
   wide as well as on a desktop.
5. Tab through the whole page. If you cannot see where the focus is at
   every stop, something in your pack is fighting base — find it and
   delete it.

### Manifest

`internal/theme` reads `manifest.json` and generates the stylesheet the
page links. `check-contrast.py` reads the same file:

```json
{
  "name": "portal",
  "label": "Portal",
  "description": "Aperture Science. Institutional orange and blue on near-black.",
  "tokens": {
    "bg": "#0b0d10",
    "surface": "#14181e",
    "accent": "#ff9f4a",
    "accent-alt": "#5cc8ff",
    "text": "#eef1f4",
    "muted": "#a7b0ba",
    "font-display": "\"DIN Alternate\", \"Arial Narrow\", sans-serif",
    "font-body": "ui-sans-serif, system-ui, sans-serif"
  },
  "og_image": "og.png",
  "og_image_alt": "Night of a Thousand Pixels 2026, as an Aperture testing notice.",
  "copy": { "…": "see below" }
}
```

`name` must match the directory, so a copied-and-renamed pack says so
instead of quietly answering to the old theme's name.

Unknown fields are rejected, not ignored. A manifest is hand-written JSON
with no schema in front of it and no compiler behind it, so `"colours"` for
`"tokens"` is the likeliest mistake anybody will make, and a pack that
loads, renders wrong and gives no reason is worse than one that refuses.

A pack directory can hold three things:

```
themes/portal/
  manifest.json      colours, fonts, copy, share image
  theme.css          optional; custom properties only
  assets/            fonts, images, icons
```

and the site serves exactly two URLs per pack:

```
/theme/portal/theme.css          generated: the :root block, then theme.css
/theme/portal/assets/og.png      anything under assets/
```

`assets/` is the only part of the directory that is public. `manifest.json`
and `theme.css` are the site's inputs, not its outputs, and a pack directory
is somewhere a designer drops files.

`og_image` names a file inside `assets/` and becomes the share card these
links get in the group chat. `og_image_alt` is required alongside it: it is
the one image on the whole site guaranteed to be seen out of context.

#### What the loader refuses

The loader is the second lock, not the first. `base.css` is the enforcement
— it loads last, holds every floor in `@layer nap-enforce`, and writes each
one as `max(<literal>, var(--token))`, so a pack hand-written into `static/`
by somebody who never went near this loader still loses to it. What the
loader adds is a *reason*, at load, with a line number, instead of a
surprise in a browser six weeks later. It refuses:

- a token that is not on the list above, including any `--_` private one;
- a token value containing `;`, `{`, `}`, `<`, `>`, a backslash, a newline
  or a CSS comment — a value that needs one of those is not a value, it is a
  rule, and it would be a rule on `:root` with a pack's name on it;
- a `theme.css` containing `--_`, `!important`, `:focus`, `outline`,
  `prefers-reduced-motion`, `@layer` or `@import`.

Quotes are allowed — a font stack cannot be written without them — and
checked for balance instead.

A pack that fails any of this is logged and skipped. The other packs load,
the server starts, and the pages that pack would have themed render in the
base palette with the base copy.

---

## Copy

`copy` is the fourth thing a pack owns, and it is the one that does most of
the work. The 2025 site was ALIEN and the part people reacted to was not the
green — it was that the button did not say "Submit".

```json
"copy": {
  "site.title": "Aperture Screening Initiative",
  "noun.film.one": "test chamber",
  "noun.film.many": "test chambers",
  "slate.empty.heading": "Nobody has volunteered yet",
  "submit.heading": "Submit a test chamber",
  "submit.blocked.at_limit.heading": "That is both of your proposals",
  "submit.blocked.at_limit.body_dated": "{quota} They are listed below. Evaluation begins on {date}."
}
```

Every key is optional. What a pack omits it inherits from
`viewmodel.BaseCopy`, which is not a placeholder set — it is the finished
copy of an unthemed Night of a Thousand Pixels. So a half-written pack
renders a whole site, and "Portal has nothing to say about error pages"
degrades to a good error page rather than to a key name.

The whole list of keys is the `Key*` constants in
`internal/web/viewmodel/copy.go`, with each one's base string next to it in
`BaseCopy`. A key that no longer exists there is inert rather than an error:
a pack outlives the templates that introduced it, and the 2026 pack has to
keep rendering in 2029 when somebody reads the archive.

### Facts are not copy

A pack writes the sentence. The view model writes the numbers, dates, names
and counts inside it, through `{placeholders}`:

| Placeholder | What arrives |
|---|---|
| `{count}` | a number already rendered with the pack's own noun: "6 test chambers" |
| `{quota}` | the whole "you have used all 2 proposals" sentence |
| `{season}` | the season's label |
| `{date}` | a formatted timestamp |
| `{title}` | a film's title |
| `{name}` | a person's display name |
| `{limit}` | a bare allowance number |
| `{status}` | an HTTP status code |

That is why `noun.pick.one` is a key and "1 pick left of 2" is not. A pack
may tell somebody they have been greedy; it may not tell them they have one
pick left when they have two.

Placeholders are named rather than positional (`%s`) because a pack author
rewrites these in a JSON file with no compiler: "{season} is in the archive"
has to survive being reordered into "the archive has {season} in it". An
unknown placeholder is left alone, so a typo shows as `{seasons}` on the
page — visible and findable, rather than a sentence with a hole in it.

Copy is text. templ escapes every string at render time, so a manifest
cannot put markup on a page, by accident or on purpose.

---

## Judging a pack

```console
$ go run ./cmd/themegallery -addr :8240
$ open http://localhost:8240/gallery
```

Every page of the site, in every state, under every pack, from the same
fixtures the template tests use — no database, no OIDC provider, no
migrations. It is a development tool and it opens the preview gate to
everybody, so it does not belong anywhere a stranger can reach it.

On a deployed site the same thing is `?theme=`, which works on any page for
a season admin (`season_member.is_admin`) and nobody else:

```
/?theme=portal
/?theme=elvira
/?theme=none      <- the unthemed site, asked for on purpose
```

`?theme=none` is the one worth remembering. "Does this page still work with
no pack at all?" is the question a pack author cannot answer from inside
their pack, and it is the promise this file makes further up.

A preview lasts exactly one request. It sets no cookie and is not
remembered, so it can be pasted into the group chat and opened on somebody
else's phone — and so that an admin who forgets about it does not spend
October looking at a pack nobody else can see.

### The list to walk

Under both packs, at 320px and on a desktop:

- the slate, empty — the morning of 1 October, and the state most people see
  first;
- the slate, full;
- the submit form;
- the submit page refusing — the cap, the closed window, the voting-only
  member;
- the 404.

Then tab through each one. If you cannot see where the focus is at every
stop, something in the pack is fighting base — find it and delete it.
