#!/usr/bin/env python3
"""The structural half of the theme-pack contract, checked against base.css.

Stdlib only, like everything else in this directory:

    python3 -m unittest discover -s themes -p 'test_*.py'
    ./themes/test_enforce_floors.py

WHY THIS FILE EXISTS

base.css's section 4 states the contract in prose: a declaration inside
`@layer nap-enforce` wins the cascade, and it must then resolve to a value a
theme pack cannot lower. A pack can set any custom property on any
descendant, so a floor written as a bare `var(--_tap)` wins the cascade and
paints the pack's number anyway.

That prose was true of --_tap and --_fs-body and false of --_focus,
--_focus-width and --_focus-offset for three rounds of hardening, and nothing
noticed, because a poisoned focus ring is not something a stylesheet can be
read for. Three lines with no !important removed all 22 rings on
preview.html. This is the check that would have caught it: every private
token consumed inside the enforce layer has to be either FLOORED with max()
or SEALED in the layer itself.

It is a static check on purpose. A browser check needs a browser; this needs
nothing, so it can run in the same breath as the contrast check and go red on
the next token somebody adds without a floor.
"""

import pathlib
import re
import unittest

HERE = pathlib.Path(__file__).resolve().parent
BASE_CSS = HERE.parent / "static" / "css" / "base.css"

# Private tokens deliberately left floorable, with the floor that does it.
# A pack may RAISE these; max() is what keeps it from lowering them, and
# sealing them would take the raising away too.
FLOORED = {"--_tap", "--_fs-body"}


def read_base():
    return BASE_CSS.read_text(encoding="utf-8")


def enforce_blocks(css):
    """Every `@layer nap-enforce { ... }` body, with its starting line."""
    blocks = []
    for match in re.finditer(r"@layer\s+nap-enforce\s*\{", css):
        depth, i = 1, match.end()
        while depth:
            if css[i] == "{":
                depth += 1
            elif css[i] == "}":
                depth -= 1
            i += 1
        blocks.append((css[: match.start()].count("\n") + 1, css[match.end() : i - 1]))
    return blocks


def sealed_tokens(css):
    """Tokens redeclared with !important inside the enforce layer.

    A pack cannot set these anywhere, because the seal matches every element
    and an unlayered important loses to a layered one at any specificity.
    """
    sealed = set()
    for _, body in enforce_blocks(css):
        for name, value in re.findall(r"(--[\w-]+)\s*:([^;{}]*);", body):
            if "!important" in value:
                sealed.add(name)
    return sealed


def max_spans(text):
    """Character ranges covered by a max(...) call, including nested ones."""
    spans = []
    for match in re.finditer(r"\bmax\s*\(", text):
        depth, i = 1, match.end()
        while depth and i < len(text):
            if text[i] == "(":
                depth += 1
            elif text[i] == ")":
                depth -= 1
            i += 1
        spans.append((match.start(), i))
    return spans


def unprotected_uses(css):
    """Private tokens read inside the enforce layer with neither floor nor seal.

    Returns (token, line) pairs. A read is protected if the token is sealed,
    or if it sits inside a max() call - which is what "a pack may raise a
    floor, never lower it" means in practice.
    """
    sealed = sealed_tokens(css)
    findings = []
    for start_line, body in enforce_blocks(css):
        spans = max_spans(body)
        for match in re.finditer(r"var\(\s*(--_[\w-]+)", body):
            token = match.group(1)
            if token in sealed:
                continue
            if any(lo <= match.start() < hi for lo, hi in spans):
                continue
            findings.append((token, start_line + body[: match.start()].count("\n")))
    return findings


