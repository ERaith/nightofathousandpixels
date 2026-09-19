#!/usr/bin/env python3
"""Tests for check-contrast.py.

Stdlib only, like the script itself:

    python3 -m unittest discover -s themes -p 'test_*.py'
    ./themes/test_check_contrast.py

The exit codes are the contract, because CI reads them and nothing else:

    0  every required pair passes
    1  at least one pair is below the floor
    2  the invocation was wrong

The first version of the script returned 2 for every pack, passing or
failing, because it was called as main(sys.argv[1:]) but indexed argv as
though the program name were still there. A permanent red in CI carries no
signal, and the moment someone works around it the real failures go quiet —
so the pass path and the fail path are both tested here.
"""

import importlib.util
import json
import pathlib
import tempfile
import unittest

HERE = pathlib.Path(__file__).resolve().parent

# The script has a hyphen in its name, so it cannot be imported normally.
_spec = importlib.util.spec_from_file_location("check_contrast", HERE / "check-contrast.py")
cc = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(cc)

PASSING = {
    "bg": "#101219",
    "surface": "#1b1e29",
    "accent": "#7cc4ff",
    "accent-alt": "#ffc46b",
    "text": "#f4f5f9",
    "muted": "#aab2c6",
}

# Fails on exactly one pair: accent on surface. Everything else clears the
# floor, so a test that goes green here would be testing the wrong thing.
ONE_BAD_PAIR = dict(PASSING, accent="#4d86b8")


class TempPack:
    """Writes a pack to a temporary file and yields its path."""

    def __init__(self, payload, suffix=".json"):
        self.payload = payload
        self.suffix = suffix

    def __enter__(self):
        self.fh = tempfile.NamedTemporaryFile("w", suffix=self.suffix, delete=False)
        if isinstance(self.payload, str):
            self.fh.write(self.payload)
        else:
            json.dump(self.payload, self.fh)
        self.fh.close()
        return self.fh.name

    def __exit__(self, *exc):
        pathlib.Path(self.fh.name).unlink(missing_ok=True)


class ExitCodes(unittest.TestCase):
    """One argument is a pack path. This is the invocation the docstring
    documents and the one CI will use."""

    def test_passing_pack_exits_zero(self):
        with TempPack({"tokens": PASSING}) as path:
            self.assertEqual(cc.main([path]), 0)

    def test_failing_pack_exits_one(self):
        with TempPack({"tokens": ONE_BAD_PAIR}) as path:
            self.assertEqual(cc.main([path]), 1)

    def test_flat_object_is_accepted_too(self):
        with TempPack(PASSING) as path:
            self.assertEqual(cc.main([path]), 0)

    def test_dashed_token_names_are_accepted(self):
        with TempPack({"--" + k: v for k, v in PASSING.items()}) as path:
            self.assertEqual(cc.main([path]), 0)

    def test_defaults_palette_passes(self):
        self.assertEqual(cc.main(["--defaults"]), 0)


class BadInvocations(unittest.TestCase):
    """Everything here is exit 2, and none of it may raise."""

    def test_no_arguments(self):
        self.assertEqual(cc.main([]), 2)

    def test_two_pack_paths(self):
        with TempPack({"tokens": PASSING}) as path:
            self.assertEqual(cc.main([path, path]), 2)

    def test_the_old_bug_would_have_needed_a_dummy_first_argument(self):
        # Before the fix this was the ONLY way to make the script work, and
        # the documented one-argument form always returned 2.
        with TempPack({"tokens": PASSING}) as path:
            self.assertEqual(cc.main(["dummy", path]), 2)

    def test_missing_file(self):
        self.assertEqual(cc.main([str(HERE / "no-such-pack.json")]), 2)

    def test_not_json(self):
        with TempPack("this is not json", suffix=".json") as path:
            self.assertEqual(cc.main([path]), 2)

    def test_pair_with_a_missing_operand(self):
        self.assertEqual(cc.main(["--pair", "#ffffff"]), 2)

    def test_pair_with_a_nonsense_colour(self):
        self.assertEqual(cc.main(["--pair", "octarine", "#101219"]), 2)


class Pair(unittest.TestCase):
    def test_pair_passing(self):
        self.assertEqual(cc.main(["--pair", "#f4f5f9", "#101219"]), 0)

    def test_pair_failing(self):
        self.assertEqual(cc.main(["--pair", "#3a3f4a", "#101219"]), 1)


class Ratio(unittest.TestCase):
    """The analysis engine underneath. It was already correct; these pin it."""

    def test_black_on_white_is_21(self):
        self.assertAlmostEqual(cc.ratio("#000000", "#ffffff"), 21.0, places=2)

    def test_identical_colours_are_1(self):
        self.assertAlmostEqual(cc.ratio("#7cc4ff", "#7cc4ff"), 1.0, places=6)

    def test_ratio_is_symmetric(self):
        self.assertAlmostEqual(cc.ratio("#7cc4ff", "#101219"), cc.ratio("#101219", "#7cc4ff"), places=9)

    def test_shorthand_hex_expands(self):
        self.assertAlmostEqual(cc.ratio("#fff", "#000"), 21.0, places=2)

    def test_eight_digit_hex_ignores_alpha(self):
        self.assertAlmostEqual(cc.ratio("#ffffff00", "#000000"), 21.0, places=2)

    def test_bad_hex_raises(self):
        with self.assertRaises(ValueError):
            cc.ratio("#12345", "#000000")


if __name__ == "__main__":
    unittest.main(verbosity=2)
