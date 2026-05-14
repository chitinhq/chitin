"""Tests for clawta-stale-worker-watchdog auto-retry and escalation logic."""
from __future__ import annotations

import importlib.util
import json
import sqlite3
import sys
import time
from importlib.machinery import SourceFileLoader
from pathlib import Path
from unittest.mock import patch

import pytest

WATCHDOG_SCRIPT = Path(__file__).resolve().parents[1] / "bin" / "clawta-stale-worker-watchdog"


def _load_module():
    """Load the hyphenated watchdog script via importlib (can't use normal import)."""
    MODULE_NAME = "clawta_stale_worker_watchdog_test"
    spec = importlib.util.spec_from_loader(
        MODULE_NAME,
        SourceFileLoader(MODULE_NAME, str(WATCHDOG_SCRIPT)),
    )
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    # Fix dataclass resolution: without __module__ set, dataclass field
    # lookups fail with AttributeError on cls.__module__.__dict__.
    module.__module__ = MODULE_NAME
    sys.modules[MODULE_NAME] = module
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


def _import_watchdog(tmp_path: Path):
    """Load the watchdog module and override paths for testing."""
    wd = _load_module()

    # Override paths to the test fixture
    wd.KANBAN_DB = tmp_path / "kanban.db"
    wd.KANBAN_FLOW_BIN = tmp_path / "kanban-flow"
    wd.HERMES_BIN = tmp_path / "hermes"
    wd.OPENCLAW_BIN = tmp_path / "openclaw"
    wd.OPENCLAW_LOG_DIR = tmp_path / "logs"
    wd.MAX_RETRIES = 3
    wd.STALE_AFTER = 2700
    wd.QUIET_AFTER = 1200

    # Create fake binaries
    for name in ("kanban-flow", "hermes", "openclaw"):
        bin_path = tmp_path / name
        bin_path.write_text("#!/bin/sh\nexit 0\n")
        bin_path.chmod(0o755)

    wd.OPENCLAW_LOG_DIR.mkdir(parents=True, exist_ok=True)

    return wd


def _create_test_db(db_path: Path) -> None:
    """Create a minimal kanban DB with the tasks table."""
    conn = sqlite3.connect(db_path)
    conn.executescript("""
        CREATE TABLE IF NOT EXISTS tasks (
            id TEXT PRIMARY KEY,
            title TEXT NOT NULL,
            body TEXT,
            assignee TEXT,
            status TEXT NOT NULL DEFAULT 'ready',
            priority INTEGER DEFAULT 0,
            created_by TEXT,
            created_at INTEGER NOT NULL,
            started_at INTEGER,
            completed_at INTEGER,
            consecutive_failures INTEGER NOT NULL DEFAULT 0,
            last_failure_error TEXT,
            spawn_failures INTEGER NOT NULL DEFAULT 0
        );
        CREATE TABLE IF NOT EXISTS task_events (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            task_id TEXT NOT NULL,
            run_id INTEGER,
            kind TEXT NOT NULL,
            payload TEXT,
            created_at INTEGER NOT NULL
        );
        CREATE TABLE IF NOT EXISTS task_runs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            task_id TEXT NOT NULL,
            profile TEXT,
            step_key TEXT,
            status TEXT NOT NULL,
            claim_lock TEXT,
            claim_expires INTEGER,
            worker_pid INTEGER,
            max_runtime_seconds INTEGER,
            last_heartbeat_at INTEGER,
            started_at INTEGER NOT NULL,
            ended_at INTEGER,
            outcome TEXT,
            summary TEXT,
            metadata TEXT,
            error TEXT
        );
        CREATE TABLE IF NOT EXISTS task_comments (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            task_id TEXT NOT NULL,
            author TEXT NOT NULL,
            body TEXT NOT NULL,
            created_at INTEGER NOT NULL
        );
    """)
    conn.commit()
    conn.close()


def _insert_task(db_path: Path, task_id: str, *, status: str = "in_progress",
                 assignee: str = "codex", age_seconds: int = 3200,
                 consecutive_failures: int = 0) -> None:
    """Insert a test task into the DB."""
    now = int(time.time())
    conn = sqlite3.connect(db_path)
    conn.execute(
        """INSERT OR REPLACE INTO tasks
           (id, title, assignee, status, priority, created_at, started_at, consecutive_failures)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?)""",
        (task_id, f"Test task {task_id}", assignee, status, 50,
         now - age_seconds, now - age_seconds, consecutive_failures),
    )
    conn.commit()
    conn.close()


def _insert_run(db_path: Path, task_id: str, *, outcome: str | None = None,
                error: str | None = None, status: str = "crashed") -> None:
    """Insert a test task_run into the DB."""
    now = int(time.time())
    conn = sqlite3.connect(db_path)
    conn.execute(
        """INSERT INTO task_runs
           (task_id, status, started_at, outcome, error)
           VALUES (?, ?, ?, ?, ?)""",
        (task_id, status, now - 3600, outcome, error),
    )
    conn.commit()
    conn.close()


