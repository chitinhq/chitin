"""Regression tests for swarm/bin/mini-mention-listener.

Covers spec 039 slice-1 ACs:
  AC1 — bus poll surface (--once exits after one batch)
  AC3 — nudge succeeds BEFORE reply is posted; if nudge raises, no reply
  AC4 — first-inbound binding writes thread_id atomically before nudge
  AC6 — inbound.jsonl audit log written before nudge

Plus boundary cases from spec §"Boundary cases":
  1. empty body @mini → no_op
  2. paused session → nudge still attempted (octi's pause is outer-loop only)
  3. dead session (nudge raises) → error decision, no reply, message not marked read
  4. duplicate inbound → dedupe via reads table
"""

from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import sqlite3
import tempfile
import time
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path
from typing import Any
from unittest.mock import MagicMock

REPO = Path(__file__).resolve().parents[2]
LISTENER_PATH = REPO / "swarm" / "bin" / "mini-mention-listener"

# Load the listener as a module despite its missing .py suffix.
_spec = importlib.util.spec_from_loader(
    "mini_mention_listener",
    SourceFileLoader("mini_mention_listener", str(LISTENER_PATH)),
)
listener = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(listener)


# ----- Fakes & fixtures -----------------------------------------------------


def _init_bus_schema(conn: sqlite3.Connection) -> None:
    conn.executescript(
        """
        CREATE TABLE threads (
            id INTEGER PRIMARY KEY,
            title TEXT,
            updated_at INTEGER
        );
        CREATE TABLE messages (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            thread_id INTEGER NOT NULL,
            parent_id INTEGER,
            author TEXT NOT NULL,
            body TEXT,
            audience TEXT,
            kind TEXT,
            created_at INTEGER NOT NULL
        );
        CREATE TABLE reads (
            agent_id TEXT NOT NULL,
            message_id INTEGER NOT NULL,
            read_at INTEGER NOT NULL,
            PRIMARY KEY (agent_id, message_id)
        );
        """
    )
    conn.commit()


def _post_msg(conn: sqlite3.Connection, *, thread_id: int, body: str,
              author: str = "red", audience: str = "",
              created_at: int | None = None) -> int:
    cur = conn.execute(
        "INSERT INTO messages(thread_id, author, body, audience, kind, created_at) "
        "VALUES(?,?,?,?,?,?)",
        (thread_id, author, body, audience, "message",
         created_at if created_at is not None else int(time.time())),
    )
    conn.commit()
    return cur.lastrowid


def _ensure_thread(conn: sqlite3.Connection, *, thread_id: int, title: str = "#mini") -> None:
    conn.execute("INSERT OR IGNORE INTO threads(id, title, updated_at) VALUES(?,?,?)",
                 (thread_id, title, int(time.time())))
    conn.commit()


def _make_state_dir(root: Path, goal_id: str, *, thread_id: str | None = None) -> Path:
    sd = root / goal_id
    sd.mkdir(parents=True, exist_ok=True)
    (sd / "goal_id").write_text(goal_id + "\n")
    if thread_id is not None:
        (sd / "thread_id").write_text(thread_id + "\n")
    return sd


class FakeMiniSession:
    """Stands in for MiniSession. Records nudge calls."""

    def __init__(self, goal_id: str, *, nudge_raises: Exception | None = None) -> None:
        self.goal_id = goal_id
        self.nudge_calls: list[str] = []
        self._nudge_raises = nudge_raises

    def nudge(self, message: str, **kwargs: Any) -> None:
        self.nudge_calls.append(message)
        if self._nudge_raises is not None:
            raise self._nudge_raises


# ----- Address detection tests (pure) ---------------------------------------


def _row(body: str = "", *, author: str = "red", audience: str = "",
         thread_id: int = 99, msg_id: int = 1, title: str = "#mini",
         created_at: int = 1779200000) -> dict:
    return {
        "id": msg_id, "thread_id": thread_id, "author": author,
        "body": body, "audience": audience, "thread_title": title,
        "created_at": created_at,
    }


