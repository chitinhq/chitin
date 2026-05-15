"""inbox.py tests — run via subprocess so we exercise the actual CLI behavior."""
from __future__ import annotations

import os
import subprocess
import sys
import sqlite3
import tempfile
import time
import unittest
from pathlib import Path


SVC_DIR = Path(__file__).resolve().parents[1]
INBOX_PY = SVC_DIR / "inbox.py"
SCHEMA_SQL = SVC_DIR / "schema.sql"


def _seed_db(db_path: Path) -> None:
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(db_path))
    conn.executescript(SCHEMA_SQL.read_text())
    conn.commit()
    conn.close()


def _post(db_path: Path, *, author: str, title: str, body: str,
          audience: str | None = None, board: str | None = None,
          kind: str = "message", ack_required: int = 0) -> tuple[int, int]:
    conn = sqlite3.connect(str(db_path))
    now = int(time.time())
    cur = conn.cursor()
    cur.execute(
        "INSERT INTO threads(board, title, author, audience, created_at, updated_at) "
        "VALUES(?,?,?,?,?,?)",
        (board, title, author, audience, now, now),
    )
    tid = cur.lastrowid
    cur.execute(
        "INSERT INTO messages(thread_id, author, audience, body, kind, ack_required, created_at) "
        "VALUES(?,?,?,?,?,?,?)",
        (tid, author, audience, body, kind, ack_required, now),
    )
    mid = cur.lastrowid
    conn.commit()
    conn.close()
    return tid, mid


def _run_inbox(db_path: Path, *args: str) -> subprocess.CompletedProcess:
    env = {**os.environ, "AGENT_BUS_DB": str(db_path)}
    return subprocess.run(
        [sys.executable, str(INBOX_PY), *args],
        capture_output=True, text=True, env=env, timeout=10,
    )


class InboxCLITests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.db = Path(self.tmp.name) / "bus.db"

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def test_silent_when_db_missing(self) -> None:
        # No DB at all; inbox should print nothing and exit 0.
        result = _run_inbox(self.db, "--agent", "red")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_silent_when_inbox_empty(self) -> None:
        _seed_db(self.db)
        result = _run_inbox(self.db, "--agent", "red")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_renders_unread_addressed_to_agent(self) -> None:
        _seed_db(self.db)
        _post(self.db, author="hermes", title="watchdog ack",
              body="cleared loop counter for the 4 epics",
              audience="red", board="chitin")
        _post(self.db, author="clawta", title="another for someone else",
              body="not for red", audience="hermes")
        _post(self.db, author="hermes", title="broadcast",
              body="for everyone", audience="*")

        result = _run_inbox(self.db, "--agent", "red")
        self.assertEqual(result.returncode, 0, msg=result.stderr)
        self.assertIn("agent-bus inbox", result.stdout)
        self.assertIn("watchdog ack", result.stdout)        # red-addressed
        self.assertIn("broadcast", result.stdout)           # *-addressed
        self.assertNotIn("another for someone else", result.stdout)  # hermes-only
        self.assertIn("from `hermes`", result.stdout)
        self.assertIn("cleared loop counter", result.stdout)

    def test_excludes_own_posts(self) -> None:
        _seed_db(self.db)
        _post(self.db, author="red", title="self note", body="x",
              audience="red,hermes")
        result = _run_inbox(self.db, "--agent", "red")
        self.assertEqual(result.stdout, "")

    def test_mark_read_clears_inbox(self) -> None:
        _seed_db(self.db)
        _post(self.db, author="hermes", title="t", body="hi", audience="red")
        first = _run_inbox(self.db, "--agent", "red", "--mark-read")
        self.assertIn("agent-bus inbox", first.stdout)
        # Second call should be silent now that the read was recorded.
        second = _run_inbox(self.db, "--agent", "red")
        self.assertEqual(second.stdout, "")

    def test_limit_caps_results(self) -> None:
        _seed_db(self.db)
        for i in range(5):
            _post(self.db, author="hermes", title=f"msg {i}",
                  body=f"body {i}", audience="red")
        result = _run_inbox(self.db, "--agent", "red", "--limit", "2")
        # Each entry produces 2 lines (header + snippet); 2 entries = 4 list lines
        list_lines = [l for l in result.stdout.splitlines() if l.startswith("- ")]
        self.assertEqual(len(list_lines), 2)


if __name__ == "__main__":
    unittest.main()
