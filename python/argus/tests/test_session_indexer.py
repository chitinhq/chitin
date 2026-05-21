"""Tests for argus.session_indexer with boundary conditions."""
import json
import os
import sqlite3
import tempfile
import time
from pathlib import Path

import pytest

from argus.session_indexer import (
    _CC_SESSION_RE,
    _CODEX_EVENT_RE,
    _CHAIN_EVENT_RE,
    _line_hash,
    _parse_ts_unix,
    _should_reindex,
    _source_file_key,
    discover_claude_code_files,
    discover_codex_files,
    discover_chain_events_files,
    index_claude_code_file,
    index_codex_file,
    init_session_db,
    bulk_index,
    parse_codex_line,
    parse_claude_code_line,
)


@pytest.fixture
def db_path(tmp_path):
    return tmp_path / "chain_index.sqlite"


@pytest.fixture
def db(db_path):
    conn = init_session_db(db_path)
    yield conn
    conn.close()


# ---------------------------------------------------------------------------
# Schema tests
# ---------------------------------------------------------------------------

class TestSchema:
    def test_init_creates_tables(self, db):
        tables = {row[0] for row in db.execute(
            "SELECT name FROM sqlite_master WHERE type='table'"
        ).fetchall()}
        assert "session_events" in tables
        assert "session_index_checkpoints" in tables

    def test_init_idempotent(self, db_path):
        conn1 = init_session_db(db_path)
        conn1.close()
        conn2 = init_session_db(db_path)
        conn2.close()
        # No error — schema is idempotent

    def test_driver_type_column_added_to_existing_chains(self, db_path):
        # Simulate a pre-existing chains table (as in production)
        conn = sqlite3.connect(str(db_path))
        conn.execute("CREATE TABLE IF NOT EXISTS chains (chain_id TEXT PRIMARY KEY, last_seq INTEGER)")
        conn.commit()
        conn.close()
        # Re-run init to apply migrations
        conn2 = init_session_db(db_path)
        # Should not raise — driver_type column was added
        conn2.execute("SELECT driver_type FROM chains LIMIT 1")
        conn2.close()

    def test_session_events_has_all_columns(self, db):
        cursor = db.execute("PRAGMA table_info(session_events)")
        cols = {row[1] for row in cursor.fetchall()}
        expected = {
            "id", "line_hash", "driver_type", "chain_id", "session_id",
            "event_type", "ts", "ts_unix", "agent", "surface",
            "action_type", "action_target", "tool_name", "decision",
            "rule_id", "reason", "escalation", "mode", "cwd",
            "model", "role", "fingerprint", "workflow_id", "envelope_id",
            "payload_json", "source_file", "source_line", "last_seen_ts",
        }
        assert expected <= cols

    def test_checkpoints_has_all_columns(self, db):
        cursor = db.execute("PRAGMA table_info(session_index_checkpoints)")
        cols = {row[1] for row in cursor.fetchall()}
        expected = {
            "source_key", "file_path", "offset", "file_mtime",
            "file_size", "inode", "updated_at",
        }
        assert expected <= cols

    def test_line_hash_unique_constraint(self, db):
        db.execute(
            "INSERT INTO session_events (line_hash, driver_type, chain_id, session_id, event_type, ts, source_file) "
            "VALUES ('hash1', 'codex', 'c1', 's1', 'decision', '2026-01-01T00:00:00Z', '/tmp/test.jsonl')"
        )
        db.commit()
        with pytest.raises(sqlite3.IntegrityError):
            db.execute(
                "INSERT INTO session_events (line_hash, driver_type, chain_id, session_id, event_type, ts, source_file) "
                "VALUES ('hash1', 'codex', 'c1', 's1', 'decision', '2026-01-01T00:00:00Z', '/tmp/test.jsonl')"
            )


# ---------------------------------------------------------------------------
# Parser tests
# ---------------------------------------------------------------------------

class TestParseTsUnix:
    def test_rfc3339_zulu(self):
        assert _parse_ts_unix("2026-05-13T14:12:12Z") > 0

    def test_rfc3339_millis(self):
        assert _parse_ts_unix("2026-05-13T14:12:12.994Z") > 0

    def test_rfc3339_offset(self):
        assert _parse_ts_unix("2026-05-13T14:12:12+00:00") > 0

    def test_empty(self):
        assert _parse_ts_unix("") == 0

    def test_invalid(self):
        assert _parse_ts_unix("not-a-date") == 0


