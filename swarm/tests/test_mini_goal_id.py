from __future__ import annotations

import datetime
import sys
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from swarm.mini._internal.goalid import mint_goal_id, slugify


class TestSlugify(unittest.TestCase):
    def test_basic(self):
        self.assertEqual(slugify("Add mini launcher"), "add-mini-launcher")

    def test_first_four_words_only(self):
        self.assertEqual(
            slugify("one two three four five six"),
            "one-two-three-four",
        )

    def test_punctuation_collapsed(self):
        # punctuation becomes dashes; "/" between words yields a dash.
        self.assertEqual(slugify("Fix bug!! in   payment/flow"), "fix-bug-in-payment-flow")

    def test_max_len_truncates(self):
        long_phrase = "supercalifragilisticexpialidocious is a real word"
        self.assertLessEqual(len(slugify(long_phrase, max_len=20)), 20)

    def test_empty_falls_back(self):
        self.assertEqual(slugify(""), "goal")


class TestMintGoalId(unittest.TestCase):
    def test_format_is_slug_dash_8hex(self):
        gid = mint_goal_id("Add mini launcher")
        self.assertRegex(gid, r"^[a-z0-9-]+-[0-9a-f]{8}$")

    def test_deterministic_with_fixed_ts(self):
        ts = datetime.datetime(2026, 5, 19, 5, 0, 0, tzinfo=datetime.timezone.utc)
        a = mint_goal_id("Add mini launcher", now=ts)
        b = mint_goal_id("Add mini launcher", now=ts)
        self.assertEqual(a, b)

    def test_distinct_for_different_goals(self):
        ts = datetime.datetime(2026, 5, 19, 5, 0, 0, tzinfo=datetime.timezone.utc)
        a = mint_goal_id("Goal alpha", now=ts)
        b = mint_goal_id("Goal beta", now=ts)
        self.assertNotEqual(a, b)

    def test_empty_goal_raises(self):
        with self.assertRaises(ValueError):
            mint_goal_id("")
        with self.assertRaises(ValueError):
            mint_goal_id("   ")


if __name__ == "__main__":
    unittest.main()
