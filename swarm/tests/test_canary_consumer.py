"""Tests for canary-consumer: lockdown_loop_detected chain event consumer."""
from __future__ import annotations

import json
import sqlite3
import sys
import types
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch

import pytest

# Import the consumer as a module — it's a script in swarm/bin/ without .py suffix.
# Dashes aren't valid Python identifiers, so we use exec-based loading.
_BIN_DIR = str(Path(__file__).parent.parent / "bin")
_CONSUMER_PATH = str(Path(_BIN_DIR) / "canary-consumer")

# Set up argus paths first so the consumer's imports work
_ARGUS_SRC = str(Path(__file__).parent.parent.parent / "python" / "argus" / "src")
_ARGUS_PKG = str(Path(__file__).parent.parent.parent / "python" / "argus")
_ANALYSIS_SRC = str(Path(__file__).parent.parent.parent / "python" / "analysis" / "src")
_ANALYSIS_PKG = str(Path(__file__).parent.parent.parent / "python" / "analysis")
for _p in [_ARGUS_SRC, _ARGUS_PKG, _ANALYSIS_SRC, _ANALYSIS_PKG]:
    if _p not in sys.path:
        sys.path.insert(0, _p)

canary_consumer = types.ModuleType("canary_consumer")
canary_consumer.__file__ = _CONSUMER_PATH
sys.modules["canary_consumer"] = canary_consumer
with open(_CONSUMER_PATH) as _f:
    _code = compile(_f.read(), _CONSUMER_PATH, "exec")
    exec(_code, canary_consumer.__dict__)

from argus.detectors import Finding
from argus import findings_store, migrations
from argus.indexer import init_db


@pytest.fixture
def chitin_dir(tmp_path: Path) -> Path:
    """Create a temporary chitin home directory with chain_index and findings DBs."""
    # Create chain_index.sqlite with session_events table
    chain_db = tmp_path / "chain_index.sqlite"
    chain_conn = sqlite3.connect(str(chain_db))
    chain_conn.execute("""
        CREATE TABLE IF NOT EXISTS session_events (
            id          INTEGER PRIMARY KEY,
            line_hash   TEXT UNIQUE NOT NULL,
            driver_type TEXT NOT NULL,
            chain_id    TEXT NOT NULL DEFAULT '',
            session_id  TEXT NOT NULL DEFAULT '',
            event_type  TEXT NOT NULL DEFAULT '',
            ts          TEXT NOT NULL DEFAULT '',
            ts_unix     INTEGER NOT NULL DEFAULT 0,
            agent       TEXT NOT NULL DEFAULT '',
            surface     TEXT NOT NULL DEFAULT '',
            action_type TEXT NOT NULL DEFAULT '',
            action_target TEXT NOT NULL DEFAULT '',
            tool_name   TEXT NOT NULL DEFAULT '',
            decision    TEXT NOT NULL DEFAULT '',
            rule_id     TEXT NOT NULL DEFAULT '',
            reason      TEXT NOT NULL DEFAULT '',
            escalation  TEXT NOT NULL DEFAULT '',
            mode        TEXT NOT NULL DEFAULT '',
            cwd         TEXT NOT NULL DEFAULT '',
            model       TEXT NOT NULL DEFAULT '',
            role        TEXT NOT NULL DEFAULT '',
            fingerprint TEXT NOT NULL DEFAULT '',
            workflow_id TEXT NOT NULL DEFAULT '',
            envelope_id TEXT NOT NULL DEFAULT '',
            payload_json TEXT,
            source_file TEXT NOT NULL,
            source_line  INTEGER NOT NULL DEFAULT 0,
            last_seen_ts INTEGER NOT NULL DEFAULT 0
        )
    """)
    chain_conn.execute("CREATE INDEX IF NOT EXISTS idx_se_event_type ON session_events(event_type, ts_unix)")
    chain_conn.execute("""
        CREATE TABLE IF NOT EXISTS session_index_checkpoints (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            source_key TEXT UNIQUE NOT NULL,
            last_line INTEGER NOT NULL DEFAULT 0,
            last_hash TEXT NOT NULL DEFAULT '',
            updated_at REAL NOT NULL DEFAULT 0
        )
    """)
    chain_conn.commit()
    chain_conn.close()

    # Create findings DB directory
    (tmp_path / "analysis").mkdir(parents=True, exist_ok=True)
    findings_db = tmp_path / "analysis" / "findings.db"
    fconn = init_db(findings_db)
    migrations.apply_pending(fconn)
    fconn.close()

    return tmp_path


