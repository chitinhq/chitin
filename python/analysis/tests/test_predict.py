"""Tests for analysis.predict — stdlib logistic regression."""
from __future__ import annotations

import json
from datetime import datetime, timezone

import pytest

from chitin_telemetry.models import Decision
from analysis.predict import (
    Vocab,
    extract_features,
    from_dict,
    predict,
    to_dict,
    train,
)


def _ts(hour: int = 12) -> datetime:
    return datetime(2026, 5, 3, hour, 0, 0, tzinfo=timezone.utc)


def _decision(*, action_type: str, agent: str, allowed: bool, hour: int = 12) -> Decision:
    return Decision(
        ts=_ts(hour),
        allowed=allowed,
        action_type=action_type,
        agent=agent,
    )


# ──────────────────────────────────────────────────────────────────
# Feature extraction
# ──────────────────────────────────────────────────────────────────

def test_extract_features_empty() -> None:
    X, y, vocab = extract_features([])
    assert X == []
    assert y == []
    assert "<unk>" in vocab.action_types
    assert "<unk>" in vocab.agents


def test_extract_features_single_row() -> None:
    decisions = [_decision(action_type="shell.exec", agent="claude-code", allowed=True)]
    X, y, vocab = extract_features(decisions)
    assert len(X) == 1
    assert len(y) == 1
    assert y[0] == 0  # allowed → not denied
    # one row, all columns wired
    assert len(X[0]) == vocab.n_features
    # bias column is the last one
    assert X[0][-1] == 1.0


def test_extract_features_label_is_deny() -> None:
    decisions = [
        _decision(action_type="shell.exec", agent="claude-code", allowed=False),
        _decision(action_type="shell.exec", agent="claude-code", allowed=True),
    ]
    _, y, _ = extract_features(decisions)
    assert y == [1, 0]


# ──────────────────────────────────────────────────────────────────
# Training behavior
# ──────────────────────────────────────────────────────────────────

def test_train_empty_returns_degenerate() -> None:
    vocab = Vocab(action_types={"<unk>": 0}, agents={"<unk>": 0})
    model = train([], [], vocab, iterations=10)
    assert model.n_samples == 0
    # predict against degenerate model returns base_rate (0.0)
    p = predict(model, "shell.exec", "claude-code", 12)
    assert p == 0.0


def test_train_separable_classes_converges() -> None:
    """When deny is perfectly correlated with action_type, predict
    should give P(deny) significantly above 0.5 for the bad type
    and significantly below for the good one."""
    decisions = (
        [_decision(action_type="rm.recursive", agent="a", allowed=False)] * 30
        + [_decision(action_type="file.read", agent="a", allowed=True)] * 30
    )
    X, y, vocab = extract_features(decisions)
    model = train(X, y, vocab, iterations=300, learning_rate=0.1)
    p_bad = predict(model, "rm.recursive", "a", 12)
    p_good = predict(model, "file.read", "a", 12)
    assert p_bad > 0.7, f"expected high deny prob for bad type, got {p_bad}"
    assert p_good < 0.3, f"expected low deny prob for good type, got {p_good}"


def test_predict_unknown_category_falls_to_unk() -> None:
    decisions = [_decision(action_type="shell.exec", agent="a", allowed=True)] * 10
    X, y, vocab = extract_features(decisions)
    model = train(X, y, vocab, iterations=50)
    # Never seen this action_type in training — must not raise
    p = predict(model, "novel.action", "a", 12)
    assert 0.0 <= p <= 1.0


# ──────────────────────────────────────────────────────────────────
# Persistence round-trip
# ──────────────────────────────────────────────────────────────────

def test_persistence_roundtrip_preserves_predictions() -> None:
    decisions = (
        [_decision(action_type="rm.recursive", agent="a", allowed=False)] * 20
        + [_decision(action_type="file.read", agent="a", allowed=True)] * 20
    )
    X, y, vocab = extract_features(decisions)
    model = train(X, y, vocab, iterations=100)
    blob = to_dict(model)
    restored = from_dict(blob)
    p_orig = predict(model, "rm.recursive", "a", 12)
    p_rest = predict(restored, "rm.recursive", "a", 12)
    assert abs(p_orig - p_rest) < 1e-9


# ──────────────────────────────────────────────────────────────────
# CLI end-to-end
# ──────────────────────────────────────────────────────────────────

def test_cli_train_then_predict_roundtrip(tmp_path) -> None:
    """End-to-end: spawn the CLI to train against synthetic
    gov-decisions JSONL, then spawn it again to predict. Catches
    regressions like the wrong-default-decisions-path bug Copilot
    surfaced on PR #256."""
    import subprocess
    import sys

    decisions_dir = tmp_path / "chitin"
    decisions_dir.mkdir()
    fixture = decisions_dir / "gov-decisions-2026-05-03.jsonl"
    rows = []
    for i in range(40):
        ok = "true" if i % 4 != 0 else "false"
        rows.append(
            f'{{"ts":"2026-05-03T10:{i:02d}:00Z","allowed":{ok},'
            f'"action_type":"shell.exec","agent":"claude-code"}}'
        )
    fixture.write_text("\n".join(rows) + "\n")

    model_path = tmp_path / "model.json"
    train_proc = subprocess.run(
        [sys.executable, "-m", "analysis.predict", "train",
         f"--decisions-dir={decisions_dir}",
         f"--out={model_path}",
         "--iterations=50"],
        capture_output=True, text=True,
    )
    assert train_proc.returncode == 0, f"train failed: {train_proc.stderr}"
    assert model_path.exists()

    predict_proc = subprocess.run(
        [sys.executable, "-m", "analysis.predict", "predict",
         f"--model={model_path}",
         "--action-type=shell.exec",
         "--agent=claude-code",
         "--hour=10"],
        capture_output=True, text=True,
    )
    assert predict_proc.returncode == 0, f"predict failed: {predict_proc.stderr}"
    out = json.loads(predict_proc.stdout)
    assert "predicted_deny_probability" in out
    assert 0.0 <= out["predicted_deny_probability"] <= 1.0


def test_base_rate_recorded() -> None:
    decisions = (
        [_decision(action_type="x", agent="a", allowed=False)] * 10
        + [_decision(action_type="x", agent="a", allowed=True)] * 30
    )
    X, y, vocab = extract_features(decisions)
    model = train(X, y, vocab, iterations=10)
    # 10 deny / 40 total = 0.25
    assert pytest.approx(model.base_rate, abs=1e-9) == 0.25
