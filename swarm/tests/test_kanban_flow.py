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

    def test_stamp_run_identity_updates_model_and_soul_fingerprint(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_a11ce111")

            self.run_flow(tmp, "start", "t_a11ce111", "--author", "tester")
            self.run_flow(
                tmp,
                "stamp-run-identity",
                "t_a11ce111",
                extra_env={
                    "CHITIN_DISPATCH_MODEL": "gpt-5.5",
                    "CHITIN_DISPATCH_SOUL_ID": "knuth",
                    "CHITIN_DISPATCH_SOUL_HASH": "b" * 64,
                    "CHITIN_DISPATCH_AGENT_FINGERPRINT": "c" * 64,
                },
            )

            conn = sqlite3.connect(db_path)
            conn.row_factory = sqlite3.Row
            run = conn.execute(
                "SELECT model, soul_id, soul_hash, agent_fingerprint FROM task_runs WHERE task_id='t_a11ce111'"
            ).fetchone()
            conn.close()

        self.assertEqual(run["model"], "gpt-5.5")
        self.assertEqual(run["soul_id"], "knuth")
        self.assertEqual(run["soul_hash"], "b" * 64)
        self.assertEqual(run["agent_fingerprint"], "c" * 64)

    def test_start_allows_redispatch_after_ended_blocked_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_b10c0de0")

            self.run_flow(tmp, "start", "t_b10c0de0", "--author", "tester")
            self.run_flow(
                tmp,
                "crash",
                "t_b10c0de0",
                "operator intervention required",
                "--author",
                "tester",
                "--run-status",
                "blocked",
                "--outcome",
                "blocked",
            )

            conn = sqlite3.connect(db_path)
            conn.execute("UPDATE tasks SET status='ready' WHERE id='t_b10c0de0'")
            conn.commit()
            conn.close()

            self.run_flow(tmp, "start", "t_b10c0de0", "--author", "tester")

            conn = sqlite3.connect(db_path)
            conn.row_factory = sqlite3.Row
            runs = conn.execute(
                """
                SELECT status, ended_at
                  FROM task_runs
                 WHERE task_id='t_b10c0de0'
                 ORDER BY id
                """
            ).fetchall()
            conn.close()

        self.assertEqual([run["status"] for run in runs], ["blocked", "running"])
        self.assertIsNotNone(runs[0]["ended_at"])
        self.assertIsNone(runs[1]["ended_at"])

    def test_block_finalizes_active_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_b10c0ed0")

            self.run_flow(tmp, "start", "t_b10c0ed0", "--author", "tester")
            self.run_flow(tmp, "block", "t_b10c0ed0", "operator hold", "--author", "tester")

            conn = sqlite3.connect(db_path)
            conn.row_factory = sqlite3.Row
            task = conn.execute(
                "SELECT status, current_run_id FROM tasks WHERE id='t_b10c0ed0'"
            ).fetchone()
            run = conn.execute(
                "SELECT status, outcome, ended_at, error FROM task_runs WHERE task_id='t_b10c0ed0'"
            ).fetchone()
            conn.close()

        self.assertEqual(task["status"], "blocked")
        self.assertIsNone(task["current_run_id"])
        self.assertEqual(run["status"], "blocked")
        self.assertEqual(run["outcome"], "blocked")
        self.assertEqual(run["error"], "operator hold")
        self.assertIsNotNone(run["ended_at"])

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

    def test_done_reads_final_event_chain_hash_from_chain_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_decafbad")
            chain_file = tmp / "session-chain.jsonl"
            chain_file.write_text(
                "\n".join(
                    [
                        json.dumps({"this_hash": "hash-1", "event": "first"}),
                        json.dumps({"this_hash": "hash-2", "event": "second"}),
                    ]
                )
                + "\n"
            )

            self.run_flow(tmp, "start", "t_decafbad", "--author", "tester")
            self.run_flow(
                tmp,
                "done",
                "t_decafbad",
                "--result",
                "Merged PR #2",
                "--author",
                "tester",
                "--chain-file",
                str(chain_file),
            )

            conn = sqlite3.connect(db_path)
            event_chain_hash = conn.execute(
                "SELECT event_chain_hash FROM task_runs WHERE task_id='t_decafbad'"
            ).fetchone()[0]
            conn.close()

        self.assertEqual(event_chain_hash, "hash-2")

    def test_done_leaves_event_chain_hash_null_for_empty_chain_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_facefeed")
            chain_file = tmp / "empty-chain.jsonl"
            chain_file.write_text("")

            self.run_flow(tmp, "start", "t_facefeed", "--author", "tester")
            self.run_flow(
                tmp,
                "done",
                "t_facefeed",
                "--result",
                "Merged PR #3",
                "--author",
                "tester",
                "--chain-file",
                str(chain_file),
            )

            conn = sqlite3.connect(db_path)
            event_chain_hash = conn.execute(
                "SELECT event_chain_hash FROM task_runs WHERE task_id='t_facefeed'"
            ).fetchone()[0]
            conn.close()

        self.assertIsNone(event_chain_hash)

    def test_start_records_audited_mutations_for_tasks_comments_and_events(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_a11d1001")

            self.run_flow(tmp, "start", "t_a11d1001", "--author", "tester")

            conn = sqlite3.connect(db_path)
            conn.row_factory = sqlite3.Row
            rows = conn.execute(
                """
                SELECT table_name, op, task_id, application_id, pid
                  FROM kanban_mutations_log
                 WHERE task_id = 't_a11d1001'
                   AND table_name IN ('tasks', 'task_comments', 'task_events')
                """
            ).fetchall()
            conn.close()

        by_table = {row["table_name"] for row in rows}
        self.assertEqual(by_table, {"tasks", "task_comments", "task_events"})
        for row in rows:
            self.assertEqual(row["op"], "INSERT" if row["table_name"] != "tasks" else "UPDATE")
            self.assertEqual(row["application_id"], 1262894167)
            self.assertGreater(row["pid"], 0)

    def test_metadata_verbs_route_through_kanban_flow(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            db_path = make_db(tmp)
            insert_task(db_path, "t_0abc0001")

            self.run_flow(tmp, "comment", "t_0abc0001", "--author", "tester", "hello from flow")
            self.run_flow(tmp, "assign", "t_0abc0001", "red")
            self.run_flow(tmp, "set-block-reason", "t_0abc0001", "needs_spec")
            self.run_flow(
                tmp,
                "record-failure",
                "t_0abc0001",
                "silent_worker_death",
                "worker disappeared",
            )

            conn = sqlite3.connect(db_path)
            conn.row_factory = sqlite3.Row
            task = conn.execute(
                """
                SELECT assignee, block_reason, consecutive_failures, last_failure_error
                  FROM tasks
                 WHERE id = 't_0abc0001'
                """
            ).fetchone()
            comment = conn.execute(
                "SELECT body FROM task_comments WHERE task_id = 't_0abc0001' ORDER BY id DESC LIMIT 1"
            ).fetchone()[0]
            conn.close()

        self.assertEqual(task["assignee"], "red")
        self.assertEqual(task["block_reason"], "needs_spec")
        self.assertEqual(task["consecutive_failures"], 1)
        self.assertEqual(
            task["last_failure_error"],
            "[silent_worker_death] worker disappeared",
        )
        self.assertEqual(comment, "hello from flow")


if __name__ == "__main__":
    unittest.main()