class TestParseCodexLine:
    def test_decision_event(self):
        raw = json.dumps({
            "ts": "2026-05-13T14:12:21.185Z",
            "chain_id": "019e21ae-42fa-73a2-846d-675876b04dfa",
            "event_type": "decision",
            "payload": {
                "tool_name": "exec_command",
                "action_type": "shell.exec",
                "action_target": "ls -la",
                "decision": "allow",
                "rule_id": "allow-shell",
            }
        })
        result = parse_codex_line(raw)
        assert result is not None
        assert result["event_type"] == "decision"
        assert result["action_type"] == "shell.exec"
        assert result["decision"] == "allow"
        # driver_type is set by index_codex_file, not parse_codex_line
        assert "driver_type" not in result
        assert result["chain_id"] == "019e21ae-42fa-73a2-846d-675876b04dfa"

    def test_task_start_event(self):
        raw = json.dumps({
            "ts": "2026-05-13T14:12:12.994Z",
            "chain_id": "test-chain-id",
            "event_type": "task_start",
            "payload": {
                "tool_name": "codex.session_start",
                "action_type": "delegate.task",
                "decision": "allow",
                "rule_id": "codex-post-hoc",
            }
        })
        result = parse_codex_line(raw)
        assert result is not None
        assert result["event_type"] == "task_start"

    def test_event_with_labels(self):
        raw = json.dumps({
            "ts": "2026-05-13T14:12:12.994Z",
            "chain_id": "chain-1",
            "event_type": "decision",
            "session_id": "sess-1",
            "surface": "codex",
            "labels": {"agent": "test-agent", "role": "external"},
            "payload": {"decision": "allow", "rule_id": "r1"}
        })
        result = parse_codex_line(raw)
        assert result is not None
        assert result["agent"] == "test-agent"
        assert result["surface"] == "codex"

    def test_invalid_json(self):
        assert parse_codex_line("not json{{{") is None

    def test_empty_line(self):
        assert parse_codex_line("") is None

    def test_missing_chain_id_and_event_type(self):
        raw = json.dumps({"ts": "2026-01-01T00:00:00Z"})
        result = parse_codex_line(raw)
        assert result is None  # no chain_id or event_type


class TestParseClaudeCodeLine:
    def test_assistant_with_tool_use(self):
        raw = json.dumps({
            "type": "assistant",
            "timestamp": "2026-05-04T12:18:51.261Z",
            "sessionId": "fdabfbca-c66d-40a3-a2fc-adc5223c9cf8",
            "message": {
                "role": "assistant",
                "content": [
                    {"type": "tool_use", "name": "Bash", "id": "tu-1", "input": {"command": "ls -la"}},
                ],
            },
        })
        results = parse_claude_code_line(raw)
        assert len(results) == 1
        r = results[0]
        assert r["event_type"] == "tool_call"
        # driver_type is set by index_claude_code_file, not parse_claude_code_line
        assert "driver_type" not in r
        assert r["tool_name"] == "Bash"
        assert r["action_type"] == "shell.exec"
        assert r["action_target"] == "ls -la"
        assert r["chain_id"] == "fdabfbca-c66d-40a3-a2fc-adc5223c9cf8"

    def test_user_message(self):
        raw = json.dumps({
            "type": "user",
            "timestamp": "2026-05-04T12:18:51.261Z",
            "sessionId": "test-session-id",
            "message": {
                "role": "user",
                "content": "Hello, please help me.",
            },
        })
        results = parse_claude_code_line(raw)
        assert len(results) == 1
        r = results[0]
        assert r["event_type"] == "user_input"
        assert r["role"] == "user"
        assert r["chain_id"] == "test-session-id"

    def test_queue_operation_skipped(self):
        raw = json.dumps({
            "type": "queue-operation",
            "operation": "enqueue",
            "timestamp": "2026-05-04T12:18:51.261Z",
            "sessionId": "test-session",
        })
        results = parse_claude_code_line(raw)
        assert len(results) == 0

    def test_file_session_id_fallback(self):
        raw = json.dumps({
            "type": "user",
            "timestamp": "2026-05-04T12:00:00Z",
            "message": {"role": "user", "content": "test"},
        })
        results = parse_claude_code_line(raw, file_session_id="from-filename-uuid")
        assert results[0]["chain_id"] == "from-filename-uuid"


# ---------------------------------------------------------------------------
# Discovery tests
# ---------------------------------------------------------------------------