@pytest.fixture
def chain_conn(chitin_dir: Path) -> sqlite3.Connection:
    """Return an open connection to chain_index.sqlite."""
    conn = sqlite3.connect(str(chitin_dir / "chain_index.sqlite"))
    conn.row_factory = sqlite3.Row
    yield conn
    conn.close()


@pytest.fixture
def findings_conn(chitin_dir: Path) -> sqlite3.Connection:
    """Return an open connection to findings.db."""
    conn = sqlite3.connect(str(chitin_dir / "analysis" / "findings.db"))
    yield conn
    conn.close()


def insert_lockdown_event(
    conn: sqlite3.Connection,
    agent: str = "test-agent",
    lockdown_count: int = 3,
    timestamp: str = "2026-05-20T12:00:00Z",
    payload: dict | None = None,
) -> int:
    """Insert a lockdown_loop_detected event into session_events and return rowid."""
    if payload is None:
        payload = {
            "agent": agent,
            "rationale": "loop detected in tool calls",
            "lockdown_count": lockdown_count,
            "window_sec": 60,
            "action": "deny",
            "threshold": 5,
        }
    conn.execute(
        """INSERT INTO session_events
           (line_hash, driver_type, event_type, ts, ts_unix, agent, action_type,
            payload_json, source_file, source_line)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
        (
            f"hash-{agent}-{lockdown_count}",
            "hermes",
            "lockdown_loop_detected",
            timestamp,
            1747742400,
            agent,
            "deny",
            json.dumps(payload),
            "test.jsonl",
            1,
        ),
    )
    conn.commit()
    row = conn.execute("SELECT last_insert_rowid()").fetchone()
    return row[0]


def insert_other_event(
    conn: sqlite3.Connection,
    event_type: str = "tool_call",
    agent: str = "other-agent",
) -> int:
    """Insert a non-lockdown event to test filtering."""
    conn.execute(
        """INSERT INTO session_events
           (line_hash, driver_type, event_type, ts, ts_unix, agent, action_type,
            payload_json, source_file, source_line)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
        (
            f"hash-{event_type}-{agent}",
            "codex",
            event_type,
            "2026-05-20T12:00:00Z",
            1747742400,
            agent,
            event_type,
            "{}",
            "other.jsonl",
            1,
        ),
    )
    conn.commit()
    row = conn.execute("SELECT last_insert_rowid()").fetchone()
    return row[0]


# ---------------------------------------------------------------------------
# Tests: session_events path
# ---------------------------------------------------------------------------


