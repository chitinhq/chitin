from __future__ import annotations

import importlib.util
import json
from importlib.machinery import SourceFileLoader
from datetime import datetime, timezone
from pathlib import Path


def load_swarm_audit():
    path = Path(__file__).resolve().parents[1] / "bin" / "swarm-audit"
    spec = importlib.util.spec_from_loader(
        "swarm_audit", SourceFileLoader("swarm_audit", str(path))
    )
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


def test_build_summary_event_shape():
    swarm_audit = load_swarm_audit()
    facts = {
        "chain": {"total_rows": 2},
        "kanban": {"lane_counts": {"ready": 1}},
        "github": {"opened": []},
        "clawta": {"recent_ticks": 3},
    }

    event = swarm_audit.build_summary_event(
        "[fix-now] claude-code denials spiked\n[ok] queue healthy",
        facts,
        24,
        21000,
        datetime(2026, 5, 13, 8, 0, tzinfo=timezone.utc),
    )

    assert event["schema_version"] == "2"
    assert event["event_type"] == "swarm.audit.summary"
    assert event["surface"] == "swarm-audit"
    assert event["payload"]["window_hours"] == 24
    assert event["payload"]["facts"] == facts
    assert event["payload"]["bullets"] == [
        {"tag": "fix-now", "text": "claude-code denials spiked"},
        {"tag": "ok", "text": "queue healthy"},
    ]
    assert event["payload"]["duration_ms"] == 21000


def test_iter_ledger_rows_since_reads_decisions_recent(monkeypatch, tmp_path):
    swarm_audit = load_swarm_audit()
    calls = []

    class Result:
        returncode = 0
        stdout = (
            '[{"ts":"2026-05-13T07:30:00+00:00","rule_id":"new"},'
            '{"ts":"2026-05-12T07:30:00+00:00","rule_id":"old"}]'
        )
        stderr = ""

    def fake_run(args, **kwargs):
        calls.append(args)
        return Result()

    monkeypatch.setattr(swarm_audit, "CHITIN_KERNEL_BIN", "chitin-kernel")
    monkeypatch.setattr(swarm_audit, "LEDGER_DIR", tmp_path)
    monkeypatch.setattr(swarm_audit.subprocess, "run", fake_run)

    rows = list(swarm_audit.iter_ledger_rows_since("2026-05-13T07:00:00+00:00"))

    assert rows == [{"ts": "2026-05-13T07:30:00+00:00", "rule_id": "new"}]
    assert calls
    assert calls[0][:3] == ["chitin-kernel", "decisions", "recent"]
    assert "--window-hours" in calls[0]
    assert "--limit" in calls[0]
    assert "5000" in calls[0]


def test_emit_summary_event_writes_parseable_event_file(monkeypatch, tmp_path):
    swarm_audit = load_swarm_audit()
    seen = {}

    class Result:
        returncode = 0
        stdout = '{"ok":true}'
        stderr = ""

    def fake_run(args, **kwargs):
        event_path = Path(args[args.index("--event-file") + 1])
        seen["args"] = args
        seen["event"] = json.loads(event_path.read_text())
        return Result()

    monkeypatch.setattr(swarm_audit, "CHITIN_KERNEL_BIN", "chitin-kernel")
    monkeypatch.setattr(swarm_audit, "LEDGER_DIR", tmp_path)
    monkeypatch.setattr(swarm_audit.subprocess, "run", fake_run)

    swarm_audit.emit_summary_event(
        "[ok] queue healthy",
        {"chain": {"total_rows": 1}},
        24,
        1234,
    )

    assert seen["args"][:2] == ["chitin-kernel", "emit"]
    assert "--event-file" in seen["args"]
    assert seen["event"]["event_type"] == "swarm.audit.summary"
    assert seen["event"]["payload"]["bullets"] == [{"tag": "ok", "text": "queue healthy"}]