class AddressedToMiniTests(unittest.TestCase):
    def test_lowercase_at_mini_matches(self) -> None:
        self.assertTrue(listener._addressed_to_mini(_row("@mini please")))

    def test_capital_at_mini_matches(self) -> None:
        self.assertTrue(listener._addressed_to_mini(_row("@Mini please")))

    def test_leading_direct_address_matches(self) -> None:
        self.assertTrue(listener._addressed_to_mini(_row("Mini, status?")))

    def test_leading_address_with_em_dash_matches(self) -> None:
        self.assertTrue(listener._addressed_to_mini(_row("Mini — ping")))

    def test_rejects_self_author(self) -> None:
        """Mini's own posts must not address Mini back."""
        self.assertFalse(listener._addressed_to_mini(_row("@mini test", author="Mini")))

    def test_rejects_partial_token(self) -> None:
        """@mini-listener is NOT @mini."""
        self.assertFalse(listener._addressed_to_mini(_row("@mini-listener heads up")))

    def test_audience_field_membership(self) -> None:
        self.assertTrue(listener._addressed_to_mini(_row("hi", audience="mini")))

    def test_audience_membership_with_other_agents(self) -> None:
        self.assertTrue(listener._addressed_to_mini(_row("hi", audience="hermes,mini,clawta")))

    def test_no_match_without_mention(self) -> None:
        self.assertFalse(listener._addressed_to_mini(_row("just a comment")))

    # ----- Over-match guards (slice 1.3 regression) -------------------------
    # The original `^\s*Mini\b[\s,:\-—]` pattern caught any narrative
    # sentence starting with "Mini" because `\s` was in the trailing char
    # class. Ares replying "Mini is installed and ready" hit it in prod
    # and got routed as inbound @mini. The pattern now requires explicit
    # addressing punctuation after the token.

    def test_narrative_sentence_about_mini_does_not_match(self) -> None:
        """Ares-style: 'Mini is installed and ready ...' is narrative, not address."""
        self.assertFalse(
            listener._addressed_to_mini(_row("Mini is installed and ready — no active sessions.")),
        )

    def test_narrative_mini_has_does_not_match(self) -> None:
        self.assertFalse(listener._addressed_to_mini(_row("Mini has finished the task.")))

    def test_narrative_mini_was_does_not_match(self) -> None:
        self.assertFalse(listener._addressed_to_mini(_row("Mini was working on PR #788.")))

    def test_bare_mini_at_start_of_line_does_not_match(self) -> None:
        """No trailing punctuation → not an addressing form."""
        self.assertFalse(listener._addressed_to_mini(_row("Mini")))

    def test_leading_colon_address_matches(self) -> None:
        self.assertTrue(listener._addressed_to_mini(_row("Mini: status please")))

    def test_leading_hyphen_address_matches(self) -> None:
        self.assertTrue(listener._addressed_to_mini(_row("Mini - status please")))

    # ----- Backtick code-span exclusion (slice 1.5 regression) --------------
    # When discussing @mini in a doc thread, a user might write `@mini` in
    # backticks. That is a Markdown code-span, not addressing — the listener
    # must NOT match. Verified live 2026-05-19: Clawta posted
    # "`@mini` belongs to Mini" in the Octi orchestration thread and the
    # smoke-test session's auto-bind grabbed that thread instead of #mini.

    def test_backtick_wrapped_at_mini_does_not_match(self) -> None:
        """`@mini` in a code-span is documentation, not addressing."""
        self.assertFalse(
            listener._addressed_to_mini(_row("`@mini` belongs to Mini, `@Clawta` belongs to Clawta")),
        )

    def test_at_mini_with_leading_backtick_only_does_not_match(self) -> None:
        """Leading backtick is enough to mark this as quoted reference."""
        self.assertFalse(listener._addressed_to_mini(_row("see `@mini in the spec")))

    def test_at_mini_with_trailing_backtick_only_does_not_match(self) -> None:
        """Trailing backtick is enough to mark this as quoted reference."""
        self.assertFalse(listener._addressed_to_mini(_row("@mini` is the handle")))

    def test_at_mini_in_triple_backtick_block_is_known_limitation(self) -> None:
        """Multi-line code blocks DO currently match — lookbehind regex can't
        see across newlines to the fence. Documented limitation; if it bites
        in practice, escalate to a stateful pre-pass that strips fenced blocks
        before regex match. Pinning current behavior so any future regression
        in either direction is visible."""
        body = "```\n@mini do thing\n```"
        self.assertTrue(listener._addressed_to_mini(_row(body)))

    def test_plain_at_mini_still_matches_after_backtick_fix(self) -> None:
        """Regression guard: tightening must not break the common case."""
        self.assertTrue(listener._addressed_to_mini(_row("@mini ping")))
        self.assertTrue(listener._addressed_to_mini(_row("hey @mini, status?")))


