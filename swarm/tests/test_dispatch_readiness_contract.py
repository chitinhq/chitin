"""Spec 022 — Dispatch readiness contract regression tests.

Static analysis + integration tests enforcing R1–R5 from spec 022.

R1: Single source of truth — watchdog consumes board_resolver.
R2: Board config requires spec_source; board_resolver uses it.
R3: kanban-flow ready rejects NULL assignee.
R4: Operator runbook exists with all five checks.
R5: Watchdog output includes spec-root decision telemetry.
"""
from __future__ import annotations

import json
import os
import sqlite3
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WATCHDOG = ROOT / "swarm" / "bin" / "board-watchdog-bounded.py"
RESOLVER = ROOT / "swarm" / "bin" / "board_resolver.py"
KANBAN_FLOW = ROOT / "scripts" / "kanban-flow"
RUNBOOK = ROOT / "docs" / "governance-setup-extras" / "dispatch-readiness.md"
BOARDS_DIR = Path(os.environ.get(
    "KANBAN_BOARDS_DIR",
    str(Path.home() / ".hermes" / "kanban" / "boards"),
))


class TestR1SingleSourceOfTruth(unittest.TestCase):
    """R1: No code outside board_resolver constructs a spec_root path."""

    def test_watchdog_imports_board_resolver(self):
        """Watchdog must import and call board_resolver functions."""
        src = WATCHDOG.read_text()
        self.assertIn("from board_resolver import", src,
                       "watchdog must import from board_resolver")

    def test_watchdog_uses_spec_dir_for_board(self):
        """Watchdog must call spec_dir_for_board, not construct paths."""
        src = WATCHDOG.read_text()
        self.assertIn("_spec_dir", src,
                       "watchdog must use _spec_dir (imported from board_resolver)")

    def test_watchdog_no_hardcoded_spec_root(self):
        """Watchdog must not contain hardcoded spec_root paths in a BOARDS dict."""
        src = WATCHDOG.read_text()
        # The old BOARDS dict had entries like:
        # "spec_root": CHITIN_REPO / ".specify" / "specs"
        self.assertNotIn('"spec_root"', src,
                         "watchdog must not have a spec_root key in a BOARDS dict")

    def test_watchdog_no_workspace_root_specify(self):
        """No hardcoded WORKSPACE_ROOT / .specify / specs path construction."""
        src = WATCHDOG.read_text()
        # The old code had: WORKSPACE_ROOT / "bench-devs-platform" / ".specify" / "specs"
        # and other constructions. After R1, watchdog should not construct these.
        # Allow legitimate env-var defaults in the module-level constants.
        # Only ban the pattern of constructing a spec_root from WORKSPACE_ROOT + path.
        for line in src.splitlines():
            # Skip comment lines
            stripped = line.strip()
            if stripped.startswith("#"):
                continue
            # Ban: WORKSPACE_ROOT / ".specify" or WORKSPACE_ROOT / "bench-devs-platform"
            if 'WORKSPACE_ROOT' in line and '".specify"' in line and not line.strip().startswith("KANBAN_BOARDS_DIR"):
                self.fail(f"watchdog constructs a spec path instead of using board_resolver: {line.strip()}")

    def test_watchdog_calls_resolve_db(self):
        """Watchdog must use board_resolver.resolve_db for DB paths."""
        src = WATCHDOG.read_text()
        self.assertIn("_resolve_db", src,
                       "watchdog must use _resolve_db from board_resolver")


class TestR2ExplicitSpecSource(unittest.TestCase):
    """R2: Board config must have spec_source; board_resolver reads it."""

    def _board_config(self, slug: str) -> dict:
        cfg_path = BOARDS_DIR / slug / "config.json"
        if not cfg_path.exists():
            self.skipTest(f"no config for board {slug}")
        return json.loads(cfg_path.read_text())

    def test_chitin_config_has_spec_source(self):
        cfg = self._board_config("chitin")
        self.assertIn("spec_source", cfg,
                       "chitin config must declare spec_source")

    def test_readybench_config_has_spec_source(self):
        cfg = self._board_config("readybench")
        self.assertIn("spec_source", cfg,
                       "readybench config must declare spec_source")

    def test_spec_source_repo_resolves_to_workspace(self):
        """board_resolver.spec_dir_for_board('chitin') should resolve to workspace/.specify/specs."""
        # We import board_resolver directly
        import importlib.util
        spec = importlib.util.spec_from_file_location("board_resolver", RESOLVER)
        br = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(br)

        result = br.spec_dir_for_board("chitin")
        self.assertIn(".specify/specs", str(result))

    def test_spec_source_resolution_returns_tag(self):
        """board_resolver.spec_source_resolution must return a source tag."""
        import importlib.util
        spec = importlib.util.spec_from_file_location("board_resolver", RESOLVER)
        br = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(br)

        path, tag = br.spec_source_resolution("chitin")
        self.assertIsInstance(tag, str)
        self.assertIn(tag, ("repo", "workspace_overlay", "owned_orgs", "default"),
                       f"unexpected source tag: {tag}")