class TestProcessEventSessionEvents:
    """Test process_events with session_events table."""

    def test_no_events_returns_zero(self, chitin_dir, chain_conn, findings_conn):
        """When there are no events, process_events returns 0."""
        count = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir, dry_run=True
        )
        assert count == 0

    def test_single_lockdown_event_processed(self, chitin_dir, chain_conn, findings_conn):
        """A single lockdown_loop_detected event is processed and persisted."""
        rowid = insert_lockdown_event(chain_conn, agent="hermes-agent-1", lockdown_count=3)

        count = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir
        )
        assert count == 1

        # Verify state was updated
        last_rowid = canary_consumer._get_last_rowid(chain_conn, canary_consumer.CONSUMER_ID)
        assert last_rowid == rowid

    def test_non_lockdown_events_ignored(self, chitin_dir, chain_conn, findings_conn):
        """Events with other event_type are ignored."""
        insert_other_event(chain_conn, "tool_call")
        insert_other_event(chain_conn, "gate_evaluate")
        insert_lockdown_event(chain_conn, agent="agent-1", lockdown_count=1)

        count = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir
        )
        assert count == 1  # Only the lockdown event

    def test_multiple_lockdown_events_processed(self, chitin_dir, chain_conn, findings_conn):
        """Multiple lockdown events are all processed."""
        insert_lockdown_event(chain_conn, agent="agent-1", lockdown_count=2)
        insert_lockdown_event(chain_conn, agent="agent-2", lockdown_count=5)
        insert_other_event(chain_conn, "tool_call")

        count = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir
        )
        assert count == 2

    def test_dedup_across_runs(self, chitin_dir, chain_conn, findings_conn):
        """Events already processed are not re-fired on subsequent runs."""
        insert_lockdown_event(chain_conn, agent="agent-1", lockdown_count=3)
        count1 = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir
        )
        assert count1 == 1

        # Run again — no new events
        count2 = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir
        )
        assert count2 == 0

        # Add another event
        insert_lockdown_event(chain_conn, agent="agent-2", lockdown_count=7)
        count3 = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir
        )
        assert count3 == 1

    def test_dry_run_does_not_persist(self, chitin_dir, chain_conn, findings_conn):
        """Dry run logs events but doesn't persist findings."""
        insert_lockdown_event(chain_conn, agent="dry-agent", lockdown_count=1)

        count = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir, dry_run=True
        )
        assert count == 1

        # Verify nothing was inserted into findings
        rows = findings_conn.execute("SELECT COUNT(*) FROM findings").fetchone()
        assert rows[0] == 0

    def test_persist_creates_findings_rows(self, chitin_dir, chain_conn, findings_conn):
        """Persisted events appear in the findings table."""
        insert_lockdown_event(chain_conn, agent="persist-agent", lockdown_count=2)

        canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir
        )

        rows = findings_conn.execute("SELECT detector, severity, title FROM findings").fetchall()
        assert len(rows) >= 1
        assert rows[0][0] == "canary_lockdown_loop_detected"
        assert rows[0][1] == "critical"
        assert "persist-agent" in rows[0][2]


# ---------------------------------------------------------------------------
# Tests: JSONL fallback
# ---------------------------------------------------------------------------


class TestProcessEventJSONLFallback:
    """Test process_events with JSONL file fallback."""

    def test_jsonl_fallback_finds_events(self, chitin_dir, chain_conn, findings_conn):
        """When session_events doesn't have the event, JSONL files are scanned."""
        # Create a JSONL event file
        jsonl_path = chitin_dir / "events-4f99efb8.jsonl"
        events = [
            {"event_type": "tool_call", "ts": "2026-05-20T12:00:00Z", "agent_instance_id": "x"},
            {
                "event_type": "lockdown_loop_detected",
                "ts": "2026-05-20T12:01:00Z",
                "payload": {
                    "agent": "jsonl-agent",
                    "rationale": "loop detected",
                    "lockdown_count": 4,
                    "window_sec": 120,
                    "action": "deny",
                    "threshold": 3,
                },
            },
            {"event_type": "gate_evaluate", "ts": "2026-05-20T12:02:00Z", "agent_instance_id": "y"},
        ]
        with open(jsonl_path, "w") as f:
            for evt in events:
                f.write(json.dumps(evt) + "\n")

        # Drop session_events table to force JSONL fallback
        chain_conn.execute("DROP TABLE session_events")
        chain_conn.commit()

        # Re-create state table (since _ensure_state_table is called in process_events)
        canary_consumer._ensure_state_table(chain_conn)

        count = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir
        )
        assert count == 1

    def test_jsonl_dedup_across_runs(self, chitin_dir, chain_conn, findings_conn):
        """JSONL events are not re-processed on subsequent runs."""
        jsonl_path = chitin_dir / "events-test-dedup.jsonl"
        events = [
            {
                "event_type": "lockdown_loop_detected",
                "ts": "2026-05-20T12:00:00Z",
                "payload": {
                    "agent": "dedup-agent",
                    "rationale": "test",
                    "lockdown_count": 1,
                    "window_sec": 30,
                    "action": "deny",
                    "threshold": 5,
                },
            },
        ]
        with open(jsonl_path, "w") as f:
            for evt in events:
                f.write(json.dumps(evt) + "\n")

        # Drop session_events to force JSONL path
        chain_conn.execute("DROP TABLE session_events")
        chain_conn.commit()
        canary_consumer._ensure_state_table(chain_conn)

        # First run
        count1 = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir
        )
        assert count1 == 1

        # Second run — same file, same events
        count2 = canary_consumer.process_events(
            chain_conn, findings_conn, chitin_dir
        )
        assert count2 == 0


