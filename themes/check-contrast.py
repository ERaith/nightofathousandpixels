#!/usr/bin/env python3
"""Check a theme pack's palette against the contrast floor in themes/README.md.

No dependencies — stdlib only, no build step, nothing to install.

    ./themes/check-contrast.py themes/portal/manifest.json
    ./themes/check-contrast.py --pair '#7cc4ff' '#101219'
    ./themes/check-contrast.py --defaults      # the base.css fallback palette

Exits non-zero if any required pair falls short, so it drops straight into
CI next to the Go tests.
"""

import json
import sys

# The palette baked into static/css/base.css as var() fallbacks. Kept here so
# the no-pack-loaded case is held to exactly the same standard as a pack.
DEFAULTS = {
    "bg": "#101219",
    "surface": "#1b1e29",
    "accent": "#7cc4ff",
    "accent-alt": "#ffc46b",
    "text": "#f4f5f9",
    "muted": "#aab2c6",
}

# (foreground, background, minimum ratio, why)
REQUIRED = [
    ("text", "bg", 4.5, "body text on the page"),
    ("text", "surface", 4.5, "body text on cards, inputs, table heads"),
    ("muted", "bg", 4.5, "de-emphasised text on the page"),
    ("muted", "surface", 4.5, "de-emphasised text on cards"),
    ("accent", "bg", 4.5, "links on the page AND .btn--primary label on accent"),
    ("accent", "surface", 4.5, "links and focus rings against a card"),
    ("accent-alt", "bg", 3.0, "non-text accents: bars, rules, borders"),
]


def parse_hex(value):
    s = str(value).strip().lstrip("#")
    if len(s) == 3:
        s = "".join(c * 2 for c in s)
    if len(s) == 8:          # #rrggbbaa — alpha ignored, flag it
        s = s[:6]
    if len(s) != 6:
        raise ValueError("expected a 3- or 6-digit hex colour, got %r" % value)
    return tuple(int(s[i:i + 2], 16) for i in (0, 2, 4))


def luminance(rgb):
    channels = []
    for raw in rgb:
        c = raw / 255
        channels.append(c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4)
    r, g, b = channels
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def ratio(fg, bg):
    a, b = luminance(parse_hex(fg)), luminance(parse_hex(bg))
    lo, hi = sorted((a, b))
    return (hi + 0.05) / (lo + 0.05)


def load(path):
    with open(path) as fh:
        data = json.load(fh)
    # Accept {"tokens": {...}} or a flat object; accept "--bg" or "bg".
    raw = data.get("tokens", data)
    return {k.lstrip("-"): v for k, v in raw.items() if isinstance(v, str)}


def report(tokens, label):
    print("Contrast check: %s\n" % label)
    failures = 0
    missing = 0
    for fg, bg, floor, why in REQUIRED:
        if fg not in tokens or bg not in tokens:
            print("  ?    --%-11s on --%-8s  (not set, base.css fallback used)" % (fg, bg))
            missing += 1
            continue
        try:
            r = ratio(tokens[fg], tokens[bg])
        except ValueError as exc:
            print("  FAIL --%-11s on --%-8s  %s" % (fg, bg, exc))
            failures += 1
            continue
        ok = r >= floor
        failures += 0 if ok else 1
        print("  %s --%-11s on --%-8s  %5.2f:1  (need %.1f)  %s"
              % ("PASS" if ok else "FAIL", fg, bg, r, floor, why))
    print()
    if failures:
        print("%d pair(s) below the floor. Adjust the palette, not base.css." % failures)
    elif missing:
        print("All set pairs pass; %d token(s) unset and falling back." % missing)
    else:
        print("All pairs pass.")
    return 1 if failures else 0


def main(argv):
    if "--defaults" in argv:
        return report(DEFAULTS, "base.css fallback palette (no pack loaded)")
    if "--pair" in argv:
        i = argv.index("--pair")
        fg, bg = argv[i + 1], argv[i + 2]
        r = ratio(fg, bg)
        print("%s on %s = %.2f:1  (4.5 = AA text, 3.0 = AA non-text)" % (fg, bg, r))
        return 0 if r >= 4.5 else 1
    if len(argv) != 2:
        print(__doc__)
        return 2
    return report(load(argv[1]), argv[1])


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