class TestDiscovery:
    def test_codex_event_filename_match(self):
        assert _CODEX_EVENT_RE.match("codex-events-019e21ae-42fa-73a2-846d-675876b04dfa.jsonl")
        assert not _CODEX_EVENT_RE.match("events-019e21ae-42fa-73a2-846d-675876b04dfa.jsonl")
        assert not _CODEX_EVENT_RE.match("random.jsonl")

    def test_chain_event_filename_match(self):
        assert _CHAIN_EVENT_RE.match("events-019e21ae-42fa-73a2-846d-675876b04dfa.jsonl")
        assert not _CHAIN_EVENT_RE.match("codex-events-019e21ae-42fa-73a2-846d-675876b04dfa.jsonl")

    def test_cc_session_filename_match(self):
        assert _CC_SESSION_RE.match("fdabfbca-c66d-40a3-a2fc-adc5223c9cf8.jsonl")
        assert not _CC_SESSION_RE.match("codex-events-xxx.jsonl")

    def test_discover_codex_files(self, tmp_path):
        chitin_dir = tmp_path / ".chitin"
        chitin_dir.mkdir()
        (chitin_dir / "codex-events-019e21ae-42fa-73a2-846d-675876b04dfa.jsonl").touch()
        (chitin_dir / "events-7c1786d4-0c6f-44c9-9984-3796686b7836.jsonl").touch()
        (chitin_dir / "other-file.txt").touch()
        files = discover_codex_files(chitin_dir)
        assert len(files) == 1
        assert files[0].name.startswith("codex-events-")

    def test_discover_claude_code_files(self, tmp_path):
        claude_dir = tmp_path / "projects" / "my-project"
        claude_dir.mkdir(parents=True)
        (claude_dir / "fdabfbca-c66d-40a3-a2fc-adc5223c9cf8.jsonl").touch()
        (claude_dir / "other.dat").touch()
        files = discover_claude_code_files(tmp_path / "projects")
        assert len(files) == 1
        assert files[0].name.endswith(".jsonl")


# ---------------------------------------------------------------------------
# Indexing tests
# ---------------------------------------------------------------------------

class TestIndexCodexFile:
    def test_basic_indexing(self, db, tmp_path):
        # Create a codex events file
        events = [
            {"ts": "2026-05-13T14:12:12.994Z", "chain_id": "c1", "event_type": "decision",
             "payload": {"tool_name": "exec_command", "action_type": "shell.exec",
                         "action_target": "ls", "decision": "allow", "rule_id": "r1"}},
            {"ts": "2026-05-13T14:12:13.185Z", "chain_id": "c1", "event_type": "decision",
             "payload": {"tool_name": "file_read", "action_type": "file.read",
                         "action_target": "test.py", "decision": "allow", "rule_id": "r2"}},
        ]
        f = tmp_path / "codex-events-019e21ae-42fa-73a2-846d-675876b04dfa.jsonl"
        with open(f, "w") as fh:
            for ev in events:
                fh.write(json.dumps(ev) + "\n")

        ins, skip, off = index_codex_file(db, f)
        assert ins == 2
        assert skip == 0
        assert off > 0

        # Verify in DB
        rows = db.execute("SELECT COUNT(*) FROM session_events WHERE driver_type = 'codex'").fetchone()
        assert rows[0] == 2

    def test_idempotent_reindex(self, db, tmp_path):
        """Re-processing a file must not duplicate rows."""
        events = [
            {"ts": "2026-05-13T14:12:12.994Z", "chain_id": "c1", "event_type": "decision",
             "payload": {"tool_name": "exec", "action_type": "shell.exec",
                         "action_target": "ls", "decision": "allow", "rule_id": "r1"}},
        ]
        f = tmp_path / "codex-events-test.jsonl"
        with open(f, "w") as fh:
            for ev in events:
                fh.write(json.dumps(ev) + "\n")

        ins1, _, _ = index_codex_file(db, f)
        assert ins1 == 1

        # Reindex from the beginning
        ins2, skip, _ = index_codex_file(db, f, offset=0)
        assert ins2 == 0
        assert skip == 1

    def test_incremental_append(self, db, tmp_path):
        """Appending to a file and reindexing should only index new lines."""
        f = tmp_path / "codex-events-test.jsonl"
        ev1 = {"ts": "2026-05-13T14:12:12Z", "chain_id": "c1", "event_type": "decision",
               "payload": {"tool_name": "bash", "action_type": "shell.exec",
                           "action_target": "echo 1", "decision": "allow", "rule_id": "r1"}}
        with open(f, "w") as fh:
            fh.write(json.dumps(ev1) + "\n")

        ins1, _, off1 = index_codex_file(db, f)
        assert ins1 == 1

        ev2 = {"ts": "2026-05-13T14:12:13Z", "chain_id": "c1", "event_type": "decision",
               "payload": {"tool_name": "bash", "action_type": "shell.exec",
                           "action_target": "echo 2", "decision": "allow", "rule_id": "r2"}}
        with open(f, "a") as fh:
            fh.write(json.dumps(ev2) + "\n")

        ins2, _, off2 = index_codex_file(db, f, offset=off1)
        assert ins2 == 1

        rows = db.execute("SELECT action_target FROM session_events ORDER BY ts_unix").fetchall()
        assert len(rows) == 2
        assert rows[0][0] == "echo 1"
        assert rows[1][0] == "echo 2"

    def test_malformed_lines_skipped(self, db, tmp_path):
        f = tmp_path / "codex-events-test.jsonl"
        with open(f, "w") as fh:
            fh.write("not json{{{  \n")
            fh.write(json.dumps({"ts": "2026-05-13T14:12:12Z", "chain_id": "c1",
                                 "event_type": "decision",
                                 "payload": {"decision": "allow", "rule_id": "r1"}}) + "\n")
            fh.write("\n")  # empty line

        ins, skip, _ = index_codex_file(db, f)
        assert ins == 1
        assert skip == 0  # malformed lines are silently skipped, not counted as duplicates

    def test_duplicate_insert_skipped_on_reread(self, db, tmp_path):
        """Re-reading same lines from offset=0 must skip IntegrityErrors."""
        f = tmp_path / "codex-events-dedup.jsonl"
        ev = {"ts": "2026-05-13T14:12:12Z", "chain_id": "c1", "event_type": "decision",
              "payload": {"tool_name": "bash", "action_type": "shell.exec",
                          "action_target": "dedup-test", "decision": "allow", "rule_id": "r1"}}
        with open(f, "w") as fh:
            fh.write(json.dumps(ev) + "\n")

        ins1, skip1, _ = index_codex_file(db, f)
        assert ins1 == 1
        assert skip1 == 0

        # Force re-read from offset=0 (simulates reindex after truncation+rewrite)
        ins2, skip2, _ = index_codex_file(db, f, offset=0)
        assert ins2 == 0
        assert skip2 == 1


