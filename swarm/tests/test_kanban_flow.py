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
    conn.commit()
    conn.close()
    return db_path


def insert_task(
    db_path: Path,
    task_id: str,
    *,
    status: str = "ready",
    workspace_path: str | None = None,
    idempotency_key: str | None = "idem-1",
) -> None:
    conn = sqlite3.connect(db_path)
    conn.execute(
        """
        INSERT INTO tasks(
          id, title, assignee, status, created_at, workspace_path,
          idempotency_key, max_runtime_seconds, current_step_key
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
        (
            task_id,
            f"Task {task_id}",
            "codex",
            status,
            1,
            workspace_path,
            idempotency_key,
            1800,
            "dispatch",
        ),
    )
    conn.commit()
    conn.close()


class KanbanFlowTaskRunTests(unittest.TestCase):
    def run_flow(self, tmp: Path, *args: str, extra_env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
        bin_dir = tmp / "bin"
        bin_dir.mkdir(exist_ok=True)
        hermes = bin_dir / "hermes"
        hermes.write_text("#!/bin/sh\nexit 0\n")
        hermes.chmod(0o755)

        env = os.environ.copy()
        env["KANBAN_DB"] = str(tmp / "kanban.db")
        env["PATH"] = f"{bin_dir}:{env['PATH']}"
        env["CHITIN_HOME"] = str(tmp / ".chitin")
        if extra_env:
            env.update(extra_env)

        return subprocess.run(
            ["bash", str(KANBAN_FLOW), *args],
            cwd=REPO_ROOT,
            text=True,
            capture_output=True,
            env=env,
            check=True,
        )

    def test_start_populates_task_run_from_driver_identity_config(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_deadbeef", workspace_path=str(tmp / "missing-worktree"))
            chitin_home = tmp / ".chitin"
            chitin_home.mkdir()
            (chitin_home / "driver_identity.json").write_text(
                json.dumps(
                    {
                        "driver_identity": {
                            "user": "kernel-user",
                            "machine_id": "kernel-box",
                            "machine_fingerprint": "f" * 64,
                        }
                    }
                )
            )

            self.run_flow(tmp, "start", "t_deadbeef", "--author", "tester")

            conn = sqlite3.connect(db_path)
            conn.row_factory = sqlite3.Row
            task = conn.execute("SELECT status, current_run_id FROM tasks WHERE id='t_deadbeef'").fetchone()
            run = conn.execute(
                """
                SELECT status, driver_id, repo_sha, lease_id, idempotency_key
                  FROM task_runs
                 WHERE task_id='t_deadbeef'
                """
            ).fetchone()
            conn.close()

        self.assertEqual(task["status"], "in_progress")
        self.assertIsNotNone(task["current_run_id"])
        self.assertEqual(run["status"], "running")
        self.assertEqual(
            run["driver_id"],
            '{"machine_fingerprint":"' + ("f" * 64) + '","machine_id":"kernel-box","user":"kernel-user"}',
        )
        self.assertIsNone(run["repo_sha"])
        self.assertEqual(run["idempotency_key"], "idem-1")
        self.assertRegex(run["lease_id"], r"^[a-f0-9-]{36}$")

    def test_start_falls_back_to_env_driver_identity_when_config_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_feedface")

            self.run_flow(
                tmp,
                "start",
                "t_feedface",
                "--author",
                "tester",
                extra_env={
                    "CHITIN_DRIVER_USER": "env-user",
                    "CHITIN_DRIVER_MACHINE_ID": "env-box",
                    "CHITIN_DRIVER_MACHINE_FINGERPRINT": "a" * 64,
                },
            )

            conn = sqlite3.connect(db_path)
            driver_id = conn.execute(
                "SELECT driver_id FROM task_runs WHERE task_id='t_feedface'"
            ).fetchone()[0]
            conn.close()

        self.assertEqual(
            driver_id,
            '{"machine_fingerprint":"' + ("a" * 64) + '","machine_id":"env-box","user":"env-user"}',
        )

    def test_pr_hash_is_preserved_until_done_and_crash_finalizes_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_cafebabe")

            self.run_flow(tmp, "start", "t_cafebabe", "--author", "tester")
            self.run_flow(
                tmp,
                "pr",
                "t_cafebabe",
                "https://github.com/chitinhq/chitin/pull/1",
                "--author",
                "tester",
                "--event-chain-hash",
                "hash-from-worker",
            )
            self.run_flow(
                tmp,
                "done",
                "t_cafebabe",
                "--result",
                "Merged PR #1",
                "--author",
                "tester",
            )

            conn = sqlite3.connect(db_path)
            conn.row_factory = sqlite3.Row
            done_run = conn.execute(
                """
                SELECT status, outcome, event_chain_hash
                  FROM task_runs
                 WHERE task_id='t_cafebabe'
                """
            ).fetchone()
            conn.close()

        self.assertEqual(done_run["status"], "done")
        self.assertEqual(done_run["outcome"], "completed")
        self.assertEqual(done_run["event_chain_hash"], "hash-from-worker")

        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_badc0de0")

            self.run_flow(tmp, "start", "t_badc0de0", "--author", "tester")
            self.run_flow(
                tmp,
                "crash",
                "t_badc0de0",
                "worker timeout",
                "--author",
                "tester",
                "--run-status",
                "timed_out",
                "--outcome",
                "timed_out",
                "--event-chain-hash",
                "timeout-hash",
            )

            conn = sqlite3.connect(db_path)
            conn.row_factory = sqlite3.Row
            task = conn.execute(
                "SELECT status, current_run_id FROM tasks WHERE id='t_badc0de0'"
            ).fetchone()
            crash_run = conn.execute(
                """
                SELECT status, outcome, event_chain_hash, ended_at
                  FROM task_runs
                 WHERE task_id='t_badc0de0'
                """
            ).fetchone()
            conn.close()

        self.assertEqual(task["status"], "blocked")
        self.assertIsNone(task["current_run_id"])
        self.assertEqual(crash_run["status"], "timed_out")
        self.assertEqual(crash_run["outcome"], "timed_out")
        self.assertEqual(crash_run["event_chain_hash"], "timeout-hash")
        self.assertIsNotNone(crash_run["ended_at"])


if __name__ == "__main__":
    unittest.main()
