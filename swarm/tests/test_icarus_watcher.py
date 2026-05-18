"""Tests for swarm/bin/icarus-watcher (ic-001 v1).

Covers the 8 spec invariants + 6 boundary cases. Uses unittest +
mocks so it runs in CI without an RTX 3090 / ollama daemon.

Per ic-001 spec §E2E coverage:
- icarus-watcher: skill match + dispatch (e2e here as integration test)
- icarus-watcher: skip non-matching + disabled skills (unit)
- post-check: lint-fix lane (unit)
- loud-fail on N=2 retries exceeded with split routing (e2e)
- WORKER_RECEIPT 6-field contract (e2e)
- VRAM lease/lock contention (unit)
- dedup composite key (unit)
"""

from __future__ import annotations

import importlib.util
import json
import os
import sqlite3
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

REPO = Path(__file__).resolve().parents[2]
WATCHER_PATH = REPO / "swarm" / "bin" / "icarus-watcher"


def load_watcher_module():
    """Import the watcher script as a module (it has no .py extension)."""
    from importlib.machinery import SourceFileLoader
    loader = SourceFileLoader("icarus_watcher", str(WATCHER_PATH))
    spec = importlib.util.spec_from_loader("icarus_watcher", loader)
    mod = importlib.util.module_from_spec(spec)
    loader.exec_module(mod)
    return mod


# Loaded once for all tests
watcher = load_watcher_module()


def _make_fake_board(tmpdir: Path, tickets: list[dict]) -> Path:
    """Create a minimal kanban.db with the given tickets."""
    db_path = tmpdir / "kanban.db"
    con = sqlite3.connect(db_path)
    con.executescript("""
        CREATE TABLE tasks (
            id TEXT PRIMARY KEY,
            title TEXT NOT NULL,
            body TEXT,
            assignee TEXT,
            status TEXT NOT NULL,
            priority INTEGER DEFAULT 0,
            created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
        );
        CREATE TABLE kanban_mutations_log (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            ts INTEGER NOT NULL DEFAULT (strftime('%s','now')),
            table_name TEXT NOT NULL,
            op TEXT NOT NULL,
            task_id TEXT
        );
    """)
    for t in tickets:
        con.execute(
            "INSERT INTO tasks (id, title, body, assignee, status, priority) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            (t["id"], t["title"], t["body"], t["assignee"], t["status"],
             t.get("priority", 0)),
        )
    con.commit()
    con.close()
    return db_path


# ── Skill parsing ─────────────────────────────────────────────────

class TestSkillParsing(unittest.TestCase):
    """Invariant 1: skill-scoped only."""

    def test_extracts_skill_from_body(self):
        body = "Some intro\nskill: lint-fix\nmore text"
        self.assertEqual(watcher.parse_skill(body), "lint-fix")

    def test_case_insensitive(self):
        self.assertEqual(watcher.parse_skill("Skill: LINT-FIX"), "lint-fix")

    def test_returns_none_when_missing(self):
        self.assertIsNone(watcher.parse_skill("no skill line here"))

    def test_returns_none_for_empty(self):
        self.assertIsNone(watcher.parse_skill(""))


# ── Disabled-lane filter (Week-1 lint-fix only) ───────────────────

class TestEnabledLanes(unittest.TestCase):
    """Per Clawta amendment msg 4476: Week 1 = lint-fix ONLY.
    Other 4 lanes are spec-approved but DISABLED. Silent skip."""

    def test_only_lint_fix_enabled_in_v1(self):
        self.assertEqual(watcher.ENABLED_LANES, {"lint-fix"})

    def test_all_5_lanes_spec_approved(self):
        self.assertEqual(watcher.SPEC_APPROVED_LANES, {
            "lint-fix", "log-pattern", "triage-classify",
            "doc-from-code", "mechanical",
        })

    def test_disabled_lane_silent_skip(self):
        """Tickets with disabled-lane skill must not invoke the model."""
        ticket = {
            "id": "t_test", "title": "t", "body": "skill: log-pattern",
            "skill": "log-pattern", "status": "ready", "assignee": "icarus",
            "priority": 0, "latest_ts": 0,
        }
        with mock.patch.object(watcher, "ollama_generate") as mock_gen, \
             mock.patch.object(watcher, "vram_lease"), \
             mock.patch.object(watcher, "ollama_ps_inspect"), \
             mock.patch.object(watcher, "post_worker_receipt"):
            result = watcher.process_ticket(ticket, "qwen3-coder:30b-32k")
        self.assertTrue(result.get("skipped"))
        mock_gen.assert_not_called()