class TestIndexClaudeCodeFile:
    def test_basic_claude_code_indexing(self, db, tmp_path):
        f = tmp_path / "fdabfbca-c66d-40a3-a2fc-adc5223c9cf8.jsonl"
        lines = [
            json.dumps({
                "type": "assistant",
                "timestamp": "2026-05-04T12:18:51.261Z",
                "sessionId": "fdabfbca-c66d-40a3-a2fc-adc5223c9cf8",
                "message": {
                    "role": "assistant",
                    "content": [{"type": "tool_use", "name": "Bash", "id": "tu-1",
                                 "input": {"command": "git status"}}],
                },
            }),
            json.dumps({
                "type": "user",
                "timestamp": "2026-05-04T12:19:00Z",
                "sessionId": "fdabfbca-c66d-40a3-a2fc-adc5223c9cf8",
                "message": {"role": "user", "content": "Thanks!"},
            }),
        ]
        with open(f, "w") as fh:
            for line in lines:
                fh.write(line + "\n")

        ins, skip, off = index_claude_code_file(db, f)
        assert ins == 2  # 1 tool_call + 1 user_input
        assert skip == 0

        rows = db.execute(
            "SELECT event_type, driver_type FROM session_events WHERE driver_type = 'claude-code'"
        ).fetchall()
        event_types = {row[0] for row in rows}
        assert "tool_call" in event_types
        assert "user_input" in event_types

    def test_tool_name_mapping(self, db, tmp_path):
        f = tmp_path / "test-session-uuid.jsonl"
        line = json.dumps({
            "type": "assistant",
            "timestamp": "2026-05-04T12:00:00Z",
            "sessionId": "s1",
            "message": {
                "role": "assistant",
                "content": [
                    {"type": "tool_use", "name": "Edit", "id": "tu-1",
                     "input": {"file_path": "/tmp/test.py", "old_string": "x", "new_string": "y"}},
                    {"type": "tool_use", "name": "Read", "id": "tu-2",
                     "input": {"file_path": "/tmp/test.py"}},
                ],
            },
        })
        with open(f, "w") as fh:
            fh.write(line + "\n")

        index_claude_code_file(db, f)
        rows = db.execute(
            "SELECT tool_name, action_type, action_target FROM session_events WHERE driver_type = 'claude-code'"
        ).fetchall()
        assert len(rows) == 2
        tool_map = {r[0]: (r[1], r[2]) for r in rows}
        assert tool_map["Edit"] == ("file.write", "/tmp/test.py")
        assert tool_map["Read"] == ("file.read", "/tmp/test.py")


