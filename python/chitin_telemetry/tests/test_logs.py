"""Tests for Slice 3 log and Discord ingestion."""
from __future__ import annotations

import tempfile
from pathlib import Path

from chitin_telemetry import migrations
from chitin_telemetry.indexer import init_db
from chitin_telemetry.logs import (
    DiscordMessage,
    DiscordRateLimited,
    PatternResult,
    ingest_discord_channel,
    ingest_log_file,
)


class FakeDiscordClient:
    def __init__(self, messages=None, exc=None):
        self.messages = messages or []
        self.exc = exc

    def fetch_channel_messages(self, channel_id: str, *, after=None, limit=50):
        if self.exc:
            raise self.exc
        return self.messages


def _ok_result(kind: str | None, subject: str | None = None) -> PatternResult:
    if kind is None:
        return PatternResult(matched=False)
    return PatternResult(matched=True, kind=kind, subject=subject, payload={})


def test_ingest_log_file_empty_boundary_records_checkpoint(monkeypatch):
    monkeypatch.setattr("chitin_telemetry.logs._extract_via_qwen", lambda *args, **kwargs: _ok_result("hermes_standup"))
    with tempfile.TemporaryDirectory() as tmpdir:
        root = Path(tmpdir)
        db_path = root / "index.db"
        log_path = root / "empty.log"
        log_path.write_text("")

        conn = init_db(db_path)
        migrations.apply_pending(conn)
        inserted, unparsed = ingest_log_file(conn, source="hermes", file_path=log_path)
        assert inserted == 0
        assert unparsed == 0
        checkpoint = migrations.get_checkpoint(conn, f"hermes:{log_path}")
        assert checkpoint is not None
        assert checkpoint["offset"] == 0
        conn.close()


def test_ingest_log_file_error_boundary_records_unparsed(monkeypatch):
    monkeypatch.setattr(
        "chitin_telemetry.logs._extract_via_qwen",
        lambda *args, **kwargs: PatternResult(matched=False, unparsed_reason="error:test"),
    )
    with tempfile.TemporaryDirectory() as tmpdir:
        root = Path(tmpdir)
        db_path = root / "index.db"
        log_path = root / "error.log"
        log_path.write_text("2026-05-13T08:00:00Z workflow failed\n")

        conn = init_db(db_path)
        migrations.apply_pending(conn)
        inserted, unparsed = ingest_log_file(conn, source="openclaw", file_path=log_path)
        assert inserted == 0
        assert unparsed == 1
        row = conn.execute("SELECT kind, reason FROM events WHERE source = 'openclaw'").fetchone()
        assert row["kind"] == "unparsed"
        assert row["reason"] == "error:test"
        conn.close()


def test_ingest_log_file_reopens_after_rotation(monkeypatch):
    monkeypatch.setattr("chitin_telemetry.logs._extract_via_qwen", lambda *args, **kwargs: _ok_result("hermes_standup"))
    with tempfile.TemporaryDirectory() as tmpdir:
        root = Path(tmpdir)
        db_path = root / "index.db"
        log_path = root / "gateway.log"
        log_path.write_text("2026-05-13T08:00:00Z standup posted\n")

        conn = init_db(db_path)
        migrations.apply_pending(conn)
        inserted, unparsed = ingest_log_file(conn, source="hermes", file_path=log_path)
        assert inserted == 1
        assert unparsed == 0

        log_path.unlink()
        log_path.write_text("2026-05-13T09:00:00Z ok\n")
        inserted, unparsed = ingest_log_file(conn, source="hermes", file_path=log_path)
        assert inserted == 1
        assert unparsed == 0
        assert conn.execute("SELECT COUNT(*) FROM events WHERE source = 'hermes'").fetchone()[0] == 2
        conn.close()


def test_ingest_log_file_records_unparsed_on_timeout(monkeypatch):
    monkeypatch.setattr(
        "chitin_telemetry.logs._extract_via_qwen",
        lambda *args, **kwargs: PatternResult(matched=False, unparsed_reason="timeout:test"),
    )
    with tempfile.TemporaryDirectory() as tmpdir:
        root = Path(tmpdir)
        db_path = root / "index.db"
        log_path = root / "workflow.log"
        log_path.write_text("2026-05-13T08:00:00Z workflow failed\n")

        conn = init_db(db_path)
        migrations.apply_pending(conn)
        inserted, unparsed = ingest_log_file(conn, source="openclaw", file_path=log_path)
        assert inserted == 0
        assert unparsed == 1
        row = conn.execute("SELECT kind, reason FROM events WHERE source = 'openclaw'").fetchone()
        assert row["kind"] == "unparsed"
        assert "timeout" in row["reason"]
        conn.close()


def test_ingest_discord_channel_handles_rate_limit_without_advancing_cursor():
    with tempfile.TemporaryDirectory() as tmpdir:
        db_path = Path(tmpdir) / "index.db"
        conn = init_db(db_path)
        migrations.apply_pending(conn)
        inserted, unparsed = ingest_discord_channel(
            conn,
            channel_id="123",
            channel_name="clawta",
            client=FakeDiscordClient(exc=DiscordRateLimited(2.0)),
        )
        assert inserted == 0
        assert unparsed == 0
        checkpoint = migrations.get_checkpoint(conn, "discord:clawta")
        assert checkpoint is not None
        conn.close()


def test_ingest_discord_channel_indexes_announces(monkeypatch):
    monkeypatch.setattr("chitin_telemetry.logs._extract_via_qwen", lambda *args, **kwargs: _ok_result("discord_clawta_announce", "t_deadbeef"))
    with tempfile.TemporaryDirectory() as tmpdir:
        db_path = Path(tmpdir) / "index.db"
        conn = init_db(db_path)
        migrations.apply_pending(conn)
        inserted, unparsed = ingest_discord_channel(
            conn,
            channel_id="123",
            channel_name="clawta",
            client=FakeDiscordClient(
                messages=[
                    DiscordMessage(
                        id="1",
                        channel_id="123",
                        content="🦞 Starting t_deadbeef to codex",
                        timestamp="2026-05-13T08:00:00+00:00",
                        author="clawta",
                    )
                ]
            ),
        )
        assert inserted == 1
        assert unparsed == 0
        row = conn.execute("SELECT kind, subject FROM events WHERE source = 'discord'").fetchone()
        assert row["kind"] == "discord_clawta_announce"
        assert row["subject"] == "t_deadbeef"
        conn.close()
