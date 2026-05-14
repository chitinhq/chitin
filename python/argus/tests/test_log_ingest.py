"""Tests for log_ingest — prefix parsing, inode rotation, truncated lines."""
from __future__ import annotations

import os
import tempfile
import time
from pathlib import Path

import pytest

from argus.cross_source_db import init_cross_source_db
from argus.log_ingest import (
    TailState,
    discover_logs,
    ingest_log_file,
    ingest_logs,
    parse_log_line,
)


class TestParseLogLine:
    def test_python_logger_format(self):
        line = "2026-05-13 21:29:49,010 INFO discord.gateway: Shard ID None resumed"
        parsed = parse_log_line(line)
        assert parsed is not None
        assert parsed.level == "INFO"
        assert parsed.logger == "discord.gateway"
        assert "Shard ID None resumed" in parsed.msg

    def test_bare_prefix_format(self):
        line = "2026-05-13 21:23:14 tick: empty queue"
        parsed = parse_log_line(line)
        assert parsed is not None
        assert parsed.level is None
        assert parsed.logger is None
        assert "tick" in parsed.msg

    def test_unparseable_line_returns_none(self):
        assert parse_log_line("garbage with no timestamp") is None

    def test_empty_line_returns_none(self):
        assert parse_log_line("") is None
        assert parse_log_line("\n") is None


class TestIngestLogFile:
    def test_empty_file_zero_inserts(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            f = tmpdir / "empty.log"
            f.write_text("")
            xs = init_cross_source_db(tmpdir / "xs.db")
            try:
                i, s, u, state = ingest_log_file(f, "hermes", xs)
            finally:
                xs.close()
            assert (i, s, u) == (0, 0, 0)

    def test_python_log_lines_ingested(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            f = tmpdir / "agent.log"
            f.write_text(
                "2026-05-13 12:00:00,000 INFO agent.standup: Standup posted\n"
                "2026-05-13 12:00:01,000 ERROR agent.run: something broke\n"
            )
            xs = init_cross_source_db(tmpdir / "xs.db")
            try:
                i, s, u, _ = ingest_log_file(f, "hermes", xs)
                rows = xs.execute(
                    "SELECT kind, subject FROM cross_source_events ORDER BY ts_unix"
                ).fetchall()
            finally:
                xs.close()
            assert i == 2
            kinds = {r[0] for r in rows}
            assert "hermes_standup" in kinds
            assert "hermes_error" in kinds

    def test_idempotent_on_replay(self):
        """Same file, same state → second pass inserts nothing."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            f = tmpdir / "agent.log"
            f.write_text("2026-05-13 12:00:00 INFO a.b: hi\n")
            xs = init_cross_source_db(tmpdir / "xs.db")
            try:
                _, _, _, state = ingest_log_file(f, "hermes", xs)
                # Replay with the advanced state → no new rows.
                i2, _, _, _ = ingest_log_file(f, "hermes", xs, state=state)
                total = xs.execute("SELECT COUNT(*) FROM cross_source_events").fetchone()[0]
            finally:
                xs.close()
            assert i2 == 0
            assert total == 1

    def test_appended_lines_picked_up(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            f = tmpdir / "agent.log"
            f.write_text("2026-05-13 12:00:00 INFO a.b: first\n")
            xs = init_cross_source_db(tmpdir / "xs.db")
            try:
                _, _, _, state = ingest_log_file(f, "hermes", xs)

                with f.open("a") as fp:
                    fp.write("2026-05-13 12:00:01 INFO a.b: second\n")

                i2, _, _, _ = ingest_log_file(f, "hermes", xs, state=state)
                total = xs.execute("SELECT COUNT(*) FROM cross_source_events").fetchone()[0]
            finally:
                xs.close()
            assert i2 == 1
            assert total == 2

    def test_truncated_last_line_deferred(self):
        """A line without trailing \\n must NOT be ingested until the newline arrives."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            f = tmpdir / "agent.log"
            f.write_text("2026-05-13 12:00:00 INFO a.b: complete\n2026-05-13 12:00:01 INFO a.b: partial")
            xs = init_cross_source_db(tmpdir / "xs.db")
            try:
                i1, _, _, state = ingest_log_file(f, "hermes", xs)
                # Only the complete line was indexed.
                assert i1 == 1

                # Now the partial line completes — second pass picks it up.
                with f.open("a") as fp:
                    fp.write(" finished\n")
                i2, _, _, _ = ingest_log_file(f, "hermes", xs, state=state)
                assert i2 == 1
            finally:
                xs.close()

    def test_logrotate_truncate_resets_offset(self):
        """logrotate copytruncate: file size drops below state.offset → reopen at 0."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            f = tmpdir / "agent.log"
            f.write_text(
                "2026-05-13 12:00:00 INFO a.b: original-one\n"
                "2026-05-13 12:00:01 INFO a.b: original-two\n"
            )
            xs = init_cross_source_db(tmpdir / "xs.db")
            try:
                _, _, _, state = ingest_log_file(f, "hermes", xs)

                # Simulate logrotate copytruncate: file truncated, then one
                # post-rotate line appended. file_size now < state.offset.
                f.write_text("2026-05-13 12:00:02 INFO a.b: post-rotate\n")
                i2, _, _, _ = ingest_log_file(f, "hermes", xs, state=state)
                total = xs.execute("SELECT COUNT(*) FROM cross_source_events").fetchone()[0]
            finally:
                xs.close()
            assert i2 == 1
            assert total == 3  # 2 original + 1 post-rotate

    def test_unparseable_line_counted_not_skipped(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            f = tmpdir / "agent.log"
            f.write_text("garbage with no timestamp\n2026-05-13 12:00:00 INFO a.b: ok\n")
            xs = init_cross_source_db(tmpdir / "xs.db")
            try:
                i, s, u, _ = ingest_log_file(f, "hermes", xs)
            finally:
                xs.close()
            assert i == 1
            assert u == 1


class TestIngestLogs:
    def test_discovers_logs_in_dir(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            (tmpdir / "a.log").write_text("")
            (tmpdir / "b.log").write_text("")
            (tmpdir / "not-a-log.txt").write_text("")
            assert {p.name for p in discover_logs(tmpdir)} == {"a.log", "b.log"}

    def test_missing_log_root_returns_zero(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            summary, _ = ingest_logs(Path(tmpdir) / "missing", "hermes", Path(tmpdir) / "xs.db")
            assert summary["inserted"] == 0
            assert summary["files"] == []