# ── Dedup composite key (sw-009 gate 4 precedent) ────────────────

class TestDedupKey(unittest.TestCase):
    def test_composite_key_includes_all_4_fields(self):
        t = {"id": "t_abc", "status": "ready", "assignee": "icarus",
             "latest_ts": 12345}
        key = watcher.dedup_key(t)
        self.assertEqual(key, "t_abc:ready:icarus:12345")

    def test_status_change_makes_new_key(self):
        t1 = {"id": "t_x", "status": "ready", "assignee": "icarus", "latest_ts": 100}
        t2 = {"id": "t_x", "status": "ready", "assignee": "icarus", "latest_ts": 200}
        self.assertNotEqual(watcher.dedup_key(t1), watcher.dedup_key(t2))


# ── Board read-only fetch (invariant 8 read-only) ─────────────────

class TestFetchReadyTickets(unittest.TestCase):
    """Invariant 8: Icarus reads board, never writes."""

    def test_returns_only_ready_status_with_spec_approved_skill(self):
        with tempfile.TemporaryDirectory() as td:
            tdir = Path(td) / "icarus"
            tdir.mkdir()
            _make_fake_board(tdir, [
                {"id": "t_1", "title": "ready+lint", "body": "skill: lint-fix",
                 "assignee": "icarus", "status": "ready"},
                {"id": "t_2", "title": "triage+lint", "body": "skill: lint-fix",
                 "assignee": "icarus", "status": "triage"},
                {"id": "t_3", "title": "ready+unknown", "body": "skill: unknown-lane",
                 "assignee": "icarus", "status": "ready"},
                {"id": "t_4", "title": "ready+no-skill", "body": "no skill line",
                 "assignee": "icarus", "status": "ready"},
                {"id": "t_5", "title": "ready+wrong-assignee", "body": "skill: lint-fix",
                 "assignee": "ares", "status": "ready"},
                {"id": "t_6", "title": "ready+wildcard", "body": "skill: lint-fix",
                 "assignee": "*", "status": "ready"},
            ])
            with mock.patch.object(watcher, "KANBAN_ROOT", Path(td)):
                tickets = watcher.fetch_ready_tickets("icarus")
        ids = {t["id"] for t in tickets}
        # t_1 (ready+lint-fix+icarus) and t_6 (ready+lint-fix+wildcard) only
        self.assertEqual(ids, {"t_1", "t_6"})


# ── VRAM lease (invariant 4) ──────────────────────────────────────

class TestVramLease(unittest.TestCase):
    """Invariant 4: explicit lease/lock prevents concurrent model swaps."""

    def test_lease_acquires_and_releases(self):
        with tempfile.TemporaryDirectory() as td:
            with mock.patch.object(watcher, "DEDUP_DIR", Path(td)), \
                 mock.patch.object(watcher, "LEASE_LOCK", Path(td) / "lease.lock"):
                # Should not raise
                with watcher.vram_lease(timeout_s=1):
                    pass

    def test_lease_times_out_when_held(self):
        """If another holder has the lock, we wait <= timeout_s then raise."""
        with tempfile.TemporaryDirectory() as td:
            lease_path = Path(td) / "lease.lock"
            with mock.patch.object(watcher, "DEDUP_DIR", Path(td)), \
                 mock.patch.object(watcher, "LEASE_LOCK", lease_path):
                # Hold the lock from a "parallel" file handle
                import fcntl
                holder = lease_path.open("a+")
                fcntl.flock(holder.fileno(), fcntl.LOCK_EX)
                try:
                    with self.assertRaises(watcher.IcarusVramContention):
                        with watcher.vram_lease(timeout_s=1):
                            pass
                finally:
                    fcntl.flock(holder.fileno(), fcntl.LOCK_UN)
                    holder.close()


# ── Loud-fail with split escalation routes (invariant 3) ──────────