class EnforceLayerFloors(unittest.TestCase):
    """The invariant: nothing the layer reads can be lowered by a pack."""

    def setUp(self):
        self.css = read_base()

    def test_base_css_is_where_we_think_it_is(self):
        # A path typo would make every other test in this file pass on an
        # empty string, which is the failure mode this whole file is about.
        self.assertTrue(BASE_CSS.is_file(), f"{BASE_CSS} is missing")
        self.assertIn("@layer nap-enforce", self.css)

    def test_the_enforce_layer_is_not_empty(self):
        # Same reason: an enforce layer that matched nothing would satisfy
        # every assertion below without enforcing anything.
        self.assertGreaterEqual(len(enforce_blocks(self.css)), 4)

    def test_every_private_token_read_in_the_layer_is_floored_or_sealed(self):
        bad = unprotected_uses(self.css)
        self.assertEqual(
            bad,
            [],
            "these private tokens are read inside @layer nap-enforce with no max() "
            "floor and no seal, so a theme pack can set them on a descendant and "
            "the floor paints the pack's number:\n"
            + "\n".join(f"  {token} at base.css:{line}" for token, line in bad)
            + "\nEither wrap the read in max(<literal>, var(...)) or seal the token "
            "with !important inside the layer.",
        )

    def test_the_focus_tokens_are_sealed(self):
        # Named individually because these three are the regression: they are
        # the ones max() cannot express a floor for, and they were the ones
        # the max() fix skipped.
        sealed = sealed_tokens(self.css)
        for token in ("--_focus", "--_focus-width", "--_focus-offset"):
            self.assertIn(token, sealed, f"{token} is not sealed inside @layer nap-enforce")

    def test_the_base_owned_colours_are_sealed(self):
        # Section 1 calls these fixed and says a pack does not get to make an
        # error message unreadable. Until they were sealed, nothing said so.
        sealed = sealed_tokens(self.css)
        for token in ("--_ok", "--_warn", "--_danger", "--_on-accent"):
            self.assertIn(token, sealed, f"{token} is not sealed inside @layer nap-enforce")

    def test_the_raisable_floors_are_not_sealed(self):
        # The other half of the contract. Sealing --_tap would stop a pack
        # making targets BIGGER, which is a thing packs may do.
        sealed = sealed_tokens(self.css)
        for token in sorted(FLOORED):
            self.assertNotIn(
                token,
                sealed,
                f"{token} is sealed, so a pack can no longer raise it; it should be "
                "floored with max() instead",
            )
            self.assertRegex(
                self.css,
                re.compile(r"max\(\s*[^;()]*,\s*var\(\s*" + re.escape(token)),
                f"{token} has no max() floor anywhere",
            )


class TheCheckItself(unittest.TestCase):
    """The detector, against a stylesheet whose answer is known.

    Without these, a regex that silently matched nothing would make the suite
    above green on any input at all - the exact shape of bug it exists to
    catch.
    """

    SEALED_AND_FLOORED = """
    :root { --_focus-width: 3px; --_tap: 44px; }
    @layer nap-enforce {
      *, *::before, *::after { --_focus-width: 3px !important; }
      :focus-visible { outline: var(--_focus-width) solid red !important; }
      button { min-block-size: max(44px, var(--_tap)) !important; }
    }
    """

    POISONABLE = """
    :root { --_focus-width: 3px; }
    @layer nap-enforce {
      :focus-visible { outline: var(--_focus-width) solid red !important; }
    }
    """

    def test_a_sealed_and_floored_sheet_is_clean(self):
        self.assertEqual(unprotected_uses(self.SEALED_AND_FLOORED), [])

    def test_the_pre_fix_shape_is_caught(self):
        found = unprotected_uses(self.POISONABLE)
        self.assertEqual([token for token, _ in found], ["--_focus-width"])

    def test_a_seal_without_important_does_not_count(self):
        # An unlayered pack rule on a descendant beats a layered rule with no
        # !important, so a seal without the flag is not a seal.
        css = self.POISONABLE.replace(
            "@layer nap-enforce {", "@layer nap-enforce {\n  * { --_focus-width: 3px; }"
        )
        self.assertEqual([token for token, _ in unprotected_uses(css)], ["--_focus-width"])

    def test_reads_outside_the_layer_are_not_the_layer_s_problem(self):
        # .skip-link reads var(--_tap) bare in section 4; the enforce layer
        # re-floors it. Only reads INSIDE the layer are load-bearing.
        css = ".skip-link { min-block-size: var(--_tap); }\n@layer nap-enforce { a { color: red; } }"
        self.assertEqual(unprotected_uses(css), [])

    def test_nested_max_is_still_a_floor(self):
        css = "@layer nap-enforce { p { font-size: max(1rem, min(2rem, var(--_fs-body))) !important; } }"
        self.assertEqual(unprotected_uses(css), [])

    def test_blocks_are_found_through_nested_braces(self):
        css = "@layer nap-enforce { @media (min-width: 30em) { p { color: var(--_x); } } }\np { color: var(--_y); }"
        self.assertEqual([token for token, _ in unprotected_uses(css)], ["--_x"])


if __name__ == "__main__":
    unittest.main()
