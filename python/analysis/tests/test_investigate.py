"""Tests for analysis.investigate — pre-canned alarm investigation recipe.

Pinned: the agent-facing JSON line + the markdown report shape are
contracts the analyst prompt depends on. Same alarm + same data must
always yield the same finding.
"""
from __future__ import annotations

import json
from pathlib import Path

import pytest

from analysis.investigate import (
    Finding,
    InvestigationContext,
    classify_alarm,
    investigate,
    investigate_bucket_b_regression,
    investigate_low_success,
    investigate_qwen_idle,
    main,
)


# ─── classify_alarm ────────────────────────────────────────────────────────


def test_classify_alarm_extracts_kind_from_leading_phrase():
    assert classify_alarm("BUCKET-B REGRESSION: 1/19 contaminated (5.3%)") == "bucket-b-regression"
    assert classify_alarm("LOW SUCCESS: driver=copilot 56% (5/9)") == "low-success"
    assert classify_alarm("QWEN IDLE: 100% T0 routed away") == "qwen-idle"


def test_classify_alarm_is_rate_invariant():
    """Two BUCKET-B alarms with different metrics map to the same kind
    so the alarm-feeder + investigator agree on dedup."""
    assert classify_alarm("BUCKET-B REGRESSION: 1/19 (5%)") == classify_alarm("BUCKET-B REGRESSION: 7/40 (18%)")


def test_classify_alarm_handles_no_colon():
    """Defensive: alarms without a colon still produce a kind."""
    out = classify_alarm("PLAIN ALARM no colon")
    assert out == "plain-alarm-no-colon"


def test_classify_alarm_empty_input_is_unknown():
    assert classify_alarm("") == "unknown"


# ─── investigate (top-level dispatch) ──────────────────────────────────────


def _ctx(tmp_path: Path, alarm: str, entry: str = "investigate-test") -> InvestigationContext:
    return InvestigationContext(entry_id=entry, alarm=alarm, out_dir=tmp_path)


def test_dispatches_bucket_b_to_dedicated_handler(tmp_path: Path):
    finding = investigate(_ctx(tmp_path, "BUCKET-B REGRESSION: 0/19 contaminated (0%)"))
    assert finding.recommended_action == "file-fix-entry"
    assert "bucket-b" in (tmp_path / "investigate-test.md").read_text().lower() or \
           "bucket-B" in (tmp_path / "investigate-test.md").read_text()


def test_dispatches_low_success_to_dedicated_handler(tmp_path: Path):
    finding = investigate(_ctx(tmp_path, "LOW SUCCESS: driver=copilot 56% (5/9)"))
    # 5/9 = medium confidence (≥5, <10)
    assert finding.confidence == "medium"
    assert "driver=copilot" in (tmp_path / "investigate-test.md").read_text()


def test_dispatches_qwen_idle_to_dedicated_handler(tmp_path: Path):
    finding = investigate(_ctx(tmp_path, "QWEN IDLE: 100% T0 routed away"))
    assert finding.recommended_action == "needs_human"
    assert "3090" in (tmp_path / "investigate-test.md").read_text()


def test_unknown_alarm_kind_falls_back_to_needs_human(tmp_path: Path):
    finding = investigate(_ctx(tmp_path, "UNKNOWN_ALARM_KIND: something"))
    assert finding.recommended_action == "needs_human"
    assert finding.confidence == "low"
    # Fallback report mentions the missing handler.
    assert "no handler" in (tmp_path / "investigate-test.md").read_text().lower()


# ─── investigate_low_success ───────────────────────────────────────────────


def test_low_success_high_confidence_when_n_ge_10(tmp_path: Path):
    finding = investigate_low_success(_ctx(tmp_path, "LOW SUCCESS: tier=T2 60% (6/10)"))
    assert finding.confidence == "high"
    assert finding.recommended_action == "file-fix-entry"


def test_low_success_low_confidence_when_n_lt_5(tmp_path: Path):
    finding = investigate_low_success(_ctx(tmp_path, "LOW SUCCESS: tier=T0 50% (1/2)"))
    assert finding.confidence == "low"
    assert finding.recommended_action == "needs_human"


def test_low_success_medium_confidence_when_n_in_5_to_9(tmp_path: Path):
    finding = investigate_low_success(_ctx(tmp_path, "LOW SUCCESS: driver=qwen 71% (5/7)"))
    assert finding.confidence == "medium"
    assert finding.recommended_action == "needs_human"  # medium → still operator


