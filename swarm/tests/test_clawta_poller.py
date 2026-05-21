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
    def test_resolve_boards_all_discovers_every_board_db(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            workspace = root / "workspace"
            workspace.mkdir()
            for slug in ("readybench", "chitin", "icarus"):
                board_dir = root / slug
                board_dir.mkdir()
                (board_dir / "kanban.db").write_text("")
                (board_dir / "config.json").write_text(
                    f'{{"workspace_root": "{workspace}"}}\n'
                )
            (root / "no-db").mkdir()
            unconfigured = root / "career"
            unconfigured.mkdir()
            (unconfigured / "kanban.db").write_text("")

            with mock.patch.object(module, "BOARDS_DIR", root):
                self.assertEqual(
                    module._resolve_boards("all"),
                    ["chitin", "icarus", "readybench"],
                )

    def test_resolve_boards_default_discovers_every_board_db(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            workspace = root / "workspace"
            workspace.mkdir()
            for slug in ("readybench", "chitin"):
                board_dir = root / slug
                board_dir.mkdir()
                (board_dir / "kanban.db").write_text("")
                (board_dir / "config.json").write_text(
                    f'{{"workspace_root": "{workspace}"}}\n'
                )

            with mock.patch.object(module, "BOARDS_DIR", root), mock.patch.dict(
                os.environ, {}, clear=True
            ):
                self.assertEqual(module._resolve_boards(None), ["chitin", "readybench"])

    def test_resolve_boards_explicit_kanban_db_stays_single_board(self) -> None:
        module = load_module()
        with mock.patch.dict(
            os.environ,
            {"KANBAN_DB": "/tmp/single-board.db", "KANBAN_BOARD": "readybench"},
            clear=True,
        ):
            self.assertEqual(module._resolve_boards(None), ["readybench"])

    def test_multi_board_set_board_ignores_explicit_kanban_db(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with mock.patch.object(module, "BOARDS_DIR", root), mock.patch.dict(
                os.environ, {"KANBAN_DB": "/tmp/single-board.db"}, clear=False
            ):
                module.MULTI_BOARD_MODE = True
                module._set_board("readybench")

            self.assertEqual(module.DB_PATH, root / "readybench" / "kanban.db")

    def test_fetch_routable_includes_chitin_worker_lane(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES
                  ('t_worker01', 'worker ticket', '', 'ready', 'chitin-worker', NULL, 50, 1),
                  ('t_ares0001', 'ares ticket', '', 'ready', 'ares', NULL, 40, 2);
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path):
                self.assertEqual(
                    [ticket["id"] for ticket in module.fetch_routable()],
                    ["t_worker01"],
                )

    def test_tick_leaves_unassigned_ready_ticket_in_open_pool(self) -> None:
        """Operating model (2026-05-20): an unassigned `ready` ticket is
        the open pool. The poller must NOT demote it — agents claim from
        the pool. Replaces the old demote_ungroomed behavior; see
        docs/strategy/chitin-kanban-operating-model.md.
        """
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute(
                "INSERT INTO tasks(id, title, body, status, assignee, priority, created_at)"
                " VALUES (?, ?, ?, ?, ?, ?, ?)",
                ("t_pool01", "open pool ticket", "groomed work", "ready", None, 50, 1),
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
                module, "demote_missing_spec_kit_entries", return_value=[]
            ), mock.patch.object(
                module, "dispatch_ready_batch", return_value=([], [], 0)
            ), mock.patch.object(module.subprocess, "run", side_effect=fake_run):
                result = module.tick(max_dispatch=1, dry_run=False)

        self.assertEqual(
            result["demoted"], [],
            "unassigned ready ticket must stay in the open pool, not be demoted",
        )
        self.assertFalse(
            any(c[:2] == [module.KANBAN_FLOW_BIN, "demote"] for c in seen),
            f"open-pool ticket must not trigger a demote; got {seen}",
        )
        self.assertFalse(
            hasattr(module, "demote_ungroomed"),
            "demote_ungroomed must stay removed — the poller no longer "
            "demotes tickets for lacking an assignee",
        )

    def test_missing_spec_kit_demote_only_checks_poller_owned_lanes(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES
                  ('t_worker01', 'worker ticket', '', 'ready', 'chitin-worker', NULL, 50, 1),
                  ('t_ares0001', 'ares ticket', '', 'ready', 'ares', NULL, 40, 2);
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module, "has_spec_kit_entry", return_value=False
            ), mock.patch.object(module, "demote_ticket", return_value=True) as demote:
                demoted = module.demote_missing_spec_kit_entries(dry_run=True)

        self.assertEqual(demoted, ["t_worker01"])
        demote.assert_called_once()

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


    def test_auto_unblock_respects_spec_blocked_until_capability(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            db_path = make_db(root)
            spec_dir = root / ".specify" / "specs" / "004-driver-allowlist"
            spec_dir.mkdir(parents=True)
            (spec_dir / "spec.md").write_text(
                "# Driver allowlist\n\n"
                "Ticket: t_7cb9cf49\n\n"
                "Blocked until: chitin-kernel drivers list --json\n",
            )
            conn = sqlite3.connect(db_path)
            conn.execute("ALTER TABLE tasks ADD COLUMN block_reason TEXT")
            conn.executemany(
                """
                INSERT INTO tasks(id, title, body, status, assignee, priority, created_at, block_reason)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                [
                    (
                        "t_7cb9cf49",
                        "driver gate",
                        "Depends on t_7c9d02b7.",
                        "blocked",
                        "red",
                        50,
                        1,
                        "dependency gate: waiting on t_7c9d02b7",
                    ),
                    ("t_7c9d02b7", "kernel dependency", "", "in_progress", "codex", 40, 2, None),
                ],
            )
            conn.execute(
                """
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES (?, ?, ?, ?)
                """,
                (
                    "t_7cb9cf49",
                    "clawta-poller",
                    "Blocked: dependency gate: waiting on t_7c9d02b7",
                    10,
                ),
            )
            conn.commit()
            conn.close()

            seen: list[list[str]] = []

            def fake_run(cmd, **kwargs):
                seen.append(list(cmd))
                if cmd[:4] == ["chitin-kernel", "drivers", "list", "--json"]:
                    return mock.Mock(returncode=2, stdout='{"error":"unknown_subcommand","message":"drivers"}', stderr="")
                raise AssertionError(f"unexpected subprocess call: {cmd}")

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module, "spec_dir_for_board", return_value=root / ".specify" / "specs"
            ), mock.patch.object(module.subprocess, "run", side_effect=fake_run):
                unblocked = module.auto_unblock_dependency_tickets(dry_run=False)

        self.assertEqual(unblocked, [])
        self.assertEqual(seen[0][:4], ["chitin-kernel", "drivers", "list", "--json"])

    def test_auto_unblock_respects_unresolved_dependency_gate_block_reason(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.execute("ALTER TABLE tasks ADD COLUMN block_reason TEXT")
            conn.executemany(
                """
                INSERT INTO tasks(id, title, body, status, assignee, priority, created_at, block_reason)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                [
                    (
                        "t_7cb9cf49",
                        "driver gate",
                        "Depends on t_7c9d02b7.",
                        "blocked",
                        "red",
                        50,
                        1,
                        "dependency gate: waiting on t_7c9d02b7 / chitin-kernel drivers list --json (currently unknown_subcommand)",
                    ),
                    ("t_7c9d02b7", "kernel dependency", "", "in_progress", "codex", 40, 2, None),
                ],
            )
            conn.execute(
                """
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES (?, ?, ?, ?)
                """,
                (
                    "t_7cb9cf49",
                    "clawta-poller",
                    "Blocked: dependency gate: waiting on t_7c9d02b7",
                    10,
                ),
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path), mock.patch.object(
                module.subprocess, "run"
            ) as fake_run:
                unblocked = module.auto_unblock_dependency_tickets(dry_run=False)

        self.assertEqual(unblocked, [])
        fake_run.assert_not_called()

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


class ClawtaPollerSpec067Tests(unittest.TestCase):
    # spec: 067-clawta-implementer-lanes

    def test_fetch_routable_for_routing_excludes_stage5_handoff(self) -> None:
        """AC1: routing path excludes clawta tickets with handoff."""
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES
                  ('t_impl001', 'implementer ticket', '', 'ready', 'clawta', NULL, 50, 1),
                  ('t_route001', 'routing ticket', '', 'ready', 'clawta', NULL, 40, 2);
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES
                  ('t_impl001', 'octi-handoff', 'Stage-5-handoff: clawta', 100);
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path):
                routing = module.fetch_routable_for_routing()
                routing_ids = [t["id"] for t in routing]
            self.assertIn("t_route001", routing_ids)
            self.assertNotIn("t_impl001", routing_ids)

    def test_fetch_routable_for_implementer_includes_stage5_handoff(self) -> None:
        """AC2: implementer path includes clawta tickets with handoff."""
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES
                  ('t_impl002', 'implementer ticket', '', 'ready', 'clawta', NULL, 50, 1),
                  ('t_route002', 'routing ticket', '', 'ready', 'clawta', NULL, 40, 2);
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES
                  ('t_impl002', 'octi-handoff', 'Stage-5-handoff: clawta', 100);
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path):
                impl = module.fetch_routable_for_implementer()
                impl_ids = [t["id"] for t in impl]
            self.assertIn("t_impl002", impl_ids)
            self.assertNotIn("t_route002", impl_ids)

    def test_fetch_routable_backward_compat_no_handoff(self) -> None:
        """AC3: existing clawta tickets without handoff stay in routing path."""
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES
                  ('t_worker03', 'worker ticket', '', 'ready', 'chitin-worker', NULL, 50, 1),
                  ('t_claw03', 'clawta routing', '', 'ready', 'clawta', NULL, 40, 2);
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path):
                all_routable = module.fetch_routable()
                all_ids = [t["id"] for t in all_routable]
                routing_ids = [t["id"] for t in module.fetch_routable_for_routing()]
            self.assertIn("t_worker03", all_ids)
            self.assertIn("t_claw03", all_ids)
            self.assertIn("t_worker03", routing_ids)
            self.assertIn("t_claw03", routing_ids)

    def test_clawta_implementer_lanes_env_var(self) -> None:
        """AC4: non-subset entries are warned and dropped."""
        with mock.patch.dict(
            os.environ,
            {"CLAWTA_IMPLEMENTER_LANES": "clawta,codex", "ROUTING_LANES": "clawta,chitin-worker"},
            clear=False,
        ):
            module = load_module()
        self.assertEqual(module.CLAWTA_IMPLEMENTER_LANES, ("clawta",))

    def test_tick_step5b_dispatches_implementer_lane(self) -> None:
        """AC5: tick() dispatches implementer-lane tickets to Clawta."""
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES
                  ('t_impl005', 'impl ticket', '', 'ready', 'clawta', NULL, 50, 1);
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES
                  ('t_impl005', 'octi-handoff', 'Stage-5-handoff: clawta', 100);
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path), \
                 mock.patch.object(module, "dispatch_ticket", return_value=True) as mock_dispatch, \
                 mock.patch.object(module, "filter_tickets_with_incomplete_runs",
                                   side_effect=lambda x: (x, [])), \
                 mock.patch.object(module, "route_clawta_assigned", return_value=[]), \
                 mock.patch.object(module, "fetch_routable_for_routing", return_value=[]), \
                 mock.patch.object(module, "fetch_routable_for_implementer",
                                   return_value=[{"id": "t_impl005", "title": "impl", "body": "",
                                                  "assignee": "clawta", "priority": 50,
                                                  "created_at": 1}]), \
                 mock.patch.object(module, "dispatch_ready_batch",
                                   return_value=([], [], 0)), \
                 mock.patch.object(module, "run_invariant_repairs", return_value=[]), \
                 mock.patch.object(module, "auto_unblock_dependency_tickets", return_value=[]), \
                 mock.patch.object(module, "apply_dependency_gate", return_value=[]), \
                 mock.patch.object(module, "demote_missing_spec_kit_entries", return_value=[]):
                result = module.tick(max_dispatch=5, dry_run=True)

            # dispatch_ticket should have been called for the implementer ticket
            dispatch_calls = [c for c in mock_dispatch.call_args_list
                              if c[0][0] == "t_impl005"]
            self.assertTrue(len(dispatch_calls) > 0,
                            "dispatch_ticket should be called for implementer-lane ticket")
            # Verify dispatch target is clawta
            self.assertEqual(dispatch_calls[0][0][1], "clawta")

    def test_route_clawta_assigned_routing_only_skips_implementer(self) -> None:
        """AC6: routing_only=True skips implementer-lane tickets from _pick_driver."""
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES
                  ('t_impl006', 'implementer', '', 'ready', 'clawta', NULL, 50, 1),
                  ('t_route006', 'routing', '', 'ready', 'clawta', NULL, 40, 2);
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES
                  ('t_impl006', 'octi-handoff', 'Stage-5-handoff: clawta', 100);
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path), \
                 mock.patch.object(module, "route_ticket", return_value="codex") as mock_route:
                module.route_clawta_assigned(dry_run=True, routing_only=True)
                routed_ids = [c[0][0]["id"] for c in mock_route.call_args_list]
            self.assertNotIn("t_impl006", routed_ids)
            self.assertIn("t_route006", routed_ids)

    def test_conflicting_handoff_comments_implementer_wins(self) -> None:
        """AC7/R7.2: conflicting handoff comments — implementer path wins."""
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES
                  ('t_conflict1', 'conflict ticket', '', 'ready', 'clawta', NULL, 50, 1);
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES
                  ('t_conflict1', 'octi-handoff', 'Stage-5-handoff: codex', 90),
                  ('t_conflict1', 'octi-handoff', 'Stage-5-handoff: clawta', 100);
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path):
                impl = module.fetch_routable_for_implementer()
                impl_ids = [t["id"] for t in impl]
            # At least one clawta handoff exists, so the implementer path matches
            self.assertIn("t_conflict1", impl_ids)

    def test_handoff_on_chitin_worker_ignored(self) -> None:
        """AC8/R7.3: handoff on chitin-worker ticket has no effect."""
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                INSERT INTO tasks(id, title, body, status, assignee, idempotency_key, priority, created_at)
                VALUES
                  ('t_cw008', 'worker with stray handoff', '', 'ready', 'chitin-worker', NULL, 50, 1);
                INSERT INTO task_comments(task_id, author, body, created_at)
                VALUES
                  ('t_cw008', 'octi-handoff', 'Stage-5-handoff: clawta', 100);
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "DB_PATH", db_path):
                impl = module.fetch_routable_for_implementer()
                routing = module.fetch_routable_for_routing()
                impl_ids = [t["id"] for t in impl]
                routing_ids = [t["id"] for t in routing]
            self.assertNotIn("t_cw008", impl_ids)
            self.assertIn("t_cw008", routing_ids)

    def test_ticket_has_stage5_handoff_regex(self) -> None:
        """STAGE5_HANDOFF_RE matches exactly the right patterns."""
        module = load_module()
        # Should match
        self.assertTrue(module.STAGE5_HANDOFF_RE.search("Stage-5-handoff: clawta"))
        self.assertTrue(module.STAGE5_HANDOFF_RE.search("Stage-5-handoff:  clawta "))
        self.assertTrue(module.STAGE5_HANDOFF_RE.search(
            "Some context\nStage-5-handoff: clawta\nMore text"
        ))
        # Should NOT match
        self.assertFalse(module.STAGE5_HANDOFF_RE.search("stage-5-handoff: clawta"))  # case
        self.assertFalse(module.STAGE5_HANDOFF_RE.search("Stage-5-handoff: codex"))
        self.assertFalse(module.STAGE5_HANDOFF_RE.search("**Stage-5-handoff: clawta**"))


if __name__ == "__main__":
    unittest.main()
