from __future__ import annotations

import importlib.util
import json
import sqlite3
import sys
import tempfile
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
BACKFILL = ROOT / "workflows" / "judge_backfill.py"
ELO = ROOT / "workflows" / "_swarm_elo.py"


def load_elo_module():
    spec = importlib.util.spec_from_loader("test_backfill_elo", SourceFileLoader("test_backfill_elo", str(ELO)))
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_backfill_module():
    sys.modules["_swarm_elo"] = load_elo_module()
    spec = importlib.util.spec_from_loader("judge_backfill_module", SourceFileLoader("judge_backfill_module", str(BACKFILL)))
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules["judge_backfill_module"] = module
    spec.loader.exec_module(module)
    return module


class JudgeBackfillTests(unittest.TestCase):
    def setUp(self) -> None:
        self.backfill = load_backfill_module()
        self.conn = sqlite3.connect(":memory:")
        self.conn.row_factory = sqlite3.Row
        self.conn.execute("CREATE TABLE swarm_dispatch_scores (pr_url TEXT)")

    def test_pr_without_ticket_skips(self) -> None:
        stats = self.backfill.process_prs(
            [{"number": 1, "url": "https://github.com/chitinhq/chitin/pull/1", "body": "no ticket", "headRefName": "feature/no-ticket"}],
            conn=self.conn,
        )
        self.assertEqual(stats["skipped_no_ticket"], 1)
        self.assertEqual(stats["scored"], 0)

    def test_already_scored_skips(self) -> None:
        self.conn.execute("INSERT INTO swarm_dispatch_scores (pr_url) VALUES (?)", ("https://github.com/chitinhq/chitin/pull/2",))
        stats = self.backfill.process_prs(
            [{"number": 2, "url": "https://github.com/chitinhq/chitin/pull/2", "body": "Closes t_1234abcd", "headRefName": "swarm/codex-1234abcd"}],
            conn=self.conn,
        )
        self.assertEqual(stats["skipped_existing"], 1)
        self.assertEqual(stats["scored"], 0)

    def test_inferred_default_path(self) -> None:
        calls = []

        def fake_run(ticket_id, pr_url, meta, *, dry_run=False):
            calls.append((ticket_id, pr_url, meta, dry_run))
            return mock.Mock(returncode=0, stdout="{}", stderr="")

        with mock.patch.object(self.backfill, "fetch_ticket_json", return_value={"comments": []}), \
             mock.patch.object(self.backfill, "run_judge", side_effect=fake_run):
            stats = self.backfill.process_prs(
                [{"number": 3, "url": "https://github.com/chitinhq/chitin/pull/3", "body": "Fixes t_deadbeef", "headRefName": "swarm/codex-deadbeef"}],
                conn=self.conn,
            )
        self.assertEqual(stats["scored"], 1)
        meta = calls[0][2]
        self.assertEqual((meta.driver, meta.model, meta.role), ("clawta", "gpt-5.5", "programmer"))
        self.assertTrue(meta.inferred)

    def test_comment_parse_path(self) -> None:
        ticket_json = {"comments": [{"body": "dispatch driver=codex model=gpt-5.4 role=programmer"}]}
        meta = self.backfill.parse_dispatch_meta_from_comments(ticket_json)
        self.assertIsNotNone(meta)
        assert meta is not None
        self.assertEqual((meta.driver, meta.model, meta.role, meta.inferred), ("codex", "gpt-5.4", "programmer", False))

    def test_error_isolation_between_prs(self) -> None:
        def fake_run(ticket_id, pr_url, meta, *, dry_run=False):
            if ticket_id == "t_badbad00":
                return mock.Mock(returncode=3, stdout="", stderr="judge failed")
            return mock.Mock(returncode=0, stdout="{}", stderr="")

        with mock.patch.object(self.backfill, "fetch_ticket_json", return_value={"comments": []}), \
             mock.patch.object(self.backfill, "run_judge", side_effect=fake_run):
            stats = self.backfill.process_prs(
                [
                    {"number": 4, "url": "https://github.com/chitinhq/chitin/pull/4", "body": "t_badbad00", "headRefName": "swarm/codex-badbad00"},
                    {"number": 5, "url": "https://github.com/chitinhq/chitin/pull/5", "body": "t_cafebabe", "headRefName": "swarm/codex-cafebabe"},
                ],
                conn=self.conn,
            )
        self.assertEqual(stats["failed"], 1)
        self.assertEqual(stats["scored"], 1)


if __name__ == "__main__":
    unittest.main()
