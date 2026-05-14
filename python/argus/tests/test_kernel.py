"""Smoke test for the kernel tick: GPU mocked unavailable, no LLM calls fire."""
from __future__ import annotations

import os
import tempfile
from pathlib import Path

import pytest

from argus import config, kernel, migrations, gpu as gpu_mod
from argus.indexer import init_db


@pytest.fixture
def db(monkeypatch):
    with tempfile.TemporaryDirectory() as tmp:
        db_path = Path(tmp) / "i.db"
        conn = init_db(db_path)
        migrations.apply_pending(conn)
        # Pre-seed an event so detectors have something to look at.
        conn.execute(
            """
            INSERT INTO events (line_hash, ts, ts_unix, allowed, rule_id, agent, action_type, source)
            VALUES ('h1', '2026-05-13T00:00:00+00:00', 1715567890, 0, 'r', 'a', 'shell.exec', 'chain')
            """
        )
        conn.commit()
        # Force ARGUS to talk to no LLM: monkeypatch gpu.status to unavailable
        monkeypatch.setattr(
            gpu_mod, "status",
            lambda **kw: gpu_mod.GpuStatus(
                available=False, reason="test_forced",
                util_pct=0.0, vram_free_mib=0, operator_active=True,
            ),
        )
        yield conn, db_path
        conn.close()


def test_tick_runs_deterministic_detectors_without_llm(db, tmp_path, monkeypatch):
    conn, db_path = db
    # Redirect heartbeat/journal/state to tmp_path to avoid touching $HOME.
    monkeypatch.setattr(kernel, "HEARTBEAT_PATH", tmp_path / "hb.json")
    monkeypatch.setattr(kernel, "JOURNAL_PATH", tmp_path / "journal.ndjson")
    monkeypatch.setattr(kernel, "STATE_HTML_PATH", tmp_path / "state.html")
    cfg = config.ArgusConfig()
    rec = kernel.tick(conn, cfg)
    # gpu unavailable means we either ran detectors_ok or detectors_failed,
    # and never hit "narrate" / "keep_warm".
    assert rec.action in {"detectors_ok", "detectors_failed", "noop"}
    assert "gpu_unavail" in rec.detail
    # State HTML written
    assert (tmp_path / "state.html").exists()
    # Heartbeat written
    assert (tmp_path / "hb.json").exists()


def test_tick_persists_detector_findings(db, tmp_path, monkeypatch):
    conn, db_path = db
    monkeypatch.setattr(kernel, "HEARTBEAT_PATH", tmp_path / "hb.json")
    monkeypatch.setattr(kernel, "JOURNAL_PATH", tmp_path / "journal.ndjson")
    monkeypatch.setattr(kernel, "STATE_HTML_PATH", tmp_path / "state.html")
    cfg = config.ArgusConfig()
    kernel.tick(conn, cfg)
    # The deterministic stuck-flow detector should fire on our single seeded
    # event from 2024 (idle >> 1h ago).
    rows = conn.execute("SELECT detector FROM findings").fetchall()
    assert any("stuck_flow" in r["detector"] for r in rows)
