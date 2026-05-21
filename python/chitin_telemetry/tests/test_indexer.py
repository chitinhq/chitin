"""Tests for chitin_telemetry.indexer with boundary conditions."""
import json
import sqlite3
import tempfile
from datetime import datetime
from pathlib import Path

import pytest

from chitin_telemetry.indexer import (
    init_db,
    index_jsonl_file,
    index_jsonl_file_from_offset,
    _line_hash,
    _parse_ts_unix,
)


class TestLineHash:
    """Test deterministic line hashing for idempotency."""

    def test_same_line_same_hash(self):
        """Same input line produces same hash."""
        line = '{"ts":"2026-05-13T08:00:00Z","allowed":false,"rule_id":"test"}'
        h1 = _line_hash(line)
        h2 = _line_hash(line)
        assert h1 == h2

    def test_different_line_different_hash(self):
        """Different lines produce different hashes."""
        line1 = '{"ts":"2026-05-13T08:00:00Z","allowed":false}'
        line2 = '{"ts":"2026-05-13T08:00:01Z","allowed":true}'
        assert _line_hash(line1) != _line_hash(line2)


class TestParseTsUnix:
    """Test timestamp parsing."""

    def test_valid_iso8601(self):
        """Parse valid ISO 8601 timestamp."""
        ts = "2026-05-13T08:00:00Z"
        unix = _parse_ts_unix(ts)
        assert isinstance(unix, int)
        assert unix > 0

    def test_invalid_timestamp_returns_none(self):
        """Invalid timestamp returns None."""
        assert _parse_ts_unix("bad-timestamp") is None
        assert _parse_ts_unix("") is None
        assert _parse_ts_unix(None) is None


class TestInitDb:
    """Test database initialization."""

    def test_init_db_creates_schema(self):
        """Init creates tables and indexes."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            cursor = conn.execute(
                "SELECT name FROM sqlite_master WHERE type='table'"
            )
            tables = {row[0] for row in cursor.fetchall()}
            assert "events" in tables

            cursor = conn.execute(
                "SELECT name FROM sqlite_master WHERE type='index' AND name LIKE 'idx_%'"
            )
            indexes = {row[0] for row in cursor.fetchall()}
            assert "idx_ts_unix" in indexes
            assert "idx_rule_id" in indexes

            conn.close()

    def test_init_db_idempotent(self):
        """Init is idempotent — can call multiple times."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn1 = init_db(db_path)
            conn1.close()

            conn2 = init_db(db_path)
            cursor = conn2.execute("SELECT COUNT(*) FROM events")
            assert cursor.fetchone()[0] == 0
            conn2.close()


