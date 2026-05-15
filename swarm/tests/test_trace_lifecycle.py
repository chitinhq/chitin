from __future__ import annotations

import json
import os
import sqlite3
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
KANBAN_FLOW = REPO_ROOT / "scripts" / "kanban-flow"
TRACE_LIFECYCLE = REPO_ROOT / "scripts" / "trace-lifecycle.sh"


def make_db(root: Path) -> Path:
    db_path = root / "kanban.db"
    conn = sqlite3.connect(db_path)
    conn.executescript(
        """
        CREATE TABLE tasks (
          id TEXT PRIMARY KEY,
          title TEXT NOT NULL,
          body TEXT,
          assignee TEXT,
          status TEXT NOT NULL,
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
          current_step_key TEXT
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
        """
    )
    conn.execute(
        """
        INSERT INTO tasks(
          id, title, assignee, status, created_at, idempotency_key,
          max_runtime_seconds, current_step_key
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        """,
        ("t_deadbeef", "Trace smoke", "clawta", "triage", 1, "idem-1", 1800, "dispatch"),
    )
    conn.commit()
    conn.close()
    return db_path


class TraceLifecycleTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmpdir = tempfile.TemporaryDirectory()
        self.tmp = Path(self.tmpdir.name)
        self.db_path = make_db(self.tmp)
        self.chitin_home = self.tmp / ".chitin"
        self.chitin_home.mkdir()
        self.bin_dir = self.tmp / "bin"
        self.bin_dir.mkdir()
        hermes = self.bin_dir / "hermes"
        hermes.write_text("#!/bin/sh\nexit 0\n")
        hermes.chmod(0o755)
        self.env = os.environ.copy()
        self.env["KANBAN_DB"] = str(self.db_path)
        self.env["CHITIN_HOME"] = str(self.chitin_home)
        self.env["PATH"] = f"{self.bin_dir}:{self.env['PATH']}"

    def tearDown(self) -> None:
        self.tmpdir.cleanup()

    def run_cmd(self, script: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["bash", str(script), *args],
            cwd=REPO_ROOT,
            env=self.env,
            text=True,
            capture_output=True,
            check=check,
        )

    def write_chain(self, name: str, hashes: list[str], *, event_types: list[str] | None = None) -> Path:
        path = self.chitin_home / f"events-{name}.jsonl"
        types = event_types or ["session_start", "intended", "executed", "session_end"]
        rows = []
        for index, this_hash in enumerate(hashes):
            prev_hash = None if index == 0 else hashes[index - 1]
            rows.append(
                json.dumps(
                    {
                        "run_id": "kernel-run-smoke",
                        "event_type": types[index],
                        "ts": f"2026-05-14T10:0{index}:00Z",
                        "seq": index,
                        "prev_hash": prev_hash,
                        "this_hash": this_hash,
                    }
                )
            )
        path.write_text("\n".join(rows) + "\n")
        return path

    def test_trace_lifecycle_smoke_has_no_gaps(self) -> None:
        self.run_cmd(KANBAN_FLOW, "ready", "t_deadbeef", "--author", "operator")
        self.run_cmd(KANBAN_FLOW, "start", "t_deadbeef", "--author", "clawta")
        chain_file = self.write_chain("smoke-run", ["hash-1", "hash-2", "hash-3", "hash-final"])
        self.run_cmd(
            KANBAN_FLOW,
            "pr",
            "t_deadbeef",
            "https://github.com/chitinhq/chitin/pull/123",
            "--author",
            "clawta",
            "--event-chain-hash",
            "hash-final",
        )
        self.run_cmd(
            KANBAN_FLOW,
            "done",
            "t_deadbeef",
            "--result",
            "Merged PR #123 after clawta gate",
            "--author",
            "clawta",
            "--chain-file",
            str(chain_file),
        )

        trace = self.run_cmd(TRACE_LIFECYCLE, "t_deadbeef")
        stdout = trace.stdout

        self.assertIn("ticket: t_deadbeef  status=done", stdout)
        self.assertIn("KANBAN", stdout)
        self.assertIn("triage -> ready by operator", stdout)
        self.assertIn("ready -> in_progress by clawta", stdout)
        self.assertIn("PR opened by clawta", stdout)
        self.assertIn("RUN    ", stdout)
        self.assertIn("CHAIN  2026-05-14T10:02:00Z", stdout)
        self.assertIn("executed seq=2 hash=hash-3", stdout)
        self.assertIn("summary: warnings=0 gaps=0", stdout)

    def test_trace_lifecycle_warns_when_chain_file_is_missing(self) -> None:
        self.run_cmd(KANBAN_FLOW, "ready", "t_deadbeef", "--author", "operator")
        self.run_cmd(KANBAN_FLOW, "start", "t_deadbeef", "--author", "clawta")
        self.run_cmd(
            KANBAN_FLOW,
            "done",
            "t_deadbeef",
            "--result",
            "Merged without local chain copy",
            "--author",
            "clawta",
            "--event-chain-hash",
            "missing-terminal-hash",
        )

        trace = self.run_cmd(TRACE_LIFECYCLE, "t_deadbeef")
        self.assertIn("WARNING: run 1: no chain file matched terminal hash", trace.stdout)
        self.assertIn("summary: warnings=1 gaps=0", trace.stdout)

    def test_trace_lifecycle_errors_for_missing_ticket(self) -> None:
        result = self.run_cmd(TRACE_LIFECYCLE, "t_feedface", check=False)
        self.assertEqual(result.returncode, 2)
        self.assertIn("ticket not found: t_feedface", result.stderr)


if __name__ == "__main__":
    unittest.main()