class TestClassifyFailure:
    """Test failure classification logic."""

    def test_silent_death_no_runs(self, tmp_path):
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_abc123")
        result = wd.classify_failure("t_abc123", tmp_path / "kanban.db")
        assert result == wd.FailureClass.SILENT_DEATH

    def test_explicit_failure_crashed(self, tmp_path):
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_abc123")
        _insert_run(tmp_path / "kanban.db", "t_abc123", outcome="crashed")
        result = wd.classify_failure("t_abc123", tmp_path / "kanban.db")
        assert result == wd.FailureClass.EXPLICIT_FAILURE

    def test_explicit_failure_with_error(self, tmp_path):
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_abc123")
        _insert_run(tmp_path / "kanban.db", "t_abc123", outcome="done", error="exit code 1")
        result = wd.classify_failure("t_abc123", tmp_path / "kanban.db")
        assert result == wd.FailureClass.EXPLICIT_FAILURE

    def test_timeout_is_silent_death(self, tmp_path):
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_abc123")
        _insert_run(tmp_path / "kanban.db", "t_abc123", outcome="timed_out")
        result = wd.classify_failure("t_abc123", tmp_path / "kanban.db")
        assert result == wd.FailureClass.SILENT_DEATH


class TestConsecutiveFailures:
    """Test consecutive failure counter operations."""

    def test_get_consecutive_failures_default(self, tmp_path):
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_abc123")
        result = wd.get_consecutive_failures(tmp_path / "kanban.db", "t_abc123")
        assert result == 0

    def test_get_consecutive_failures_with_count(self, tmp_path):
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_abc123", consecutive_failures=2)
        result = wd.get_consecutive_failures(tmp_path / "kanban.db", "t_abc123")
        assert result == 2

    def test_increment_consecutive_failures(self, tmp_path):
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_abc123", consecutive_failures=1)
        new_count = wd.increment_consecutive_failures(
            tmp_path / "kanban.db", "t_abc123",
            wd.FailureClass.SILENT_DEATH, "test reason"
        )
        assert new_count == 2
        # Verify it persisted
        result = wd.get_consecutive_failures(tmp_path / "kanban.db", "t_abc123")
        assert result == 2


class TestFindStaleCandidates:
    """Test the main stale-detection + retry/escalate decision logic."""

    def test_first_silent_death_auto_retries(self, tmp_path):
        """First silent death: auto-retry (consecutive_failures goes 0 → 1)."""
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_stale01")
        with patch.object(wd, "has_active_worker", return_value=False), \
             patch.object(wd, "has_pr", return_value=False), \
             patch.object(wd, "quiet_seconds", return_value=1500):
            checked, retried, escalated = wd.find_stale_candidates(max_retries=3)
        assert len(checked) == 1
        assert len(retried) == 1
        assert len(escalated) == 0
        assert retried[0].consecutive_failures == 1
        assert retried[0].failure_class == wd.FailureClass.SILENT_DEATH

    def test_third_silent_death_escalates(self, tmp_path):
        """After 3 silent deaths (consecutive_failures becomes 3), escalate."""
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_stale01", consecutive_failures=2)
        with patch.object(wd, "has_active_worker", return_value=False), \
             patch.object(wd, "has_pr", return_value=False), \
             patch.object(wd, "quiet_seconds", return_value=1500):
            checked, retried, escalated = wd.find_stale_candidates(max_retries=3)
        assert len(retried) == 0
        assert len(escalated) == 1
        assert escalated[0].consecutive_failures == 3
        assert escalated[0].failure_class == wd.FailureClass.SILENT_DEATH

    def test_explicit_failure_escalates_immediately(self, tmp_path):
        """Explicit failure always escalates regardless of retry count."""
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_stale01", consecutive_failures=0)
        _insert_run(tmp_path / "kanban.db", "t_stale01", outcome="crashed")
        with patch.object(wd, "has_active_worker", return_value=False), \
             patch.object(wd, "has_pr", return_value=False), \
             patch.object(wd, "quiet_seconds", return_value=1500):
            checked, retried, escalated = wd.find_stale_candidates(max_retries=3)
        assert len(retried) == 0
        assert len(escalated) == 1
        assert escalated[0].failure_class == wd.FailureClass.EXPLICIT_FAILURE

    def test_second_silent_death_auto_retries(self, tmp_path):
        """Second consecutive silent death (1 → 2): still auto-retry."""
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_stale01", consecutive_failures=1)
        with patch.object(wd, "has_active_worker", return_value=False), \
             patch.object(wd, "has_pr", return_value=False), \
             patch.object(wd, "quiet_seconds", return_value=1500):
            checked, retried, escalated = wd.find_stale_candidates(max_retries=3)
        assert len(retried) == 1
        assert retried[0].consecutive_failures == 2

    def test_active_worker_skips(self, tmp_path):
        """Tickets with active workers are not stale."""
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_active01", age_seconds=5000)
        with patch.object(wd, "has_active_worker", return_value=True), \
             patch.object(wd, "has_pr", return_value=False), \
             patch.object(wd, "quiet_seconds", return_value=5000):
            checked, retried, escalated = wd.find_stale_candidates(max_retries=3)
        assert len(checked) == 1
        assert len(retried) == 0
        assert len(escalated) == 0

    def test_has_pr_skips(self, tmp_path):
        """Tickets with an open PR are not stale."""
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_pr01", age_seconds=5000)
        # Insert PR event
        conn = sqlite3.connect(tmp_path / "kanban.db")
        conn.execute(
            "INSERT INTO task_events (task_id, kind, payload, created_at) VALUES (?, ?, ?, ?)",
            ("t_pr01", "pr_opened", '{"url": "https://github.com/example/pull/1"}', int(time.time())),
        )
        conn.commit()
        conn.close()
        with patch.object(wd, "has_active_worker", return_value=False), \
             patch.object(wd, "quiet_seconds", return_value=5000):
            checked, retried, escalated = wd.find_stale_candidates(max_retries=3)
        assert len(checked) == 1
        assert len(retried) == 0
        assert len(escalated) == 0