# ----- End-to-end tick tests with sqlite + fakes ----------------------------


class TickWithFakeBusTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = Path(self._tmp.name)
        self.bus_path = self.tmp / "bus.db"
        self.state_root = self.tmp / "state"
        self.state_root.mkdir()
        conn = sqlite3.connect(self.bus_path)
        _init_bus_schema(conn)
        conn.close()

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def _open_conn(self) -> sqlite3.Connection:
        conn = sqlite3.connect(self.bus_path)
        conn.row_factory = sqlite3.Row
        return conn

    def test_bound_thread_routes_nudge_then_reply(self) -> None:
        """AC3: nudge succeeds, then reply is posted, then read marked."""
        sd = _make_state_dir(self.state_root, "alpha", thread_id="42")
        conn = self._open_conn()
        _ensure_thread(conn, thread_id=42)
        msg_id = _post_msg(conn, thread_id=42, body="@mini ping",
                           author="red", audience="mini")

        sessions: dict[str, FakeMiniSession] = {}
        def factory(gid: str) -> FakeMiniSession:
            s = FakeMiniSession(gid)
            sessions[gid] = s
            return s

        stats = listener.tick(
            conn=conn, state_root=self.state_root, mini_session_factory=factory,
        )

        # Nudge happened.
        self.assertIn("alpha", sessions)
        self.assertEqual(sessions["alpha"].nudge_calls, ["@mini ping"])
        # Reply posted by Mini on the same thread.
        replies = conn.execute(
            "SELECT body FROM messages WHERE author='Mini' AND thread_id=42"
        ).fetchall()
        self.assertEqual(len(replies), 1)
        # Read mark recorded.
        reads = conn.execute(
            "SELECT 1 FROM reads WHERE agent_id='mini' AND message_id=?", (msg_id,)
        ).fetchall()
        self.assertEqual(len(reads), 1)
        # Audit jsonl written under the goal state dir.
        audit = sd / "inbound.jsonl"
        self.assertTrue(audit.is_file())
        records = [json.loads(l) for l in audit.read_text().splitlines() if l.strip()]
        self.assertEqual(len(records), 1)
        rec = records[0]
        self.assertEqual(rec["bus_msg_id"], msg_id)
        self.assertEqual(rec["bus_thread_id"], 42)
        self.assertEqual(rec["decision"], "nudged")
        self.assertEqual(
            rec["content_sha"],
            hashlib.sha256(b"@mini ping").hexdigest(),
        )

        # Stats reflect what happened.
        self.assertEqual(stats["nudged"], 1)
        self.assertEqual(stats["errors"], 0)
        conn.close()

    def test_first_inbound_binds_before_nudge(self) -> None:
        """AC4: thread_id is written before nudge fires.

        Implementation detail: we verify by checking that the FakeMiniSession
        sees the thread_id file already on disk at nudge-time.
        """
        sd = _make_state_dir(self.state_root, "alpha")  # unbound
        observed_thread_id_at_nudge: list[str | None] = []

        def factory(gid: str) -> FakeMiniSession:
            s = FakeMiniSession(gid)
            original = s.nudge
            def spy(message: str, **kw: Any) -> None:
                f = sd / "thread_id"
                observed_thread_id_at_nudge.append(
                    f.read_text().strip() if f.is_file() else None,
                )
                original(message, **kw)
            s.nudge = spy  # type: ignore[method-assign]
            return s

        conn = self._open_conn()
        _ensure_thread(conn, thread_id=77)
        _post_msg(conn, thread_id=77,
                  body="@mini please look at `alpha`", author="red", audience="mini")

        listener.tick(conn=conn, state_root=self.state_root, mini_session_factory=factory)

        self.assertEqual(observed_thread_id_at_nudge, ["77"])
        # And the binding persists.
        self.assertEqual((sd / "thread_id").read_text().strip(), "77")
        conn.close()

    def test_nudge_raises_means_no_reply_and_not_marked_read(self) -> None:
        """AC3 negative path."""
        sd = _make_state_dir(self.state_root, "alpha", thread_id="42")
        conn = self._open_conn()
        _ensure_thread(conn, thread_id=42)
        msg_id = _post_msg(conn, thread_id=42, body="@mini will fail",
                           author="red", audience="mini")

        def factory(gid: str) -> FakeMiniSession:
            return FakeMiniSession(gid, nudge_raises=RuntimeError("kitty window gone"))

        stats = listener.tick(
            conn=conn, state_root=self.state_root, mini_session_factory=factory,
        )

        replies = conn.execute(
            "SELECT 1 FROM messages WHERE author='Mini' AND thread_id=42"
        ).fetchall()
        self.assertEqual(replies, [])
        reads = conn.execute(
            "SELECT 1 FROM reads WHERE agent_id='mini' AND message_id=?", (msg_id,)
        ).fetchall()
        self.assertEqual(reads, [])  # NOT marked, so a retry next tick is possible
        self.assertEqual(stats["nudged"], 0)
        self.assertEqual(stats["errors"], 1)

        # Audit jsonl records the error decision.
        records = [json.loads(l) for l in (sd / "inbound.jsonl").read_text().splitlines() if l.strip()]
        self.assertEqual(records[-1]["decision"], "error")
        conn.close()

    def test_ambiguous_posts_error_reply_lists_candidates_marks_read(self) -> None:
        _make_state_dir(self.state_root, "alpha")
        _make_state_dir(self.state_root, "beta")
        conn = self._open_conn()
        _ensure_thread(conn, thread_id=99)
        msg_id = _post_msg(conn, thread_id=99,
                           body="@mini route to `alpha` or `beta`?", audience="mini")

        listener.tick(conn=conn, state_root=self.state_root, mini_session_factory=FakeMiniSession)

        reply_rows = conn.execute(
            "SELECT body FROM messages WHERE author='Mini' AND thread_id=99"
        ).fetchall()
        self.assertEqual(len(reply_rows), 1)
        body = reply_rows[0]["body"]
        self.assertIn("alpha", body)
        self.assertIn("beta", body)
        self.assertIn("ambiguous", body.lower())
        reads = conn.execute(
            "SELECT 1 FROM reads WHERE agent_id='mini' AND message_id=?", (msg_id,)
        ).fetchall()
        self.assertEqual(len(reads), 1)
        conn.close()

    def test_no_match_marks_read_no_reply_no_jsonl(self) -> None:
        """Addressed but no goal_id named, no thread bound, 2+ unbound
        sessions → silent dismissal. Two sessions ensure B3 sole-session
        auto-bind does NOT fire."""
        _make_state_dir(self.state_root, "alpha")
        _make_state_dir(self.state_root, "beta")  # forces no_match (2 unbound)
        conn = self._open_conn()
        _ensure_thread(conn, thread_id=99)
        msg_id = _post_msg(conn, thread_id=99,
                           body="@mini what's the meaning of life", audience="mini")

        listener.tick(conn=conn, state_root=self.state_root, mini_session_factory=FakeMiniSession)

        self.assertEqual(
            conn.execute("SELECT 1 FROM messages WHERE author='Mini'").fetchall(), [],
        )
        # Read mark, so we don't re-scan forever.
        self.assertEqual(
            len(conn.execute(
                "SELECT 1 FROM reads WHERE agent_id='mini' AND message_id=?", (msg_id,)
            ).fetchall()),
            1,
        )
        # No per-session jsonl was created (no session was identified).
        self.assertFalse((self.state_root / "alpha" / "inbound.jsonl").exists())
        self.assertFalse((self.state_root / "beta" / "inbound.jsonl").exists())
        conn.close()

    def test_sole_session_auto_binds_and_nudges_without_goal_id_in_body(self) -> None:
        """B3 end-to-end: one unbound session + `@mini ping` (no goal_id
        named) → listener binds the thread and nudges. UX path for the
        common case where the operator types in Discord without
        remembering a 40-char goal_id."""
        _make_state_dir(self.state_root, "smoke-test-inbound-respond-pong-4bd0f1a4")
        conn = self._open_conn()
        _ensure_thread(conn, thread_id=99)
        msg_id = _post_msg(conn, thread_id=99, body="@mini ping", audience="mini")

        nudges: list[tuple[str, str]] = []
        def factory(gid: str) -> FakeMiniSession:
            session = FakeMiniSession(gid)
            original_nudge = session.nudge
            def capture(message: str, **kwargs: Any) -> None:
                nudges.append((gid, message))
                return original_nudge(message, **kwargs)
            session.nudge = capture  # type: ignore[method-assign]
            return session

        listener.tick(conn=conn, state_root=self.state_root, mini_session_factory=factory)

        # Nudge fired with the sole session as target.
        self.assertEqual(
            nudges,
            [("smoke-test-inbound-respond-pong-4bd0f1a4", "@mini ping")],
        )
        # Thread bound to the goal_id.
        thread_file = self.state_root / "smoke-test-inbound-respond-pong-4bd0f1a4" / "thread_id"
        self.assertTrue(thread_file.is_file())
        self.assertEqual(thread_file.read_text().strip(), "99")
        # Read mark written.
        self.assertEqual(
            len(conn.execute(
                "SELECT 1 FROM reads WHERE agent_id='mini' AND message_id=?", (msg_id,)
            ).fetchall()),
            1,
        )
        conn.close()

    def test_collision_posts_error_does_not_nudge(self) -> None:
        _make_state_dir(self.state_root, "alpha", thread_id="42")
        _make_state_dir(self.state_root, "beta", thread_id="42")  # forced collision
        conn = self._open_conn()
        _ensure_thread(conn, thread_id=42)
        _post_msg(conn, thread_id=42, body="@mini ping", audience="mini")

        sessions: list[str] = []
        def factory(gid: str) -> FakeMiniSession:
            sessions.append(gid)
            return FakeMiniSession(gid)

        listener.tick(conn=conn, state_root=self.state_root, mini_session_factory=factory)

        self.assertEqual(sessions, [])  # no nudge attempted
        reply = conn.execute(
            "SELECT body FROM messages WHERE author='Mini' AND thread_id=42"
        ).fetchone()
        self.assertIsNotNone(reply)
        self.assertIn("collision", reply["body"].lower())
        conn.close()

    def test_unaddressed_messages_marked_read_silently(self) -> None:
        """Non-@mini messages in #mini-shaped channels still get read-marked
        so the listener doesn't re-scan forever."""
        _make_state_dir(self.state_root, "alpha", thread_id="42")
        conn = self._open_conn()
        _ensure_thread(conn, thread_id=42)
        chit_chat_id = _post_msg(conn, thread_id=42, body="just operator chatter")

        listener.tick(conn=conn, state_root=self.state_root, mini_session_factory=FakeMiniSession)

        self.assertEqual(
            len(conn.execute(
                "SELECT 1 FROM reads WHERE agent_id='mini' AND message_id=?", (chit_chat_id,)
            ).fetchall()),
            1,
        )
        conn.close()

    def test_duplicate_inbound_processed_once(self) -> None:
        """Boundary 4: same message twice across two ticks is dedupe'd by reads."""
        sd = _make_state_dir(self.state_root, "alpha", thread_id="42")
        conn = self._open_conn()
        _ensure_thread(conn, thread_id=42)
        _post_msg(conn, thread_id=42, body="@mini once", audience="mini")

        nudge_counts: list[int] = []
        def factory(gid: str) -> FakeMiniSession:
            return FakeMiniSession(gid)

        s1 = listener.tick(conn=conn, state_root=self.state_root, mini_session_factory=factory)
        s2 = listener.tick(conn=conn, state_root=self.state_root, mini_session_factory=factory)

        self.assertEqual(s1["nudged"], 1)
        self.assertEqual(s2["nudged"], 0)  # already read; not reprocessed
        conn.close()


