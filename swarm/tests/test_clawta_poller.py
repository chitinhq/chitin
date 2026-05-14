from __future__ import annotations

import importlib.util
import os
import sqlite3
import sys
import tempfile
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path
from unittest import mock


REPO_ROOT = Path(__file__).resolve().parents[2]
POLLER = REPO_ROOT / "swarm" / "bin" / "clawta-poller"


def load_module():
    spec = importlib.util.spec_from_loader(
        "clawta_poller_test",
        SourceFileLoader("clawta_poller_test", str(POLLER)),
    )
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules["clawta_poller_test"] = module
    spec.loader.exec_module(module)
    return module


def make_db(root: Path) -> Path:
    db_path = root / "kanban.db"
    conn = sqlite3.connect(db_path)
    conn.executescript(
        """
        CREATE TABLE tasks (
          id TEXT PRIMARY KEY,
          title TEXT NOT NULL,
          body TEXT,
          status TEXT NOT NULL,
          assignee TEXT,
          priority INTEGER DEFAULT 0,
          created_at INTEGER NOT NULL
        );
        CREATE TABLE task_comments (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          task_id TEXT NOT NULL,
          author TEXT,
          body TEXT NOT NULL,
          created_at INTEGER NOT NULL
        );
        CREATE TABLE task_events (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          task_id TEXT NOT NULL,
          kind TEXT NOT NULL,
          payload TEXT,
          created_at INTEGER
        );
        CREATE TABLE task_runs (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          task_id TEXT NOT NULL,
          status TEXT NOT NULL,
          started_at INTEGER NOT NULL
        );
        """
    )
    conn.commit()
    conn.close()
    return db_path