class TestDryRun:
    """Test dry-run mode — counts state transitions but doesn't execute them."""

    def test_dry_run_reports_without_side_effects(self, tmp_path):
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        _insert_task(tmp_path / "kanban.db", "t_dry01", consecutive_failures=0)

        # Invariant: --dry-run is fully side-effect free. find_stale_candidates
        # only *predicts* the post-increment count; it must not touch the DB,
        # and main() must not call auto_retry_ticket / block_ticket_with_history.
        with patch.object(wd, "has_active_worker", return_value=False), \
             patch.object(wd, "has_pr", return_value=False), \
             patch.object(wd, "quiet_seconds", return_value=1500), \
             patch.object(wd, "auto_retry_ticket") as mock_retry, \
             patch.object(wd, "block_ticket_with_history") as mock_block:
            import io
            old_stdout = sys.stdout
            sys.stdout = io.StringIO()
            old_argv = sys.argv
            sys.argv = ["watchdog", "--dry-run", "--json"]
            try:
                wd.main()
                output = sys.stdout.getvalue()
            finally:
                sys.stdout = old_stdout
                sys.argv = old_argv

        data = json.loads(output)
        assert data["dry_run"] is True
        assert data["retried"] == 1
        # No side-effect calls were made...
        mock_retry.assert_not_called()
        mock_block.assert_not_called()
        # ...and the persisted counter is untouched (the report's "1" is a
        # prediction of what the count *would* become, not a write).
        assert wd.get_consecutive_failures(tmp_path / "kanban.db", "t_dry01") == 0

    def test_legacy_schema_without_counter_escalates(self, tmp_path):
        """Boundary: a board predating the consecutive_failures column.

        The retry counter can never advance there, so a silent death must
        escalate immediately instead of auto-retrying forever.
        """
        wd = _import_watchdog(tmp_path)
        db = tmp_path / "kanban.db"
        conn = sqlite3.connect(db)
        conn.executescript("""
            CREATE TABLE tasks (
                id TEXT PRIMARY KEY, title TEXT NOT NULL, body TEXT,
                assignee TEXT, status TEXT NOT NULL DEFAULT 'ready',
                priority INTEGER DEFAULT 0, created_by TEXT,
                created_at INTEGER NOT NULL, started_at INTEGER,
                completed_at INTEGER, spawn_failures INTEGER NOT NULL DEFAULT 0
            );
            CREATE TABLE task_events (
                id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL,
                run_id INTEGER, kind TEXT NOT NULL, payload TEXT,
                created_at INTEGER NOT NULL
            );
            CREATE TABLE task_runs (
                id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL,
                status TEXT NOT NULL, started_at INTEGER NOT NULL,
                ended_at INTEGER, outcome TEXT, error TEXT
            );
            CREATE TABLE task_comments (
                id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL,
                author TEXT NOT NULL, body TEXT NOT NULL, created_at INTEGER NOT NULL
            );
        """)
        now = int(time.time())
        conn.execute(
            "INSERT INTO tasks (id, title, assignee, status, priority, created_at, started_at) "
            "VALUES (?, ?, ?, ?, ?, ?, ?)",
            ("t_legacy01", "legacy", "codex", "in_progress", 50, now - 3200, now - 3200),
        )
        conn.commit()
        conn.close()
        assert wd.schema_supports_retry_tracking(db) is False
        with patch.object(wd, "has_active_worker", return_value=False), \
             patch.object(wd, "has_pr", return_value=False), \
             patch.object(wd, "quiet_seconds", return_value=1500):
            checked, retried, escalated = wd.find_stale_candidates(max_retries=3)
        assert len(retried) == 0
        assert len(escalated) == 1

    def test_empty_board_yields_no_candidates(self, tmp_path):
        """Boundary: empty board (no in_progress tickets) → nothing to do."""
        wd = _import_watchdog(tmp_path)
        _create_test_db(tmp_path / "kanban.db")
        with patch.object(wd, "has_active_worker", return_value=False), \
             patch.object(wd, "has_pr", return_value=False), \
             patch.object(wd, "quiet_seconds", return_value=1500):
            checked, retried, escalated = wd.find_stale_candidates(max_retries=3)
        assert checked == []
        assert retried == []
        assert escalated == []