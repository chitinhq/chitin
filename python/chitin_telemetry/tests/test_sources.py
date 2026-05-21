"""Tests for Argus kanban/git source pollers."""
from __future__ import annotations

import json
import sqlite3
import tempfile
from pathlib import Path

from chitin_telemetry import migrations
from chitin_telemetry.indexer import init_db
from chitin_telemetry.sources import _open_kanban_readonly, _respect_gh_rate_limit, ingest_git_repo, ingest_kanban_sources


def _argus_conn(tmpdir: str) -> sqlite3.Connection:
    db_path = Path(tmpdir) / "chitin_telemetry.db"
    conn = init_db(db_path)
    migrations.apply_pending(conn)
    return conn


def test_open_kanban_readonly_retries_locked_db(monkeypatch):
    attempts = {"count": 0}

    class DummyConn:
        row_factory = None

        def execute(self, *_args, **_kwargs):
            return None

    def fake_connect(*_args, **_kwargs):
        attempts["count"] += 1
        if attempts["count"] < 3:
            raise sqlite3.OperationalError("database is locked")
        return DummyConn()

    monkeypatch.setattr(sqlite3, "connect", fake_connect)
    conn = _open_kanban_readonly(Path("/tmp/fake.db"), retry_attempts=3, sleep_s=0)
    assert isinstance(conn, DummyConn)
    assert attempts["count"] == 3


def test_ingest_kanban_sources_indexes_tasks_events_and_comments():
    with tempfile.TemporaryDirectory() as tmpdir:
        boards_root = Path(tmpdir) / "boards"
        board_dir = boards_root / "chitin"
        board_dir.mkdir(parents=True)
        kanban_db = board_dir / "kanban.db"
        src = sqlite3.connect(kanban_db)
        src.execute("CREATE TABLE tasks (id TEXT PRIMARY KEY, title TEXT, body TEXT, status TEXT, assignee TEXT, priority INTEGER, created_at INTEGER)")
        src.execute("CREATE TABLE task_events (id INTEGER PRIMARY KEY, task_id TEXT, kind TEXT, payload TEXT, created_at INTEGER)")
        src.execute("CREATE TABLE task_comments (id INTEGER PRIMARY KEY, task_id TEXT, author TEXT, body TEXT, created_at INTEGER)")
        src.execute("INSERT INTO tasks VALUES ('t_12345678', 'Fix normalizeGenericLeak', 'Touch `internal/driver`', 'ready', 'codex', 2, 100)")
        src.execute("INSERT INTO task_events VALUES (1, 't_12345678', 'status_transition', ?, 110)", (json.dumps({"from": "ready", "to": "triage", "by": "clawta"}),))
        src.execute("INSERT INTO task_comments VALUES (1, 't_12345678', 'red', 'Correction: `normalizeGenericLeak` lives in internal/driver.', 120)")
        src.commit()
        src.close()

        conn = _argus_conn(tmpdir)
        stats = ingest_kanban_sources(conn, boards_root)
        assert stats == {"tickets": 1, "transitions": 1, "comments": 1}
        rows = conn.execute("SELECT kind, ticket_id FROM events WHERE source = 'kanban' ORDER BY ts_unix").fetchall()
        assert [(r["kind"], r["ticket_id"]) for r in rows] == [
            ("kanban_ticket_create", "t_12345678"),
            ("kanban_status_transition", "t_12345678"),
            ("kanban_comment", "t_12345678"),
        ]
        conn.close()


def test_respect_gh_rate_limit_pauses_until_reset():
    slept = {"seconds": 0}

    def fake_runner(args, **_kwargs):
        class Result:
            returncode = 0
            stdout = "HTTP/1.1 200 OK\nx-ratelimit-remaining: 0\nx-ratelimit-reset: 101\n\n{}"
            stderr = ""
        return Result()

    _respect_gh_rate_limit(
        Path("."),
        runner=fake_runner,
        sleep_fn=lambda seconds: slept.__setitem__("seconds", seconds),
        now_fn=lambda: 100.0,
    )
    assert slept["seconds"] >= 1.0


def test_ingest_git_repo_handles_empty_repo():
    with tempfile.TemporaryDirectory() as tmpdir:
        conn = _argus_conn(tmpdir)

        def fake_runner(args, **_kwargs):
            class Result:
                def __init__(self, returncode, stdout="", stderr=""):
                    self.returncode = returncode
                    self.stdout = stdout
                    self.stderr = stderr
            if args[:4] == ["git", "-C", tmpdir, "log"]:
                return Result(128, "", "fatal: your current branch does not have any commits yet")
            return Result(1, "[]", "gh unavailable")

        stats = ingest_git_repo(conn, Path(tmpdir), runner=fake_runner, sleep_fn=lambda *_: None, now_fn=lambda: 100.0)
        assert stats == {"commits": 0, "prs": 0, "merged_prs": 0, "reviews": 0}
        assert conn.execute("SELECT COUNT(*) FROM events WHERE source = 'git'").fetchone()[0] == 0
        conn.close()


def test_ingest_git_repo_pr_without_reviews_indexes_pr_not_reviews():
    with tempfile.TemporaryDirectory() as tmpdir:
        conn = _argus_conn(tmpdir)
        repo = Path(tmpdir)

        def fake_runner(args, **_kwargs):
            class Result:
                def __init__(self, returncode, stdout="", stderr=""):
                    self.returncode = returncode
                    self.stdout = stdout
                    self.stderr = stderr

            if args[:4] == ["git", "-C", str(repo), "log"]:
                return Result(0, "")
            if args[:3] == ["gh", "api", "-i"]:
                return Result(0, "HTTP/1.1 200 OK\nx-ratelimit-remaining: 50\nx-ratelimit-reset: 999\n\n{}")
            if args[:4] == ["gh", "pr", "list", "--repo"]:
                return Result(0, json.dumps([{
                    "number": 14,
                    "title": "Fix t_12345678",
                    "body": "Closes t_12345678",
                    "createdAt": "2026-05-13T08:00:00Z",
                    "updatedAt": "2026-05-14T09:00:00Z",
                    "mergedAt": None,
                    "state": "OPEN",
                    "isDraft": False,
                    "url": "https://example.invalid/pr/14",
                    "headRefName": "swarm/codex-fix-ticket",
                }]))
            if args[:4] == ["gh", "pr", "view", "14"]:
                return Result(0, json.dumps({
                    "files": [{"path": "python/chitin_telemetry/detectors.py"}],
                    "reviews": [],
                    "statusCheckRollup": [{"conclusion": "SUCCESS"}],
                    "mergeStateStatus": "CLEAN",
                    "reviewDecision": "",
                    "commits": [],
                }))
            raise AssertionError(f"unexpected command: {args}")

        stats = ingest_git_repo(conn, repo, runner=fake_runner, sleep_fn=lambda *_: None, now_fn=lambda: 200.0)
        assert stats["prs"] == 1
        assert stats["reviews"] == 0
        kinds = {r["kind"] for r in conn.execute("SELECT kind FROM events WHERE source = 'git'").fetchall()}
        assert "git_pr_opened" in kinds
        assert "git_review_submitted" not in kinds
        conn.close()