# ---------------------------------------------------------------------------
# Checkpoint tests
# ---------------------------------------------------------------------------

class TestCheckpoints:
    def test_save_and_load_checkpoint(self, db, tmp_path):
        from argus.session_indexer import _save_checkpoint, _get_checkpoint
        key = "codex:/tmp/test.jsonl"
        _save_checkpoint(db, key, "/tmp/test.jsonl", 100, 1700000000.0, 200, 12345)
        cp = _get_checkpoint(db, key)
        assert cp is not None
        assert cp["offset"] == 100
        assert cp["file_mtime"] == 1700000000.0

    def test_should_reindex_new_file(self, tmp_path):
        f = tmp_path / "new.jsonl"
        f.touch()
        assert _should_reindex(f, None) is True

    def test_should_reindex_truncated(self, tmp_path):
        f = tmp_path / "truncated.jsonl"
        f.write_text("x" * 500)
        checkpoint = {"offset": 1000, "file_mtime": 0, "file_size": 1000, "inode": 0}
        assert _should_reindex(f, checkpoint) is True


# ---------------------------------------------------------------------------
# Bulk index tests
# ---------------------------------------------------------------------------

class TestBulkIndex:
    def test_bulk_index_codex(self, db, tmp_path):
        chitin_dir = tmp_path / ".chitin"
        chitin_dir.mkdir()
        claude_dir = tmp_path / ".claude" / "projects"
        claude_dir.mkdir(parents=True)

        # Create a codex events file
        ev = {"ts": "2026-05-13T14:12:12Z", "chain_id": "bulk-test", "event_type": "decision",
              "payload": {"tool_name": "bash", "action_type": "shell.exec",
                          "action_target": "echo", "decision": "allow", "rule_id": "r1"}}
        f = chitin_dir / "codex-events-019e21ae-42fa-73a2-846d-675876b04dfa.jsonl"
        with open(f, "w") as fh:
            fh.write(json.dumps(ev) + "\n")

        # Use the db connection we already initialized
        from argus.session_indexer import _save_checkpoint, _file_fingerprint
        # Need to re-init with the same path
        db2 = init_session_db(tmp_path / "test_db.sqlite")
        try:
            stats = bulk_index(db2, chitin_dir, claude_dir)
            assert stats["codex_files"] == 1
            assert stats["codex_inserted"] >= 1
        finally:
            db2.close()

    def test_bulk_index_idempotent(self, tmp_path):
        chitin_dir = tmp_path / ".chitin"
        chitin_dir.mkdir()
        claude_dir = tmp_path / ".claude" / "projects"
        claude_dir.mkdir(parents=True)

        ev = {"ts": "2026-05-13T14:12:12Z", "chain_id": "idem-test", "event_type": "decision",
              "payload": {"tool_name": "bash", "action_type": "shell.exec",
                          "action_target": "echo", "decision": "allow", "rule_id": "r1"}}
        f = chitin_dir / "codex-events-019e21ae-42fa-73a2-846d-675876b04dfa.jsonl"
        with open(f, "w") as fh:
            fh.write(json.dumps(ev) + "\n")

        db = init_session_db(tmp_path / "test_idem.sqlite")
        try:
            stats1 = bulk_index(db, chitin_dir, claude_dir)
            stats2 = bulk_index(db, chitin_dir, claude_dir)
            assert stats1["codex_inserted"] >= 1
            # Second run: file offset already at end, so 0 inserted and 0 skipped
            # (duplicates are only counted as skipped when we try to INSERT)
            assert stats2["codex_inserted"] == 0
        finally:
            db.close()


# ---------------------------------------------------------------------------
# Regex tests
# ---------------------------------------------------------------------------

class TestRegexPatterns:
    def test_codex_pattern_valid(self):
        assert _CODEX_EVENT_RE.match(
            "codex-events-019e21ae-42fa-73a2-846d-675876b04dfa.jsonl"
        )

    def test_events_pattern_valid(self):
        assert _CHAIN_EVENT_RE.match(
            "events-7c1786d4-0c6f-44c9-9984-3796686b7836.jsonl"
        )

    def test_cc_session_pattern_valid(self):
        assert _CC_SESSION_RE.match(
            "fdabfbca-c66d-40a3-a2fc-adc5223c9cf8.jsonl"
        )

    def test_wrong_patterns_rejected(self):
        assert not _CODEX_EVENT_RE.match("events-xxx.jsonl")
        assert not _CHAIN_EVENT_RE.match("codex-events-xxx.jsonl")
        assert not _CC_SESSION_RE.match("codex-events-xxx.jsonl")