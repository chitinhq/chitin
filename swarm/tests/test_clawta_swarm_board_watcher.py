from __future__ import annotations

import json
import os
import sqlite3
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "swarm" / "bin" / "clawta-swarm-board-watcher"


def make_db(path: Path) -> None:
    conn = sqlite3.connect(path)
    conn.execute(
        """
        CREATE TABLE tasks (
          id TEXT PRIMARY KEY,
          title TEXT NOT NULL,
          body TEXT,
          assignee TEXT,
          status TEXT NOT NULL,
          priority INTEGER DEFAULT 0,
          created_at INTEGER NOT NULL
        )
        """
    )
    conn.executemany(
        "INSERT INTO tasks(id,title,assignee,status,priority,created_at) VALUES(?,?,?,?,?,?)",
        [
            ("sw-001", "Clawta direct", "clawta", "ready", 1, 1),
            ("sw-002", "Wildcard direct", "*", "ready", 2, 2),
            ("sw-003", "Wrong assignee", "ares", "ready", 3, 3),
            ("sw-004", "Wrong status", "clawta", "triage", 4, 4),
        ],
    )
    conn.commit()
    conn.close()


def test_watcher_posts_once_for_ready_clawta_or_wildcard(tmp_path: Path) -> None:
    db = tmp_path / "kanban.db"
    state = tmp_path / "seen.txt"
    send_log = tmp_path / "sends.log"
    fake_openclaw = tmp_path / "openclaw"
    make_db(db)
    fake_openclaw.write_text(
        f"#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >>{send_log}\n"
    )
    fake_openclaw.chmod(0o755)

    env = {
        **os.environ,
        "KANBAN_DB": str(db),
        "CLAWTA_SWARM_WATCHER_STATE": str(state),
        "OPENCLAW_BIN": str(fake_openclaw),
    }
    first = subprocess.run([str(SCRIPT)], env=env, text=True, capture_output=True, check=True)
    second = subprocess.run([str(SCRIPT)], env=env, text=True, capture_output=True, check=True)

    assert json.loads(first.stdout) == {"board": "swarm", "posted": 2}
    assert json.loads(second.stdout) == {"board": "swarm", "posted": 0}
    log = send_log.read_text()
    assert "channel:1505613628286701588" in log
    assert "-m 🔔 clawta detected swarm board ticket: sw-001 Clawta direct" in log
    assert "sw-002 Wildcard direct" in log
    assert "sw-003" not in log
    assert "sw-004" not in log