class TestR3AssigneeGate(unittest.TestCase):
    """R3: kanban-flow ready rejects tickets with no assignee."""

    def _make_db(self, tmp: Path) -> Path:
        db_path = tmp / "kanban.db"
        conn = sqlite3.connect(db_path)
        conn.executescript("""
            CREATE TABLE tasks (
                id TEXT PRIMARY KEY,
                title TEXT NOT NULL,
                body TEXT,
                assignee TEXT,
                status TEXT NOT NULL,
                block_reason TEXT,
                priority INTEGER DEFAULT 0,
                created_by TEXT,
                created_at INTEGER NOT NULL,
                started_at INTEGER,
                completed_at INTEGER,
                workspace_path TEXT,
                claim_lock TEXT,
                claim_expires INTEGER,
                result TEXT,
                idempotency_key TEXT,
                max_runtime_seconds INTEGER,
                last_heartbeat_at INTEGER,
                current_run_id INTEGER,
                current_step_key TEXT,
                consecutive_failures INTEGER NOT NULL DEFAULT 0,
                last_failure_error TEXT
            );
            CREATE TABLE task_events (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                task_id TEXT NOT NULL,
                kind TEXT NOT NULL,
                payload TEXT,
                created_at INTEGER NOT NULL
            );
            CREATE TABLE task_comments (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                task_id TEXT NOT NULL,
                author TEXT NOT NULL,
                body TEXT NOT NULL,
                created_at INTEGER NOT NULL
            );
        """)
        conn.commit()
        conn.close()
        return db_path

    def _run_flow(self, tmp: Path, *args: str) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env["KANBAN_DB"] = str(tmp / "kanban.db")
        env["KANBAN_BOARD"] = "chitin"
        env["HOME"] = str(tmp)
        return subprocess.run(
            [str(KANBAN_FLOW), *args],
            capture_output=True,
            text=True,
            env=env,
            timeout=15,
        )

    def test_ready_rejects_null_assignee(self):
        """kanban-flow ready must reject a ticket with NULL assignee."""
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            db = self._make_db(tmp)
            conn = sqlite3.connect(db)
            conn.execute(
                "INSERT INTO tasks(id, title, assignee, status, created_at, idempotency_key, max_runtime_seconds, current_step_key) "
                "VALUES (?, ?, NULL, 'triage', ?, ?, ?, ?)",
                ("t_a1b2c3d4", "Null assignee test", 1, "idem-1", 1800, "dispatch"),
            )
            conn.commit()
            conn.close()

            result = self._run_flow(tmp, "ready", "t_a1b2c3d4")
            self.assertNotEqual(result.returncode, 0,
                               "kanban-flow ready should reject NULL assignee")
            combined = result.stderr + result.stdout
            self.assertIn("assignee", combined.lower(),
                           f"error message should mention assignee requirement, got: {combined}")

    def test_ready_accepts_valid_assignees(self):
        """kanban-flow ready must accept all valid assignees."""
        valid_assignees = [("codex", "t_a0b1c2d3"), ("copilot", "t_a1b2c3d4"), ("claude-code", "t_a2b3c4d5"), ("gemini", "t_a3b4c5d6"), ("clawta", "t_a4b5c6d7"), ("red", "t_a5b6c7d8")]
        for assignee, task_id in valid_assignees:
            with self.subTest(assignee=assignee):
                with tempfile.TemporaryDirectory() as tmp:
                    tmp = Path(tmp)
                    db = self._make_db(tmp)
                    conn = sqlite3.connect(db)
                    conn.execute(
                        "INSERT INTO tasks(id, title, assignee, status, created_at, idempotency_key, max_runtime_seconds, current_step_key) "
                        "VALUES (?, ?, ?, 'triage', ?, ?, ?, ?)",
                        (task_id, f"Test {assignee}", assignee, 1, "idem-1", 1800, "dispatch"),
                    )
                    conn.commit()
                    conn.close()

                    result = self._run_flow(tmp, "ready", task_id)
                    self.assertEqual(result.returncode, 0,
                                       f"kanban-flow ready should accept assignee={assignee}: {result.stderr}")


class TestR4RunbookExists(unittest.TestCase):
    """R4: Operator runbook exists with all five dispatch-readiness checks."""

    def test_runbook_file_exists(self):
        self.assertTrue(RUNBOOK.exists(), "dispatch-readiness.md must exist")

    def test_runbook_contains_all_five_checks(self):
        if not RUNBOOK.exists():
            self.skipTest("runbook file missing")
        text = RUNBOOK.read_text().lower()

        checks = [
            "invariants",           # 1. Invariants block present
            "spec-kit",             # 2. Spec-kit entry exists
            "assignee",             # 3. Assignee is set
            "blocked until",        # 4. No unresolved Blocked until
            "tracking epic",        # 5. Not a tracking epic
        ]
        for check in checks:
            self.assertIn(check, text,
                           f"runbook must mention check: {check}")

    def test_runbook_mentions_spec_source(self):
        if not RUNBOOK.exists():
            self.skipTest("runbook file missing")
        text = RUNBOOK.read_text()
        self.assertIn("spec_source", text,
                       "runbook must mention the spec_source config field")


class TestR5WatchdogTelemetry(unittest.TestCase):
    """R5: Watchdog output includes spec-root decision telemetry."""

    def test_watchdog_contains_spec_root_telemetry(self):
        """board-watchdog-bounded.py must emit 'spec root:' lines."""
        src = WATCHDOG.read_text()
        self.assertIn("spec root:", src,
                       "watchdog must emit spec-root telemetry")

    def test_watchdog_uses_spec_source_resolution(self):
        """Watchdog must call spec_source_resolution for the tag."""
        src = WATCHDOG.read_text()
        self.assertIn("_spec_source_resolution", src,
                       "watchdog must use _spec_source_resolution for telemetry")


if __name__ == "__main__":
    unittest.main()