class TestIndexJsonlFile:
    """Test JSONL file indexing with boundary conditions."""

    def test_index_empty_file(self):
        """Empty file produces no errors."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            file_path = Path(tmpdir) / "empty.jsonl"
            file_path.write_text("")

            conn = init_db(db_path)
            inserted, skipped = index_jsonl_file(conn, file_path)
            conn.close()

            assert inserted == 0
            assert skipped == 0

    def test_index_single_event(self):
        """Single event is indexed without divide-by-zero."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            file_path = Path(tmpdir) / "single.jsonl"
            file_path.write_text(
                '{"ts":"2026-05-13T08:00:00Z","allowed":false,"rule_id":"test"}\n'
            )

            conn = init_db(db_path)
            inserted, skipped = index_jsonl_file(conn, file_path)

            assert inserted == 1
            assert skipped == 0

            # Verify it's in the database
            cursor = conn.execute("SELECT COUNT(*) FROM events")
            assert cursor.fetchone()[0] == 1
            conn.close()

    def test_index_malformed_jsonl_line_skipped(self):
        """Malformed JSON lines are skipped, never block."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            file_path = Path(tmpdir) / "mixed.jsonl"
            file_path.write_text(
                'not valid json\n'
                '{"ts":"2026-05-13T08:00:00Z","allowed":false,"rule_id":"test1"}\n'
                '{"ts":"bad-ts","allowed":false}\n'
                '{"ts":"2026-05-13T08:00:01Z","allowed":true,"rule_id":"test2"}\n'
            )

            conn = init_db(db_path)
            inserted, skipped = index_jsonl_file(conn, file_path)

            # 2 good lines indexed, malformed ones skipped
            assert inserted == 2
            conn.close()

    def test_index_duplicate_replay_idempotent(self):
        """Indexing same file twice is idempotent via line_hash."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            file_path = Path(tmpdir) / "replay.jsonl"
            content = (
                '{"ts":"2026-05-13T08:00:00Z","allowed":false,"rule_id":"test1"}\n'
                '{"ts":"2026-05-13T08:00:01Z","allowed":true,"rule_id":"test2"}\n'
            )
            file_path.write_text(content)

            conn = init_db(db_path)

            # First index
            inserted1, skipped1 = index_jsonl_file(conn, file_path)
            assert inserted1 == 2
            assert skipped1 == 0

            # Second index (replay)
            inserted2, skipped2 = index_jsonl_file(conn, file_path)
            assert inserted2 == 0
            assert skipped2 == 2

            # Database still has exactly 2 rows
            cursor = conn.execute("SELECT COUNT(*) FROM events")
            assert cursor.fetchone()[0] == 2
            conn.close()

    def test_index_nonexistent_file_returns_zeros(self):
        """Indexing nonexistent file returns (0, 0) gracefully."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)
            inserted, skipped = index_jsonl_file(conn, Path("/nonexistent/file.jsonl"))

            assert inserted == 0
            assert skipped == 0
            conn.close()

    def test_index_preserves_all_fields(self):
        """All decision fields are indexed correctly."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            file_path = Path(tmpdir) / "full.jsonl"
            event = {
                "ts": "2026-05-13T08:00:00Z",
                "allowed": False,
                "mode": "enforce",
                "rule_id": "test-rule",
                "reason": "test-reason",
                "escalation": "elevated",
                "agent": "claude-code",
                "action_type": "shell.exec",
                "action_target": "rm -rf /tmp",
                "envelope_id": "env_123",
                "tier": "5",
                "cost_usd": 0.05,
                "input_bytes": 100,
                "tool_calls": 2,
                "model": "claude-opus",
                "role": "programmer",
                "workflow_id": "wf_456",
                "fingerprint": "fp_789",
            }
            file_path.write_text(json.dumps(event) + "\n")

            conn = init_db(db_path)
            conn.row_factory = sqlite3.Row
            inserted, skipped = index_jsonl_file(conn, file_path)
            assert inserted == 1

            # Verify all fields
            cursor = conn.execute("SELECT * FROM events")
            row = cursor.fetchone()
            assert row["allowed"] == 0
            assert row["rule_id"] == "test-rule"
            assert row["agent"] == "claude-code"
            assert row["cost_usd"] == 0.05
            assert row["model"] == "claude-opus"
            conn.close()


def test_index_jsonl_from_offset_indexes_appended_lines_and_rollover():
    """Follower primitive indexes appended lines and newly discovered date-rollover files."""
    from chitin_telemetry.indexer import _decision_files, index_jsonl_file_from_offset

    with tempfile.TemporaryDirectory() as tmpdir:
        root = Path(tmpdir)
        db_path = root / "test.db"
        decisions = root / "decisions"
        decisions.mkdir()
        day1 = decisions / "gov-decisions-2026-05-13.jsonl"
        day2 = decisions / "gov-decisions-2026-05-14.jsonl"
        day1.write_text('{"ts":"2026-05-13T08:00:00Z","allowed":false,"rule_id":"r1"}\n')

        conn = init_db(db_path)
        offsets = {}
        for f in _decision_files(decisions):
            _, _, offsets[f] = index_jsonl_file_from_offset(conn, f, offsets.get(f, 0))

        day1.write_text(day1.read_text() + '{"ts":"2026-05-13T08:00:01Z","allowed":true,"rule_id":"r2"}\n')
        day2.write_text('{"ts":"2026-05-14T00:00:00Z","allowed":false,"rule_id":"r3"}\n')
        for f in _decision_files(decisions):
            _, _, offsets[f] = index_jsonl_file_from_offset(conn, f, offsets.get(f, 0))

        assert conn.execute("SELECT COUNT(*) FROM events").fetchone()[0] == 3
        conn.close()


def test_index_jsonl_from_offset_waits_for_newline_before_processing():
    with tempfile.TemporaryDirectory() as tmpdir:
        root = Path(tmpdir)
        db_path = root / "test.db"
        file_path = root / "gov-decisions-2026-05-13.jsonl"
        file_path.write_text('{"ts":"2026-05-13T08:00:00Z","allowed":false,"rule_id":"r1"}')

        conn = init_db(db_path)
        inserted, skipped, next_offset = index_jsonl_file_from_offset(conn, file_path, 0)
        assert inserted == 0
        assert skipped == 0
        assert next_offset == 0

        file_path.write_text(file_path.read_text() + "\n")
        inserted, skipped, next_offset = index_jsonl_file_from_offset(conn, file_path, next_offset)
        assert inserted == 1
        assert conn.execute("SELECT COUNT(*) FROM events").fetchone()[0] == 1
        assert next_offset == file_path.stat().st_size
        conn.close()
