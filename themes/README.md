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

Ticket **F2** owns the manifest format and the Go loader that turns it into
`:root` custom properties; treat the shape below as the expected input to
this contract rather than as the final schema. `check-contrast.py` reads
it today:

```json
{
  "name": "portal",
  "label": "Portal",
  "tokens": {
    "bg": "#0d1117",
    "surface": "#161b22",
    "accent": "#ffb454",
    "accent-alt": "#7ee787",
    "text": "#f0f6fc",
    "muted": "#b1bac4",
    "font-display": "\"Portal Sans\", Georgia, serif",
    "font-body": "ui-sans-serif, system-ui, sans-serif"
  }
}
```

Theme *copy* (headings, button labels, the tagline) is ticket **F3** and
lands in the same manifest under a `copy` key, with base strings as the
fallback. Nothing in this file changes when that arrives.