class TestSplitEscalation(unittest.TestCase):
    """Invariant 3: capability → Clawta, infra → operator. Don't conflate."""

    def _make_lint_fix_ticket(self):
        return {
            "id": "t_esc", "title": "esc test",
            "body": "skill: lint-fix\npath: /tmp/x\nlinter: ruff\nlint_command: ruff /tmp/x",
            "skill": "lint-fix", "status": "ready", "assignee": "icarus",
            "priority": 0, "latest_ts": 0,
        }

    def test_infra_failure_routes_to_operator(self):
        ticket = self._make_lint_fix_ticket()
        with mock.patch.object(watcher, "vram_lease"), \
             mock.patch.object(watcher, "ollama_ps_inspect",
                               side_effect=watcher.IcarusInfraFailure("gpu oom")), \
             mock.patch.object(watcher, "post_worker_receipt") as mock_receipt:
            result = watcher.process_ticket(ticket, "qwen3-coder:30b-32k")
        self.assertEqual(result.get("escalated"), "operator")
        # Receipt should have block_reason=infra_failure
        mock_receipt.assert_called_once()
        kwargs = mock_receipt.call_args.kwargs
        self.assertEqual(kwargs["block_reason"], "infra_failure")

    def test_capability_ceiling_routes_to_clawta(self):
        ticket = self._make_lint_fix_ticket()
        # Mock lane handler to always return fail (post-check fails)
        fail_result = {"status": "fail", "post_check_output": "still bad",
                       "diff_path": "/tmp/diff"}
        with mock.patch.object(watcher, "vram_lease"), \
             mock.patch.object(watcher, "ollama_ps_inspect"), \
             mock.patch.dict(watcher.LANE_HANDLERS,
                             {"lint-fix": lambda t, m: fail_result}), \
             mock.patch.object(watcher, "post_worker_receipt") as mock_receipt:
            result = watcher.process_ticket(ticket, "qwen3-coder:30b-32k")
        self.assertEqual(result.get("escalated"), "clawta")
        # Receipt should have block_reason=local_ceiling_exceeded
        kwargs = mock_receipt.call_args.kwargs
        self.assertEqual(kwargs["block_reason"], "local_ceiling_exceeded")
        self.assertEqual(kwargs["retry_count"], 2)


# ── WORKER_RECEIPT contract (invariant 5) ─────────────────────────

class TestReceiptContract(unittest.TestCase):
    """Invariant 5: 6 enumerated fields per Clawta amendment."""

    def test_receipt_includes_all_6_fields(self):
        ticket = {"id": "t_r", "title": "receipt test", "body": "",
                  "skill": "lint-fix", "status": "ready", "assignee": "icarus",
                  "priority": 0, "latest_ts": 0}
        captured = []

        def fake_print(arg):
            captured.append(arg)

        with mock.patch("builtins.print", side_effect=fake_print), \
             mock.patch("importlib.util.spec_from_file_location",
                        return_value=None):  # bus post is best-effort, skip
            try:
                watcher.post_worker_receipt(
                    ticket, lane="lint-fix", prompt_class="lint-fix",
                    post_check_output="ok", diff_path="/tmp/x",
                    retry_count=0, model_used="qwen3-coder:30b-32k",
                    success=True,
                )
            except Exception:
                pass  # bus post may fail (no agent-bus in test env)

        # Find the WORKER_RECEIPT line in captured prints
        receipt_line = next(
            (c for c in captured if "WORKER_RECEIPT" in str(c)), None,
        )
        self.assertIsNotNone(receipt_line)
        # Extract the JSON payload after the colon
        payload_str = receipt_line.split("WORKER_RECEIPT:", 1)[1].strip()
        payload = json.loads(payload_str)
        required_fields = {"lane", "prompt_class", "post_check_output",
                           "diff_path", "retry_count", "model_used"}
        self.assertTrue(required_fields.issubset(payload.keys()),
                        f"missing fields: {required_fields - payload.keys()}")


# ── Lint-fix post-check (lane handler) ────────────────────────────

