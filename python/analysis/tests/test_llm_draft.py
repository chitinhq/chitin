"""Tests for Layer 2 LLM-drafted rules.

The LLM call is mocked. We test the fallback contract: any failure in the
LLM path falls back to the heuristic draft, never raises, never aborts.
"""
from datetime import datetime, timezone
from unittest.mock import patch

from analysis.llm_draft import enrich_with_llm
from analysis.models import Pattern, PredictedImpact, RuleDraft


def _heuristic_draft():
    return RuleDraft(
        kind="heuristic",
        template="t",
        confidence="medium",
        rule_yaml="rules: []\n",
        predicted_impact=PredictedImpact(samples_evaluated=1, would_allow=1,
                                         would_still_deny=0, method="m"),
        notes="",
    )


def _pattern():
    return Pattern(
        rule_id="r", action_type="t", agent_id="a", count=1,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny", sample_envelope_ids=("e1",), decisions=(),
    )


def test_llm_failure_falls_back_to_heuristic():
    with patch("analysis.llm_draft._call_ollama", side_effect=RuntimeError("boom")):
        out = enrich_with_llm([(_pattern(), _heuristic_draft())])
    assert len(out) == 1
    assert out[0].kind == "heuristic-fallback"
    assert out[0].rule_yaml == "rules: []\n"


def test_llm_success_returns_llm_draft():
    fake_yaml = "rules:\n  - id: llm-drafted\n"

    def fake_call(*args, **kwargs):
        return fake_yaml

    with patch("analysis.llm_draft._call_ollama", side_effect=fake_call):
        out = enrich_with_llm([(_pattern(), _heuristic_draft())])
    assert len(out) == 1
    assert out[0].kind == "llm"
    assert "llm-drafted" in out[0].rule_yaml
    # Impact prediction is INHERITED (not re-evaluated against LLM yaml).
    # Method tag must say so, so consumers don't conflate.
    assert "inherited" in out[0].predicted_impact.method.lower()


def test_no_heuristic_passes_through():
    with patch("analysis.llm_draft._call_ollama", return_value="..."):
        out = enrich_with_llm([(_pattern(), None)])
    assert out == []


def test_empty_llm_response_falls_back():
    with patch("analysis.llm_draft._call_ollama", return_value="   "):
        out = enrich_with_llm([(_pattern(), _heuristic_draft())])
    assert len(out) == 1
    assert out[0].kind == "heuristic-fallback"
    assert "empty LLM response" in out[0].notes