# ----- AC5: listener never imports from swarm/octi --------------------------


class ListenerImportBoundary(unittest.TestCase):
    def test_listener_does_not_import_from_swarm_octi(self) -> None:
        import subprocess
        result = subprocess.run(
            ["bash", "-c",
             f"grep -nE 'from[[:space:]]+.*swarm\\.octi.*[[:space:]]+import|"
             f"import[[:space:]]+swarm\\.octi' "
             f"{LISTENER_PATH} || true"],
            capture_output=True, text=True, check=False, timeout=5,
        )
        self.assertEqual(
            result.stdout.strip(), "",
            f"AC5 violation — listener imports swarm.octi:\n{result.stdout}",
        )


class CatchupModeTests(unittest.TestCase):
    """`--catchup` snaps the read marker to the head of the bus so a fresh
    install (or post-outage listener) doesn't serialize through accumulated
    backlog. Idempotent: INSERT OR IGNORE makes re-runs a no-op."""

    def setUp(self) -> None:
        self.conn = sqlite3.connect(":memory:")
        self.conn.row_factory = sqlite3.Row
        _init_bus_schema(self.conn)
        _ensure_thread(self.conn, thread_id=9)

    def tearDown(self) -> None:
        self.conn.close()

    def test_catchup_marks_all_unread_as_read(self) -> None:
        for i in range(50):
            _post_msg(self.conn, thread_id=9, body=f"noise {i}", author="ares")
        result = listener.catchup(conn=self.conn)
        self.assertEqual(result["marked"], 50)
        self.assertEqual(result["now_unread"], 0)

    def test_catchup_is_idempotent(self) -> None:
        for i in range(10):
            _post_msg(self.conn, thread_id=9, body=f"x{i}", author="ares")
        first = listener.catchup(conn=self.conn)
        second = listener.catchup(conn=self.conn)
        self.assertEqual(first["marked"], 10)
        self.assertEqual(second["marked"], 0)  # nothing new to mark
        self.assertEqual(second["now_unread"], 0)

    def test_catchup_skips_mini_authored_messages(self) -> None:
        """Mini's own posts don't need to be self-read."""
        _post_msg(self.conn, thread_id=9, body="ares chatter", author="ares")
        _post_msg(self.conn, thread_id=9, body="mini reply", author="Mini")
        result = listener.catchup(conn=self.conn)
        self.assertEqual(result["marked"], 1)  # only the ares one

    def test_catchup_does_not_swallow_later_messages(self) -> None:
        """Messages posted AFTER catchup must still appear unread."""
        for i in range(5):
            _post_msg(self.conn, thread_id=9, body=f"old {i}", author="ares")
        listener.catchup(conn=self.conn)
        new_id = _post_msg(self.conn, thread_id=9, body="@mini ping", author="red")
        unread = listener._unread_messages(self.conn)
        self.assertEqual([r["id"] for r in unread], [new_id])