# ---------------------------------------------------------------------------
# Tests: state tracking
# ---------------------------------------------------------------------------


class TestStateTracking:
    """Test canary_consumer_state table management."""

    def test_ensure_state_table_creates_table(self, chain_conn):
        """_ensure_state_table creates the table if it doesn't exist."""
        canary_consumer._ensure_state_table(chain_conn)
        rows = chain_conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name='canary_consumer_state'"
        ).fetchall()
        assert len(rows) == 1

    def test_get_last_rowid_default(self, chain_conn):
        """Default last_rowid is 0 when no state exists."""
        canary_consumer._ensure_state_table(chain_conn)
        last = canary_consumer._get_last_rowid(chain_conn, canary_consumer.CONSUMER_ID)
        assert last == 0

    def test_update_last_rowid_persists(self, chitin_dir, chain_conn, findings_conn):
        """After processing, last_rowid is persisted correctly."""
        rowid = insert_lockdown_event(chain_conn, agent="track-agent", lockdown_count=6)
        canary_consumer.process_events(chain_conn, findings_conn, chitin_dir)

        last = canary_consumer._get_last_rowid(chain_conn, canary_consumer.CONSUMER_ID)
        assert last == rowid


# ---------------------------------------------------------------------------
# Tests: _make_finding
# ---------------------------------------------------------------------------


class TestMakeFinding:
    """Test _make_finding event-to-Finding conversion."""

    def test_basic_finding_creation(self):
        """_make_finding creates a Finding with correct fields."""
        event = {
            "source": "session_events:42",
            "agent": "test-agent",
            "reason": "loop detected",
            "ts": "2026-05-20T12:00:00+00:00",
            "lockdown_count": 5,
            "window_sec": 60,
            "action": "deny",
            "threshold": 3,
            "payload": {"extra_key": "extra_val"},
        }
        finding = canary_consumer._make_finding(event)
        assert finding.detector == "canary_lockdown_loop_detected"
        assert finding.severity == "critical"
        assert "test-agent" in finding.title
        assert finding.details["lockdown_count"] == 5
        assert finding.details["window_sec"] == 60
        assert finding.details["source"] == "session_events:42"
        assert finding.details["extra_key"] == "extra_val"

    def test_finding_with_empty_fields(self):
        """_make_finding handles events with missing optional fields."""
        event = {
            "source": "jsonl:events-test.jsonl:0",
            "agent": "",
            "reason": "",
            "ts": "",
            "lockdown_count": 0,
            "window_sec": 0,
            "action": "",
            "threshold": 0,
            "payload": {},
        }
        finding = canary_consumer._make_finding(event)
        assert finding.detector == "canary_lockdown_loop_detected"
        # Default agent should be "unknown" when empty
        assert finding.details["agent"] == "unknown"


# ---------------------------------------------------------------------------
# Tests: main() CLI
# ---------------------------------------------------------------------------


class TestMainCLI:
    """Test command-line interface."""

    def test_once_mode(self, chitin_dir):
        """--once processes events and exits."""
        chain_c = sqlite3.connect(str(chitin_dir / "chain_index.sqlite"))
        insert_lockdown_event(chain_c, agent="cli-agent", lockdown_count=1)
        chain_c.close()

        with patch("sys.argv", ["canary-consumer", "--once", "--chitin-dir", str(chitin_dir)]):
            rc = canary_consumer.main()
        assert rc == 0

    def test_missing_chain_db_exits_with_error(self, tmp_path):
        """--once with non-existent chain_index returns 1."""
        fake_dir = tmp_path / "nonexistent"
        fake_dir.mkdir()
        with patch("sys.argv", ["canary-consumer", "--once", "--chitin-dir", str(fake_dir)]):
            rc = canary_consumer.main()
        assert rc == 1