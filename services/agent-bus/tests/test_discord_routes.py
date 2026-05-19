"""Tests for the deterministic (board, audience) → channel resolver.

Boundary checklist (Knuth: name them up front):

- Empty routes table + no board + no audience → UnroutableError
- Only global default present → returns global for any input
- Board match present → wins over global
- Audience match (single agent) → wins over board
- Audience match (multiple agents, multiple matching rows) → highest
  priority wins; tie broken by older updated_at
- Explicit mute (channel_id=NULL) row matches → returns None (not raise)
- set_route is upsert: same (scope, key) twice → second wins
- unset_route returns True iff a row was removed
- set_route validation: invalid scope / empty key / non-'*' global key
- audience parsing strips whitespace, ignores empty entries
"""

from __future__ import annotations

import importlib.util
import os
import sys
import tempfile
import unittest
from pathlib import Path


SVC_DIR = Path(__file__).resolve().parents[1]


def _load(name: str):
    if str(SVC_DIR) not in sys.path:
        sys.path.insert(0, str(SVC_DIR))
    if name in sys.modules:
        del sys.modules[name]
    import importlib
    return importlib.import_module(name)


class DiscordRoutesTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.db_path = Path(self.tmp.name) / "bus.db"
        os.environ["AGENT_BUS_DB"] = str(self.db_path)
        self.db = _load("db")
        self.routes = _load("discord_routes")
        self.conn = self.db.connect(self.db_path)

    def tearDown(self) -> None:
        self.conn.close()
        self.tmp.cleanup()
        os.environ.pop("AGENT_BUS_DB", None)

    # ---- baseline: no routes at all ----

    def test_unroutable_when_no_routes(self):
        with self.assertRaises(self.routes.UnroutableError):
            self.routes.resolve_channel(self.conn, board=None, audience=None)

    def test_unroutable_when_board_has_no_route_and_no_global(self):
        with self.assertRaises(self.routes.UnroutableError):
            self.routes.resolve_channel(self.conn, board="chitin", audience="red")

    # ---- global default ----

    def test_global_default_used_when_nothing_else_matches(self):
        self.routes.set_route(self.conn, scope="global", key="*", channel_id="C-GLOBAL")
        self.assertEqual(
            self.routes.resolve_channel(self.conn, board=None, audience=None),
            "C-GLOBAL",
        )
        self.assertEqual(
            self.routes.resolve_channel(self.conn, board="chitin", audience="ares"),
            "C-GLOBAL",
        )

    # ---- precedence: audience > board > global ----

    def test_board_wins_over_global(self):
        self.routes.set_route(self.conn, scope="global", key="*", channel_id="C-GLOBAL")
        self.routes.set_route(self.conn, scope="board", key="chitin", channel_id="C-SWARM")
        self.assertEqual(
            self.routes.resolve_channel(self.conn, board="chitin", audience=None),
            "C-SWARM",
        )

    def test_audience_wins_over_board(self):
        self.routes.set_route(self.conn, scope="global", key="*", channel_id="C-GLOBAL")
        self.routes.set_route(self.conn, scope="board", key="chitin", channel_id="C-SWARM")
        self.routes.set_route(self.conn, scope="audience", key="clawta", channel_id="C-CLAWTA")
        self.assertEqual(
            self.routes.resolve_channel(self.conn, board="chitin", audience="clawta"),
            "C-CLAWTA",
        )

    def test_audience_for_multi_agent_picks_highest_priority(self):
        self.routes.set_route(self.conn, scope="audience", key="clawta",
                              channel_id="C-CLAWTA", priority=10)
        self.routes.set_route(self.conn, scope="audience", key="ares",
                              channel_id="C-ARES", priority=99)
        self.assertEqual(
            self.routes.resolve_channel(self.conn, board="chitin", audience="ares,clawta"),
            "C-ARES",
        )

    def test_audience_priority_tie_broken_by_older_updated_at(self):
        # Both priority=50; clawta's updated_at is older, so clawta wins.
        # Set updated_at directly to avoid wall-clock flakiness on fast boxes.
        self.routes.set_route(self.conn, scope="audience", key="clawta",
                              channel_id="C-CLAWTA", priority=50)
        self.routes.set_route(self.conn, scope="audience", key="ares",
                              channel_id="C-ARES", priority=50)
        self.conn.execute(
            "UPDATE discord_routes SET updated_at=? WHERE scope='audience' AND key=?",
            (1_000_000, "clawta"),
        )
        self.conn.execute(
            "UPDATE discord_routes SET updated_at=? WHERE scope='audience' AND key=?",
            (1_000_001, "ares"),
        )
        self.conn.commit()
        self.assertEqual(
            self.routes.resolve_channel(self.conn, board="chitin", audience="ares,clawta"),
            "C-CLAWTA",
        )

    # ---- explicit mute ----

    def test_mute_row_returns_none_not_error(self):
        self.routes.set_route(self.conn, scope="board", key="readybench",
                              channel_id=None)
        self.assertIsNone(
            self.routes.resolve_channel(self.conn, board="readybench", audience=None)
        )

    def test_audience_mute_wins_over_board_channel(self):
        self.routes.set_route(self.conn, scope="board", key="chitin", channel_id="C-SWARM")
        self.routes.set_route(self.conn, scope="audience", key="red", channel_id=None)
        # Posts to red specifically are intentionally muted (not in Discord)
        self.assertIsNone(
            self.routes.resolve_channel(self.conn, board="chitin", audience="red")
        )

    # ---- upsert + unset ----

    def test_set_route_is_upsert(self):
        self.routes.set_route(self.conn, scope="board", key="chitin", channel_id="C-1")
        self.routes.set_route(self.conn, scope="board", key="chitin", channel_id="C-2")
        rows = self.routes.list_routes(self.conn)
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0].channel_id, "C-2")

    def test_unset_returns_true_if_removed(self):
        self.routes.set_route(self.conn, scope="board", key="chitin", channel_id="C-1")
        self.assertTrue(self.routes.unset_route(self.conn, scope="board", key="chitin"))
        self.assertFalse(self.routes.unset_route(self.conn, scope="board", key="chitin"))

    # ---- validation ----

    def test_set_route_rejects_invalid_scope(self):
        with self.assertRaises(ValueError):
            self.routes.set_route(self.conn, scope="floor", key="x", channel_id="C")

    def test_set_route_rejects_empty_key(self):
        with self.assertRaises(ValueError):
            self.routes.set_route(self.conn, scope="audience", key="", channel_id="C")

    def test_set_route_rejects_non_star_global_key(self):
        with self.assertRaises(ValueError):
            self.routes.set_route(self.conn, scope="global", key="anything", channel_id="C")

    # ---- audience parsing ----

    def test_audience_whitespace_and_empty_entries_handled(self):
        self.routes.set_route(self.conn, scope="audience", key="clawta", channel_id="C-CLAWTA")
        for aud in ["clawta", " clawta ", "clawta,", ",clawta", ",,clawta,,"]:
            self.assertEqual(
                self.routes.resolve_channel(self.conn, board=None, audience=aud),
                "C-CLAWTA",
                msg=f"audience={aud!r}",
            )

    def test_empty_audience_string_falls_through(self):
        self.routes.set_route(self.conn, scope="board", key="chitin", channel_id="C-SWARM")
        self.assertEqual(
            self.routes.resolve_channel(self.conn, board="chitin", audience=""),
            "C-SWARM",
        )


if __name__ == "__main__":
    unittest.main()