class ClawtaPollerDependencyTests(unittest.TestCase):
    def test_dispatch_ready_batch_skips_ticket_with_incomplete_task_run(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                """
                INSERT INTO task_runs(task_id, status, started_at)
                VALUES (?, ?, ?)
                """,
                ("t_running01", "running", 1),
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module, "fetch_ready_for_terminal_lanes",
                return_value=[{
                    "id": "t_running01",
                    "title": "already running",
                    "assignee": "codex",
                    "priority": 50,
                    "created_at": 1,
                }],
            ), mock.patch.object(module, "dispatch_ticket") as dispatch_ticket:
                dispatched, demoted, queue_size = module.dispatch_ready_batch(1, dry_run=False)

        self.assertEqual(dispatched, [])
        self.assertEqual(demoted, [])
        self.assertEqual(queue_size, 0)
        dispatch_ticket.assert_not_called()

    def test_tick_demotes_ticket_missing_invariants_and_boundaries(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                """
                INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    "t_missinginv",
                    "missing field",
                    "Acceptance:\n- add regression test",
                    "ready",
                    "codex",
                    50,
                    1,
                ),
            )
            conn.commit()
            conn.close()

            seen: list[list[str]] = []

            def fake_run(cmd, **kwargs):
                seen.append(list(cmd))
                if cmd[0] == module.KANBAN_FLOW_BIN and cmd[1] == "demote":
                    return mock.Mock(returncode=0, stdout="", stderr="")
                raise AssertionError(f"unexpected subprocess call: {cmd}")

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module, "run_invariant_repairs", return_value={"skipped": "test"}
            ), mock.patch.object(
                module, "dispatch_ready_batch", return_value=([], [], 0)
            ), mock.patch.object(
                module.subprocess, "run", side_effect=fake_run
            ):
                result = module.tick(max_dispatch=1, dry_run=False)

        self.assertEqual(result["demoted"], ["t_missinginv"])
        demote_cmd = next(cmd for cmd in seen if cmd[0] == module.KANBAN_FLOW_BIN)
        self.assertEqual(demote_cmd[:3], [module.KANBAN_FLOW_BIN, "demote", "t_missinginv"])
        self.assertIn("missing invariants_and_boundaries: field", demote_cmd[3])

    def test_tick_demotes_ticket_with_invariant_but_no_boundaries(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                """
                INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    "t_nobounds",
                    "missing boundary list",
                    "invariants_and_boundaries: Invariant: parser never returns an empty action.",
                    "ready",
                    "codex",
                    50,
                    1,
                ),
            )
            conn.commit()
            conn.close()

            seen: list[list[str]] = []

            def fake_run(cmd, **kwargs):
                seen.append(list(cmd))
                if cmd[0] == module.KANBAN_FLOW_BIN and cmd[1] == "demote":
                    return mock.Mock(returncode=0, stdout="", stderr="")
                raise AssertionError(f"unexpected subprocess call: {cmd}")

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module, "run_invariant_repairs", return_value={"skipped": "test"}
            ), mock.patch.object(
                module, "dispatch_ready_batch", return_value=([], [], 0)
            ), mock.patch.object(
                module.subprocess, "run", side_effect=fake_run
            ):
                result = module.tick(max_dispatch=1, dry_run=False)

        self.assertEqual(result["demoted"], ["t_nobounds"])
        demote_cmd = next(cmd for cmd in seen if cmd[0] == module.KANBAN_FLOW_BIN)
        self.assertIn("missing explicit boundary list", demote_cmd[3])

    def test_tick_blocks_unmerged_pr_before_routing(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                """
                INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    "t_probepr00",
                    "probe pr dependency",
                    "invariants_and_boundaries:\n"
                    "  Invariant: dependency-gated tickets do not dispatch early.\n"
                    "  Boundaries: open PR\n\n"
                    "Acceptance.\n\nDepends on PR #99999 before routing.",
                    "ready",
                    "clawta",
                    50,
                    1,
                ),
            )
            conn.commit()
            conn.close()

            seen: list[list[str]] = []

            def fake_run(cmd, **kwargs):
                seen.append(list(cmd))
                if cmd[:3] == ["gh", "pr", "view"]:
                    return mock.Mock(
                        returncode=0,
                        stdout='{"state":"OPEN","mergedAt":null,"number":99999,"url":"https://github.com/chitinhq/chitin/pull/99999"}',
                        stderr="",
                    )
                if cmd[0] == module.KANBAN_FLOW_BIN and cmd[1] == "block":
                    return mock.Mock(returncode=0, stdout="", stderr="")
                raise AssertionError(f"unexpected subprocess call: {cmd}")

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module, "run_invariant_repairs", return_value={"skipped": "test"}
            ), mock.patch.object(
                module, "fetch_routable", return_value=[]
            ), mock.patch.object(
                module, "fetch_ready_for_terminal_lanes", return_value=[]
            ), mock.patch.object(
                module.subprocess, "run", side_effect=fake_run
            ):
                result = module.tick(max_dispatch=1, dry_run=False)

        self.assertEqual(result["dependency_blocked"], ["t_probepr00"])
        self.assertEqual(result["routed"], [])
        self.assertEqual(result["queue_size"], 0)
        block_cmd = next(cmd for cmd in seen if cmd[0] == module.KANBAN_FLOW_BIN)
        self.assertEqual(block_cmd[0:3], [module.KANBAN_FLOW_BIN, "block", "t_probepr00"])
        self.assertIn("PR #99999", block_cmd[3])
        self.assertIn("state=open", block_cmd[3])

    def test_tick_blocks_triage_ticket_dependency_before_routing(self) -> None:
        """An upstream stuck in triage is uncertain — block downstream until it advances."""
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executemany(
                """
                INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                [
                (
                    "t_depprobe",
                    "probe ticket dependency",
                    "invariants_and_boundaries:\n"
                    "  Invariant: dependency-gated tickets do not dispatch early.\n"
                    "  Boundaries: in-progress dependency\n\n"
                    "Depends on t_deadbeef landing first.",
                        "ready",
                        "clawta",
                        50,
                        1,
                    ),
                    (
                        "t_deadbeef",
                        "upstream work",
                        "invariants_and_boundaries:\n"
                        "  Invariant: upstream work keeps its own boundary list.\n"
                        "  Boundaries: done, archived",
                        "triage",
                        "clawta",
                        40,
                        2,
                    ),
                ],
            )
            conn.commit()
            conn.close()

            seen: list[list[str]] = []

            def fake_run(cmd, **kwargs):
                seen.append(list(cmd))
                if cmd[0] == module.KANBAN_FLOW_BIN and cmd[1] == "block":
                    return mock.Mock(returncode=0, stdout="", stderr="")
                raise AssertionError(f"unexpected subprocess call: {cmd}")

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module, "run_invariant_repairs", return_value={"skipped": "test"}
            ), mock.patch.object(
                module, "fetch_routable", return_value=[]
            ), mock.patch.object(
                module, "fetch_ready_for_terminal_lanes", return_value=[]
            ), mock.patch.object(
                module.subprocess, "run", side_effect=fake_run
            ):
                result = module.tick(max_dispatch=1, dry_run=False)

        self.assertEqual(result["dependency_blocked"], ["t_depprobe"])
        block_cmd = next(cmd for cmd in seen if cmd[0] == module.KANBAN_FLOW_BIN)
        self.assertIn("t_deadbeef", block_cmd[3])
        self.assertIn("status=triage", block_cmd[3])

    def test_tick_does_not_block_when_upstream_is_groomed(self) -> None:
        """Upstreams in ready/todo/in_progress/done are advancing — don't block downstream.

        Regression for board-watchdog 2026-05-13: 30-ticket triage↔ready
        oscillation caused by the poller blocking tickets whose upstream
        was already in ready/in_progress, contradicting hermes' grooming
        semantics that promoted them in the first place.
        """
        module = load_module()
        for upstream_status in ("ready", "todo", "in_progress", "done"):
            with self.subTest(upstream_status=upstream_status):
                with tempfile.TemporaryDirectory() as tmp:
                    db_path = make_db(Path(tmp))
                    conn = sqlite3.connect(db_path)
                    conn.executemany(
                        """
                        INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)
                        VALUES (?, ?, ?, ?, ?, ?, ?)
                        """,
                        [
                            (
                                "t_depprobe",
                                "probe ticket dependency",
                                "invariants_and_boundaries:\n"
                                "  Invariant: dependency-gated tickets do not dispatch early.\n"
                                "  Boundaries: in-progress dependency\n\n"
                                "Depends on t_deadbeef landing first.",
                                "ready",
                                "clawta",
                                50,
                                1,
                            ),
                            (
                                "t_deadbeef",
                                "upstream work",
                                "invariants_and_boundaries:\n"
                                "  Invariant: upstream work has its own boundary list.\n"
                                "  Boundaries: done, archived",
                                upstream_status,
                                "codex",
                                40,
                                2,
                            ),
                        ],
                    )
                    conn.commit()
                    conn.close()

                    def fake_run(cmd, **kwargs):
                        if cmd[0] == module.KANBAN_FLOW_BIN and cmd[1] == "block":
                            raise AssertionError(
                                f"unexpected block call for upstream {upstream_status}: {cmd}"
                            )
                        raise AssertionError(f"unexpected subprocess call: {cmd}")

                    with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                        module, "run_invariant_repairs", return_value={"skipped": "test"}
                    ), mock.patch.object(
                        module, "fetch_routable", return_value=[]
                    ), mock.patch.object(
                        module, "fetch_ready_for_terminal_lanes", return_value=[]
                    ), mock.patch.object(
                        module.subprocess, "run", side_effect=fake_run
                    ):
                        result = module.tick(max_dispatch=1, dry_run=False)

                self.assertEqual(result["dependency_blocked"], [])

    def test_auto_unblocks_dependency_ticket_when_pr_merges(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                """
                INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    "t_unblock",
                    "blocked on pr",
                    "Depends on PR #99999.",
                    "blocked",
                    "red",
                    50,
                    1,
                ),
            )
            conn.execute(
                """
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES (?, ?, ?, ?)
                """,
                (
                    "t_unblock",
                    "clawta-poller",
                    "Blocked: dependency gate: waiting on PR #99999 (state=open)",
                    10,
                ),
            )
            conn.commit()
            conn.close()

            seen: list[list[str]] = []

            def fake_run(cmd, **kwargs):
                seen.append(list(cmd))
                if cmd[:3] == ["gh", "pr", "view"]:
                    return mock.Mock(
                        returncode=0,
                        stdout='{"state":"MERGED","mergedAt":"2026-05-13T15:00:00Z","number":99999,"url":"https://github.com/chitinhq/chitin/pull/99999"}',
                        stderr="",
                    )
                if cmd[0] == module.KANBAN_FLOW_BIN and cmd[1] == "unblock":
                    return mock.Mock(returncode=0, stdout="", stderr="")
                if cmd[:5] == ["hermes", "kanban", "--board", module.BOARD, "comment"]:
                    return mock.Mock(returncode=0, stdout="", stderr="")
                raise AssertionError(f"unexpected subprocess call: {cmd}")

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module.subprocess, "run", side_effect=fake_run
            ):
                unblocked = module.auto_unblock_dependency_tickets(dry_run=False)

        self.assertEqual(unblocked, ["t_unblock"])
        self.assertEqual(seen[1][0:4], [module.KANBAN_FLOW_BIN, "unblock", "t_unblock", "--author"])
        self.assertIn("Dependency gate cleared: PR #99999", seen[2][-1])


class ClawtaPollerRoutingTests(unittest.TestCase):
    def test_route_ticket_propagates_router_circuit_breaker_env(self) -> None:
        with mock.patch.dict(
            os.environ,
            {"CLAWTA_ROUTER_MODE": "deterministic", "CLAWTA_FORCE_DRIVER": "codex"},
            clear=False,
        ):
            module = load_module()

        ticket = {
            "id": "t_routerenv",
            "title": "router env ticket",
            "body": "body",
            "assignee": "clawta",
            "priority": 50,
        }

        def fake_run(cmd, **kwargs):
            self.assertEqual(cmd, [sys.executable, str(module.PICK_DRIVER)])
            self.assertEqual(kwargs["timeout"], 60)
            self.assertEqual(kwargs["env"]["ROUTER_MODE"], "deterministic")
            self.assertEqual(kwargs["env"]["FORCE_DRIVER"], "codex")
            self.assertEqual(kwargs["input"], '{"complexity":"low"}')
            return mock.Mock(
                returncode=0,
                stdout='{"driver":"codex","reasoning":"forced"}',
                stderr="",
            )

        with mock.patch.object(
            module, "classify_ticket_for_routing", return_value='{"complexity":"low"}'
        ), mock.patch.object(module.subprocess, "run", side_effect=fake_run):
            driver = module.route_ticket(ticket, dry_run=True)

        self.assertEqual(driver, "codex")


if __name__ == "__main__":
    unittest.main()