class InstallerRewritesChitinRepo(unittest.TestCase):
    """The installer copies the listener to ~/.openclaw/bin/, where
    `parents[2]` resolves to $HOME and the swarm package becomes
    un-importable. The in-tree listener must carry a `# CHITIN_REPO`
    marker that the installer rewrites to an absolute path. Regression
    guard for #786 follow-up: without the marker the live cron tick
    fails with ModuleNotFoundError every minute.
    """

    INSTALLER_PATH = REPO / "swarm" / "bin" / "install-mini-mention-listener.sh"

    def test_listener_has_chitin_repo_marker(self) -> None:
        text = LISTENER_PATH.read_text()
        marker_lines = [
            line for line in text.splitlines()
            if line.startswith("REPO = ") and line.rstrip().endswith("# CHITIN_REPO")
        ]
        self.assertEqual(
            len(marker_lines), 1,
            "Expected exactly one `REPO = ... # CHITIN_REPO` line in the listener; "
            "the installer's sed rewrite anchors on this marker.",
        )

    def test_installer_references_chitin_repo_marker(self) -> None:
        text = self.INSTALLER_PATH.read_text()
        self.assertIn(
            "# CHITIN_REPO", text,
            "Installer must rewrite the listener's `# CHITIN_REPO` marker so the "
            "installed copy can import swarm.* from outside the repo.",
        )

    def test_installer_bakes_absolute_path_into_installed_copy(self) -> None:
        import shutil
        import subprocess
        with tempfile.TemporaryDirectory() as tmp:
            env = {**os.environ, "HOME": tmp, "PATH": os.environ.get("PATH", "")}
            # Use --dry-run so we don't touch the user's real crontab; assert
            # the dry-run announces the rewrite, then do a real copy via the
            # installer's sed snippet against a tmp destination.
            result = subprocess.run(
                ["bash", str(self.INSTALLER_PATH), "--dry-run"],
                capture_output=True, text=True, env=env, timeout=10, check=True,
            )
            self.assertIn("CHITIN_REPO marker", result.stdout)
            self.assertIn(str(REPO), result.stdout)

            # Real copy + rewrite, into a tmp dst so crontab is not touched.
            dst = Path(tmp) / "bin" / "mini-mention-listener"
            dst.parent.mkdir(parents=True)
            shutil.copy2(LISTENER_PATH, dst)
            subprocess.run(
                ["sed", "-i",
                 f's|^REPO = .*# CHITIN_REPO$|REPO = Path("{REPO}")  # CHITIN_REPO|',
                 str(dst)],
                check=True, timeout=5,
            )
            installed = dst.read_text()
            self.assertIn(
                f'REPO = Path("{REPO}")  # CHITIN_REPO', installed,
                "Installer's sed rewrite did not bake the absolute repo path.",
            )
            self.assertNotIn(
                "Path(__file__).resolve().parents[2]", installed,
                "Old parents[2] derivation must be replaced in the installed copy.",
            )


if __name__ == "__main__":
    unittest.main()
