from __future__ import annotations

import importlib.util
import io
import json
import sys
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path
from unittest import mock


JUDGE = Path(__file__).resolve().parents[1] / "workflows" / "judge.py"
ELO = Path(__file__).resolve().parents[1] / "workflows" / "_swarm_elo.py"


def load_elo_module():
    spec = importlib.util.spec_from_loader("test_swarm_elo_dep", SourceFileLoader("test_swarm_elo_dep", str(ELO)))
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_judge_module():
    sys.modules["_swarm_elo"] = load_elo_module()
    spec = importlib.util.spec_from_loader("judge_module", SourceFileLoader("judge_module", str(JUDGE)))
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class JudgeTests(unittest.TestCase):
    def test_build_outcome_metadata_classifies_pr_state(self) -> None:
        judge = load_judge_module()
        metadata = judge.build_outcome_metadata(
            {"task": {"title": "Add database migration tests", "body": "Expand coverage for ELO schema."}},
            {
                "title": "feat: expand schema",
                "body": "Adds richer outcome keys",
                "additions": 180,
                "deletions": 20,
                "changed_files": 5,
                "commits": [{"sha": "a"}, {"sha": "b"}],
                "state": "OPEN",
                "is_draft": False,
                "merge_state_status": "CLEAN",
                "review_decision": "APPROVED",
                "created_at": "2026-05-13T10:00:00Z",
                "updated_at": "2026-05-13T11:00:00Z",
                "merged_at": None,
                "status_check_rollup": [{"conclusion": "SUCCESS"}],
            },
            "diff --git a/swarm/workflows/_swarm_elo.py b/swarm/workflows/_swarm_elo.py",
            "programmer",
            "feature",
        )
        self.assertEqual(metadata["complexity_bucket"], "medium")
        self.assertEqual(metadata["pr_outcome"], "open")
        self.assertEqual(metadata["ci_outcome"], "passed")
        self.assertEqual(metadata["review_outcome"], "approved")
        self.assertIn("database", metadata["capabilities"])
        self.assertEqual(metadata["pr_created_at"], 1778666400)

    def test_main_writes_rich_context(self) -> None:
        judge = load_judge_module()
        record_calls = []
        update_calls = []

        class FakeElo:
            def open_db(self):
                return object()

            def record_score(self, conn, *args, **kwargs):
                record_calls.append((args, kwargs))
                return 7

            def update_elo(self, conn, *args, **kwargs):
                update_calls.append((args, kwargs))
                return 1512.5

        stdout = io.StringIO()
        argv = [
            "judge.py",
            "--ticket", "t_6db63d0b",
            "--pr-url", "https://github.com/chitinhq/chitin/pull/321",
            "--driver", "codex",
            "--model", "gpt-5.5",
        ]
        with mock.patch.object(judge, "elo", FakeElo()), \
             mock.patch.object(judge, "fetch_ticket", return_value={"task": {"title": "Fix CI flake", "body": "Repair workflow test coverage"}}), \
             mock.patch.object(judge, "fetch_pr_summary", return_value={
                 "title": "fix: stabilize CI",
                 "body": "Adds tests and workflow fixes",
                 "additions": 90,
                 "deletions": 10,
                 "changed_files": 4,
                 "commits": [{"sha": "abc12345", "msg": "fix(ci): stabilize tests"}],
                 "state": "OPEN",
                 "is_draft": False,
                 "merge_state_status": "CLEAN",
                 "review_decision": "REVIEW_REQUIRED",
                 "created_at": "2026-05-13T10:00:00Z",
                 "updated_at": "2026-05-13T11:00:00Z",
                 "merged_at": None,
                 "status_check_rollup": [{"status": "IN_PROGRESS"}],
             }), \
             mock.patch.object(judge, "fetch_pr_diff", return_value="diff --git a/.github/workflows/test.yml b/.github/workflows/test.yml"), \
             mock.patch.object(judge, "call_judge", return_value={
                 "code_quality": 4,
                 "test_coverage": 5,
                 "scope_adherence": 5,
                 "efficiency": 4,
                 "review_friendliness": 4,
                 "reasoning": "Looks good.",
             }), \
             mock.patch.object(sys, "argv", argv), \
             mock.patch.object(sys, "stdout", stdout):
            rc = judge.main()

        self.assertEqual(rc, 0)
        self.assertTrue(record_calls and update_calls)
        _, record_kwargs = record_calls[0]
        _, update_kwargs = update_calls[0]
        self.assertEqual(record_kwargs["complexity_bucket"], "medium")
        self.assertEqual(record_kwargs["ci_outcome"], "pending")
        self.assertEqual(record_kwargs["review_outcome"], "pending")
        self.assertIn("ci", record_kwargs["capabilities"])
        self.assertEqual(update_kwargs["role"], "programmer")
        payload = json.loads(stdout.getvalue())
        self.assertEqual(payload["driver"], "codex")
        self.assertEqual(payload["model"], "gpt-5.5")
        self.assertEqual(payload["task_class"], "bugfix")


if __name__ == "__main__":
    unittest.main()