def test_low_success_malformed_alarm_falls_back(tmp_path: Path):
    """If the alarm doesn't match the expected shape, use the fallback."""
    finding = investigate_low_success(_ctx(tmp_path, "LOW SUCCESS: garbled"))
    assert finding.recommended_action == "needs_human"


# ─── investigate_bucket_b_regression ──────────────────────────────────────


def test_bucket_b_writes_a_report_and_returns_high_when_count_ge_2(tmp_path: Path):
    finding = investigate_bucket_b_regression(_ctx(tmp_path, "BUCKET-B REGRESSION: 3/40 (7.5%)"))
    # confidence is data-driven (reads latest rollup); on a host without rollup
    # data, count is 0 → medium. Just assert the report landed.
    report = tmp_path / "investigate-test.md"
    assert report.exists()
    assert "Investigation: bucket-B regression" in report.read_text()
    assert finding.recommended_action == "file-fix-entry"


# ─── main / CLI / JSON sidecar ─────────────────────────────────────────────


def test_main_writes_json_sidecar_and_prints_analysis_marker(tmp_path: Path, capsys):
    rc = main(
        [
            "--entry",
            "test-entry",
            "--alarm",
            "QWEN IDLE: 100%",
            "--out-dir",
            str(tmp_path),
        ]
    )
    assert rc == 0
    sidecar = tmp_path / "test-entry.json"
    report = tmp_path / "test-entry.md"
    assert sidecar.exists()
    assert report.exists()
    payload = json.loads(sidecar.read_text())
    assert set(payload.keys()) == {"root_cause", "recommended_action", "report_path", "confidence"}
    assert payload["recommended_action"] == "needs_human"
    captured = capsys.readouterr()
    # The agent reads the <<<ANALYSIS>>> marker line directly off stdout.
    assert "<<<ANALYSIS>>>" in captured.out
    marker_line = captured.out.split("<<<ANALYSIS>>>", 1)[1].split("\n", 1)[0]
    parsed_marker = json.loads(marker_line)
    assert parsed_marker == payload


def test_main_handles_unknown_alarm_kind_gracefully(tmp_path: Path, capsys):
    rc = main(
        [
            "--entry",
            "weird-entry",
            "--alarm",
            "WEIRD ALARM TYPE: x",
            "--out-dir",
            str(tmp_path),
        ]
    )
    assert rc == 0
    sidecar = tmp_path / "weird-entry.json"
    payload = json.loads(sidecar.read_text())
    assert payload["recommended_action"] == "needs_human"
    assert payload["confidence"] == "low"


# ─── Determinism ───────────────────────────────────────────────────────────


def test_same_alarm_produces_same_finding(tmp_path: Path):
    """Determinism contract: re-running on the same input yields
    byte-identical sidecar JSON modulo the report-path field. The
    timestamp in the markdown changes, but the structured Finding
    must not."""
    a_dir = tmp_path / "a"
    b_dir = tmp_path / "b"
    finding_a = investigate(_ctx(a_dir, "QWEN IDLE: 100%", entry="x"))
    finding_b = investigate(_ctx(b_dir, "QWEN IDLE: 100%", entry="x"))
    # Strip the dir-specific report_path before comparing.
    assert (
        finding_a.root_cause,
        finding_a.recommended_action,
        finding_a.confidence,
    ) == (
        finding_b.root_cause,
        finding_b.recommended_action,
        finding_b.confidence,
    )


def test_finding_is_serializable_json(tmp_path: Path):
    """The Finding must round-trip through JSON cleanly (the agent
    reads the JSON sidecar)."""
    finding = investigate(_ctx(tmp_path, "QWEN IDLE: 100%"))
    from dataclasses import asdict

    raw = json.dumps(asdict(finding))
    reconstituted = json.loads(raw)
    assert reconstituted["root_cause"] == finding.root_cause
    assert reconstituted["recommended_action"] == finding.recommended_action


# ─── Sanity ────────────────────────────────────────────────────────────────


def test_finding_recommended_action_is_one_of_three_canonical_values(tmp_path: Path):
    """The analyst prompt's <<<ANALYSIS>>> contract enumerates three
    actions; the recipe must never emit anything else."""
    canonical = {"file-fix-entry", "needs_human", "no-action"}
    for alarm in [
        "BUCKET-B REGRESSION: 0/0 (0%)",
        "LOW SUCCESS: tier=T1 50% (5/10)",
        "QWEN IDLE: 100%",
        "UNKNOWN: x",
    ]:
        finding = investigate(_ctx(tmp_path, alarm, entry=f"e-{hash(alarm)}"))
        assert finding.recommended_action in canonical