class TestLintFixPostCheck(unittest.TestCase):
    """Per spec §Lane spec: lint-fix post-check = `lint` exit 0 on fixed file."""

    def test_noop_when_file_already_lints_clean(self):
        """If pre-check passes, lane returns noop without invoking the model.

        Per Ares review: uses allowlisted `ruff` command + mocks
        subprocess.run so test doesn't depend on ruff being installed."""
        with tempfile.NamedTemporaryFile(
            mode="w", suffix=".py", delete=False,
        ) as tf:
            tf.write("x = 1\n")
            path = tf.name
        try:
            body = (f"skill: lint-fix\npath: {path}\nlinter: ruff\n"
                    f"lint_command: ruff check {path}")
            ticket = {"id": "t_lf", "title": "noop test", "body": body,
                      "skill": "lint-fix", "status": "ready",
                      "assignee": "icarus", "priority": 0, "latest_ts": 0}
            fake_pass = mock.MagicMock(returncode=0, stdout="", stderr="")
            with mock.patch.object(watcher.subprocess, "run",
                                   return_value=fake_pass), \
                 mock.patch.object(watcher, "ollama_generate") as mock_gen:
                result = watcher.lane_lint_fix(ticket, "qwen3-coder:30b-32k")
            self.assertEqual(result["status"], "noop")
            mock_gen.assert_not_called()
        finally:
            os.unlink(path)

    def test_pass_when_model_output_lints_clean(self):
        """Pre-fail + model output lints clean → status=pass.

        Mocks subprocess.run with side_effect list: first call (pre-lint)
        returns 1, second call (post-lint on tmp file) returns 0."""
        with tempfile.NamedTemporaryFile(
            mode="w", suffix=".py", delete=False,
        ) as tf:
            tf.write("bad\n")
            path = tf.name
        try:
            body = (f"skill: lint-fix\npath: {path}\nlinter: ruff\n"
                    f"lint_command: ruff check {path}")
            ticket = {"id": "t_lf2", "title": "pass test", "body": body,
                      "skill": "lint-fix", "status": "ready",
                      "assignee": "icarus", "priority": 0, "latest_ts": 0}
            fake_fail = mock.MagicMock(returncode=1, stdout="lint errors", stderr="")
            fake_pass = mock.MagicMock(returncode=0, stdout="", stderr="")
            with mock.patch.object(watcher.subprocess, "run",
                                   side_effect=[fake_fail, fake_pass]), \
                 mock.patch.object(watcher, "ollama_generate",
                                   return_value="fixed_content"), \
                 tempfile.TemporaryDirectory() as fixes_td, \
                 mock.patch.object(watcher, "DEDUP_DIR", Path(fixes_td)):
                result = watcher.lane_lint_fix(ticket, "qwen3-coder:30b-32k")
            self.assertEqual(result["status"], "pass")
            self.assertIn("post_check_output", result)
            self.assertIn("diff_path", result)
        finally:
            os.unlink(path)

    def test_shell_injection_blocked_by_allowlist(self):
        """Per Ares review (msg 4563): lint_command starting with
        non-allowlisted binary must loud-fail before any subprocess.run
        call. Prevents arbitrary shell from a malicious ticket body."""
        with tempfile.NamedTemporaryFile(
            mode="w", suffix=".py", delete=False,
        ) as tf:
            tf.write("x = 1\n")
            path = tf.name
        try:
            body = (f"skill: lint-fix\npath: {path}\nlinter: ruff\n"
                    f"lint_command: bash -c 'rm -rf /'")  # malicious
            ticket = {"id": "t_lf_evil", "title": "shell injection",
                      "body": body, "skill": "lint-fix", "status": "ready",
                      "assignee": "icarus", "priority": 0, "latest_ts": 0}
            with self.assertRaises(watcher.IcarusTicketMalformed) as ctx:
                watcher.lane_lint_fix(ticket, "qwen3-coder:30b-32k")
            self.assertIn("not in allowlist", str(ctx.exception))
        finally:
            os.unlink(path)

    def test_malformed_ticket_raises(self):
        """Ticket missing path/linter/lint_command → IcarusTicketMalformed."""
        body = "skill: lint-fix"  # missing all 3 fields
        ticket = {"id": "t_lf3", "title": "malformed", "body": body,
                  "skill": "lint-fix", "status": "ready",
                  "assignee": "icarus", "priority": 0, "latest_ts": 0}
        with self.assertRaises(watcher.IcarusTicketMalformed):
            watcher.lane_lint_fix(ticket, "qwen3-coder:30b-32k")


if __name__ == "__main__":
    unittest.main(verbosity=2)
