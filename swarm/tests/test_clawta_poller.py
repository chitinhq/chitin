from __future__ import annotations

import importlib.util
import os
import sqlite3
import sys
import tempfile
import unittest
from contextlib import redirect_stderr
from io import StringIO
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
          idempotency_key TEXT,
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
          started_at INTEGER NOT NULL,
          ended_at INTEGER
        );
        """
    )
    conn.commit()
    conn.close()
    return db_path


class ClawtaPollerDependencyTests(unittest.TestCase):
    def test_has_spec_kit_entry_accepts_existing_board_spec(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            spec = Path(tmp) / "123-owned" / "spec.md"
            spec.parent.mkdir(parents=True)
            spec.write_text("# spec\n")
            ticket = {"body": "Spec: .specify/specs/123-owned/spec.md"}
            with mock.patch.object(module, "BOARD", "chitin"), mock.patch.object(
                module, "spec_dir_for_board", return_value=Path(tmp)
            ):
                self.assertTrue(module.has_spec_kit_entry(ticket))
                self.assertIsNone(module.missing_spec_kit_reason(ticket))

    def test_missing_spec_kit_reason_rejects_shared_ticket_without_workspace_spec(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            ticket = {"id": "t_missing1", "body": "Spec: .specify/specs/123-shared/spec.md"}
            with mock.patch.object(module, "BOARD", "readybench"), mock.patch.object(
                module, "spec_dir_for_board", return_value=Path(tmp)
            ):
                self.assertFalse(module.has_spec_kit_entry(ticket))
                self.assertIn("missing spec-kit entry", module.missing_spec_kit_reason(ticket) or "")

    def test_has_spec_kit_entry_accepts_spec_that_mentions_ticket_id(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            spec = Path(tmp) / "002-scripts-manifest" / "spec.md"
            spec.parent.mkdir(parents=True)
            spec.write_text("> Spec-kit entry for ticket `t_75c8c8c1`\n")
            ticket = {"id": "t_75c8c8c1", "body": "No spec path in ticket body"}
            with mock.patch.object(module, "BOARD", "chitin"), mock.patch.object(
                module, "spec_dir_for_board", return_value=Path(tmp)
            ):
                self.assertTrue(module.has_spec_kit_entry(ticket))
                self.assertIsNone(module.missing_spec_kit_reason(ticket))

    def test_has_spec_kit_entry_requires_exact_ticket_id_in_spec(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            spec = Path(tmp) / "002-scripts-manifest" / "spec.md"
            spec.parent.mkdir(parents=True)
            spec.write_text("> Spec-kit entry for ticket `t_75c8c8c10`\n")
            ticket = {"id": "t_75c8c8c1", "body": "No spec path in ticket body"}
            with mock.patch.object(module, "BOARD", "chitin"), mock.patch.object(
                module, "spec_dir_for_board", return_value=Path(tmp)
            ):
                self.assertFalse(module.has_spec_kit_entry(ticket))

    def test_has_spec_kit_entry_empty_boundary_rejects_ticket_without_bindings(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            ticket = {"id": "", "body": ""}
            with mock.patch.object(module, "BOARD", "chitin"), mock.patch.object(
                module, "spec_dir_for_board", return_value=Path(tmp)
            ):
                self.assertFalse(module.has_spec_kit_entry(ticket))
                self.assertIn("missing spec-kit entry", module.missing_spec_kit_reason(ticket) or "")

    def test_has_spec_kit_entry_max_boundary_accepts_large_spec_reverse_binding(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            spec = Path(tmp) / "002-scripts-manifest" / "spec.md"
            spec.parent.mkdir(parents=True)
            spec.write_text(("context\n" * 5000) + "Refs: t_75c8c8c1\n")
            ticket = {"id": "t_75c8c8c1", "body": "No spec path in ticket body"}
            with mock.patch.object(module, "BOARD", "chitin"), mock.patch.object(
                module, "spec_dir_for_board", return_value=Path(tmp)
            ):
                self.assertTrue(module.has_spec_kit_entry(ticket))

    def test_has_spec_kit_entry_error_boundary_skips_unreadable_spec(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            broken = Path(tmp) / "001-broken" / "spec.md"
            broken.parent.mkdir(parents=True)
            broken.symlink_to(Path(tmp) / "missing-target.md")
            valid = Path(tmp) / "002-scripts-manifest" / "spec.md"
            valid.parent.mkdir(parents=True)
            valid.write_text("Refs: t_75c8c8c1\n")
            ticket = {"id": "t_75c8c8c1", "body": "No spec path in ticket body"}
            with mock.patch.object(module, "BOARD", "chitin"), mock.patch.object(
                module, "spec_dir_for_board", return_value=Path(tmp)
            ):
                self.assertTrue(module.has_spec_kit_entry(ticket))

    def test_dispatch_ready_batch_skips_ticket_with_incomplete_task_run(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                ("t_running01", "already running", "", "ready", "codex", "idem-1", 50, 1),
            )
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

    def test_dispatch_ready_batch_allows_ticket_with_ended_blocked_task_run(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES ('t_blocked01', 'previously blocked', '', 'ready', 'codex', 'idem-1', 50, 1);
                INSERT INTO task_runs(task_id, status, started_at, ended_at)
                VALUES ('t_blocked01', 'blocked', 1, 2);
                """
            )
            conn.commit()
            conn.close()

            ticket = {
                "id": "t_blocked01",
                "title": "previously blocked",
                "assignee": "codex",
                "priority": 50,
                "created_at": 1,
            }
            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module, "fetch_ready_for_terminal_lanes",
                return_value=[ticket],
            ), mock.patch.object(module, "dispatch_ticket", return_value=True) as dispatch_ticket:
                dispatched, demoted, queue_size = module.dispatch_ready_batch(1, dry_run=False)

        self.assertEqual(dispatched, ["t_blocked01"])
        self.assertEqual(demoted, [])
        self.assertEqual(queue_size, 1)
        dispatch_ticket.assert_called_once_with(
            "t_blocked01",
            "codex",
            "ready terminal-lane ticket; dispatching by priority/FIFO without re-demoting from truncated body",
            False,
        )

    def test_dispatch_ready_batch_skips_ticket_when_matching_idempotency_key_is_running(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES
                  ('t_running01', 'already running', '', 'in_progress', 'codex', 'idem-shared', 50, 1),
                  ('t_duplicate2', 'duplicate ready ticket', '', 'ready', 'codex', 'idem-shared', 40, 2);
                INSERT INTO task_runs(task_id, status, started_at)
                VALUES ('t_running01', 'running', 1);
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module, "fetch_ready_for_terminal_lanes",
                return_value=[{
                    "id": "t_duplicate2",
                    "title": "duplicate ready ticket",
                    "assignee": "codex",
                    "priority": 40,
                    "created_at": 2,
                }],
            ), mock.patch.object(module, "dispatch_ticket") as dispatch_ticket:
                dispatched, demoted, queue_size = module.dispatch_ready_batch(1, dry_run=False)

        self.assertEqual(dispatched, [])
        self.assertEqual(demoted, [])
        self.assertEqual(queue_size, 0)
        dispatch_ticket.assert_not_called()
    def test_extract_dependency_refs_ignores_parent_hierarchy_refs(self) -> None:
        module = load_module()

        refs = module.extract_dependency_refs(
            "\n".join(
                [
                    "Parent: t_abcd1234",
                    "**Parent:** t_feedface",
                    "**parent**: t_decafbad",
                    "Depends on: t_deadbeef",
                    "Blocks: t_cafebabe",
                    "Plain mention t_8badf00d still counts.",
                ]
            )
        )

        self.assertEqual(
            [ref.ticket_id for ref in refs if ref.kind == "ticket"],
            ["t_deadbeef", "t_cafebabe", "t_8badf00d"],
        )

    def test_extract_dependency_refs_empty_boundary_returns_empty_refs(self) -> None:
        module = load_module()

        self.assertEqual(module.extract_dependency_refs(None), [])
        self.assertEqual(module.extract_dependency_refs(""), [])

    def test_extract_dependency_refs_max_boundary_deduplicates_many_refs(self) -> None:
        module = load_module()

        body = "\n".join(
            [
                *(f"Depends on: t_{i:08x}" for i in range(20)),
                *(f"Blocks: t_{i:08x}" for i in range(20)),
            ]
        )

        refs = module.extract_dependency_refs(body)

        self.assertEqual(len([ref for ref in refs if ref.kind == "ticket"]), 20)

    def test_extract_dependency_refs_error_boundary_treats_malformed_parent_as_plain_ref(self) -> None:
        module = load_module()

        refs = module.extract_dependency_refs("Parent t_abcd1234")

        self.assertEqual(
            [ref.ticket_id for ref in refs if ref.kind == "ticket"],
            ["t_abcd1234"],
        )

    def test_tick_demotes_ticket_missing_spec_kit_entry(self) -> None:
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
        self.assertIn("missing spec-kit entry", demote_cmd[3])

    def test_tick_demotes_ticket_with_missing_spec_file(self) -> None:
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
                    "Spec: .specify/specs/999-missing/spec.md",
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
        self.assertIn("missing spec-kit entry", demote_cmd[3])

    def test_is_tracking_epic_recognizes_marker(self) -> None:
        module = load_module()
        positives = [
            "Tracking-epic: true",
            "tracking-epic: TRUE",
            "Tracking-Epic:  yes",
            "tracking-epic: 1",
            "Header text\n\nTracking-epic: true\n\nMore text\n",
        ]
        for body in positives:
            with self.subTest(case="positive", body=body[:40]):
                self.assertTrue(module.is_tracking_epic(body))
        negatives = [
            "",
            None,
            "Goal: ship the thing",
            "Tracking-epic: false",
            "Tracking-epic: no",
            "tracking-epic: maybe",
            "Refers to Tracking-epic: true inside a sentence",
        ]
        for body in negatives:
            with self.subTest(case="negative", body=str(body)[:40]):
                self.assertFalse(module.is_tracking_epic(body))

    def test_tick_skips_demote_ungroomed_for_tracking_epic(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                "INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)"
                " VALUES (?, ?, ?, ?, ?, ?, ?)",
                ("t_epic1", "tracking epic", "Goal: tracker.\n\nTracking-epic: true\n",
                 "ready", None, 50, 1),
            )
            conn.commit(); conn.close()

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
            ), mock.patch.object(module.subprocess, "run", side_effect=fake_run):
                result = module.tick(max_dispatch=1, dry_run=False)

        self.assertEqual(result["demoted"], [])
        self.assertFalse(any(c[:2] == [module.KANBAN_FLOW_BIN, "demote"] for c in seen),
                         f"tracking epic must not be demoted; got {seen}")

    def test_tick_skips_demote_missing_invariants_for_tracking_epic(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                "INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)"
                " VALUES (?, ?, ?, ?, ?, ?, ?)",
                ("t_epic2", "tracking epic with terminal lane assignee",
                 "Goal: tracker — no invariants here.\n\nTracking-epic: true\n",
                 "ready", "codex", 50, 1),
            )
            conn.commit(); conn.close()

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
            ), mock.patch.object(module.subprocess, "run", side_effect=fake_run):
                result = module.tick(max_dispatch=1, dry_run=False)

        self.assertEqual(result["demoted"], [])
        self.assertFalse(any(c[:2] == [module.KANBAN_FLOW_BIN, "demote"] for c in seen),
                         f"tracking epic must not be demoted; got {seen}")

    def test_missing_invariants_reason_accepts_singular_and_plural_boundary(self) -> None:
        # Locks in: the gate accepts both `Boundary:` (singular) and
        # `Boundaries:` (plural) so author choice never traps a ticket
        # in a promote-demote loop. Closes t_b8e4f138.
        module = load_module()
        for form in ("Boundary: empty, max", "Boundaries: empty, max"):
            with self.subTest(form=form):
                body = (
                    "invariants_and_boundaries:\n"
                    "  Invariant: parser never returns an empty action.\n"
                    f"  {form}\n"
                )
                self.assertIsNone(
                    module.missing_invariants_reason({"body": body}),
                    f"gate should accept '{form}' form",
                )

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
                module, "demote_missing_spec_kit_entries", return_value=[]
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
                module, "demote_missing_spec_kit_entries", return_value=[]
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
                        module, "demote_missing_spec_kit_entries", return_value=[]
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

    def test_auto_unblock_skips_loop_detected_ticket(self) -> None:
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
                    "t_looped",
                    "blocked on pr",
                    "Depends on PR #99999.",
                    "blocked",
                    "red",
                    50,
                    1,
                ),
            )
            conn.executemany(
                """
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES (?, ?, ?, ?)
                """,
                [
                    (
                        "t_looped",
                        "clawta-poller",
                        "Blocked: dependency gate: waiting on PR #99999 (state=open)",
                        10,
                    ),
                    (
                        "t_looped",
                        "board-watchdog",
                        "Blocked: loop_detected=true; watchdog owns this until manual repair",
                        11,
                    ),
                ],
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module.subprocess, "run"
            ) as fake_run:
                unblocked = module.auto_unblock_dependency_tickets(dry_run=False)

        self.assertEqual(unblocked, [])
        fake_run.assert_not_called()

    def test_ticket_has_loop_detected_marker_empty_boundary_returns_false(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                """
                INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                ("t_nomarker", "blocked on pr", "", "blocked", "red", 50, 1),
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path):
                has_marker = module.ticket_has_loop_detected_marker("t_nomarker")

        self.assertFalse(has_marker)

    def test_ticket_has_loop_detected_marker_max_boundary_checks_latest_20_comments(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                """
                INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                ("t_latest20", "blocked on pr", "", "blocked", "red", 50, 1),
            )
            conn.executemany(
                """
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES (?, ?, ?, ?)
                """,
                [
                    (
                        "t_latest20",
                        "board-watchdog",
                        "Blocked: loop_detected=true; oldest comment beyond scan limit",
                        1,
                    ),
                    *[
                        (
                            "t_latest20",
                            "clawta-poller",
                            f"Blocked: dependency gate still waiting, comment {i}",
                            i,
                        )
                        for i in range(2, 22)
                    ],
                ],
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path):
                has_marker = module.ticket_has_loop_detected_marker("t_latest20")

        self.assertFalse(has_marker)

    def test_ticket_has_loop_detected_marker_error_boundary_returns_false(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "kanban.db"
            conn = sqlite3.connect(db_path)
            conn.execute(
                """
                CREATE TABLE tasks (
                  id TEXT PRIMARY KEY,
                  title TEXT NOT NULL,
                  body TEXT,
                  status TEXT NOT NULL,
                  assignee TEXT,
                  priority INTEGER DEFAULT 0,
                  created_at INTEGER NOT NULL
                )
                """
            )
            conn.execute(
                """
                INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                ("t_badshape", "blocked on pr", "", "blocked", "red", 50, 1),
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path):
                has_marker = module.ticket_has_loop_detected_marker("t_badshape")

        self.assertFalse(has_marker)


class ClawtaPollerRoutingTests(unittest.TestCase):
    def test_pick_driver_timeout_empty_boundary_uses_default(self) -> None:
        with mock.patch.dict(os.environ, {"CLAWTA_PICK_DRIVER_TIMEOUT_SECONDS": ""}, clear=False):
            module = load_module()

        self.assertEqual(module.PICK_DRIVER_TIMEOUT_SECONDS, 75)

    def test_pick_driver_timeout_max_boundary_passes_large_configured_value(self) -> None:
        with mock.patch.dict(os.environ, {"CLAWTA_PICK_DRIVER_TIMEOUT_SECONDS": "300"}, clear=False):
            module = load_module()

        ticket = {
            "id": "t_timeoutmax",
            "title": "router timeout ticket",
            "body": "body",
            "assignee": "clawta",
            "priority": 50,
        }

        def fake_run(cmd, **kwargs):
            self.assertEqual(kwargs["timeout"], 300)
            return mock.Mock(
                returncode=0,
                stdout='{"driver":"codex","reasoning":"configured timeout"}',
                stderr="",
            )

        with mock.patch.object(
            module, "classify_ticket_for_routing", return_value='{"complexity":"low"}'
        ), mock.patch.object(module.subprocess, "run", side_effect=fake_run):
            driver = module.route_ticket(ticket, dry_run=True)

        self.assertEqual(driver, "codex")

    def test_pick_driver_timeout_error_boundary_malformed_env_falls_back(self) -> None:
        stderr = StringIO()
        with mock.patch.dict(
            os.environ, {"CLAWTA_PICK_DRIVER_TIMEOUT_SECONDS": "seventy-five"}, clear=False
        ), redirect_stderr(stderr):
            module = load_module()

        self.assertEqual(module.PICK_DRIVER_TIMEOUT_SECONDS, 75)
        self.assertIn("CLAWTA_PICK_DRIVER_TIMEOUT_SECONDS", stderr.getvalue())
        self.assertIn("using 75", stderr.getvalue())

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
            self.assertEqual(kwargs["timeout"], module.PICK_DRIVER_TIMEOUT_SECONDS)
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
