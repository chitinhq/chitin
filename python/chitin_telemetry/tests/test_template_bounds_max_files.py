"""Tests for the bounds:max_files_changed heuristic template."""
from datetime import datetime, timezone

from chitin_telemetry.templates.bounds_max_files_changed import draft
from chitin_telemetry.models import Decision, Pattern


def _pattern_with_reason(*reasons):
    decisions = tuple(
        Decision(
            ts=datetime(2026, 4, 25, 8, i, tzinfo=timezone.utc),
            allowed=False,
            rule_id="bounds:max_files_changed",
            action_type="git.push",
            reason=r,
            envelope_id=f"e{i}",
        )
        for i, r in enumerate(reasons)
    )
    return Pattern(
        rule_id="bounds:max_files_changed",
        action_type="git.push",
        agent_id="<unknown>",
        count=len(reasons),
        first_seen=decisions[0].ts,
        last_seen=decisions[-1].ts,
        decision_class="deny",
        sample_envelope_ids=tuple(d.envelope_id for d in decisions[:3]),
        decisions=decisions,
    )


def test_doc_batch_drafted():
    p = _pattern_with_reason(
        "41 files changed exceeds ceiling of 25 (all under docs/)",
        "30 files changed exceeds ceiling of 25 (all under docs/)",
    )
    d = draft(p)
    assert d is not None
    assert d.template == "bounds_max_files_changed"
    # Schema check: emits valid chitin global Bounds block, not a fake per-rule field.
    assert "bounds:" in d.rule_yaml
    assert "max_files_changed" in d.rule_yaml
    # Per-rule bounds with path predicate requires kernel change (Issue #70).
    assert "Issue #70" in d.notes
    assert d.confidence == "low"
    assert d.predicted_impact.would_allow == 2


def test_no_doc_signal_returns_none():
    p = _pattern_with_reason(
        "60 files changed exceeds ceiling of 25",
        "100 files changed exceeds ceiling of 25",
    )
    assert draft(p) is None


def test_empty_pattern_returns_none():
    p = Pattern(
        rule_id="bounds:max_files_changed",
        action_type="git.push",
        agent_id="<unknown>",
        count=0,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=(),
        decisions=(),
    )
    assert draft(p) is None
