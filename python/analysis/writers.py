"""Output writers — JSON canonical and markdown projection.

JSON is the contract. Markdown is regenerable from JSON. Both deterministic
given identical inputs (and a fixed `generated_at`).
"""
from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any, Optional

from analysis.types import Pattern, RuleDraft


@dataclass(frozen=True)
class Finding:
    """A pattern paired with a (possibly None) rule draft, plus rank."""

    rank: int
    pattern: Pattern
    draft: Optional[RuleDraft]


def build_finding(pattern: Pattern, draft: Optional[RuleDraft], rank: int) -> Finding:
    return Finding(rank=rank, pattern=pattern, draft=draft)


def _pattern_to_json(p: Pattern) -> dict[str, Any]:
    return {
        "rule_id": p.rule_id,
        "action_type": p.action_type,
        "agent_id": p.agent_id,
        "count": p.count,
        "first_seen": p.first_seen.isoformat(),
        "last_seen": p.last_seen.isoformat(),
        "decision_class": p.decision_class,
        "sample_envelope_ids": list(p.sample_envelope_ids),
    }


def _draft_to_json(d: RuleDraft) -> dict[str, Any]:
    return {
        "kind": d.kind,
        "template": d.template,
        "confidence": d.confidence,
        "rule_yaml": d.rule_yaml,
        "predicted_impact": {
            "samples_evaluated": d.predicted_impact.samples_evaluated,
            "would_allow": d.predicted_impact.would_allow,
            "would_still_deny": d.predicted_impact.would_still_deny,
            "method": d.predicted_impact.method,
        },
        "notes": d.notes,
    }


def _finding_to_json(f: Finding) -> dict[str, Any]:
    obj = {"rank": f.rank, **_pattern_to_json(f.pattern)}
    obj["draft"] = _draft_to_json(f.draft) if f.draft else None
    return obj


def write_json(
    path: Path,
    *,
    findings: list[Finding],
    no_template: list[dict[str, Any]],
    input_summary: dict[str, Any],
    generated_at: datetime,
    window_since: datetime,
    window_until: datetime,
    window_days: int,
) -> None:
    """Write the canonical analysis JSON. Deterministic given fixed inputs (I4)."""
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    body = {
        "schema_version": "1",
        "stream": "decisions",
        "generated_at": generated_at.isoformat(),
        "window": {
            "days": window_days,
            "since": window_since.isoformat(),
            "until": window_until.isoformat(),
        },
        "input_summary": dict(input_summary),
        "patterns": [_finding_to_json(f) for f in findings],
        "no_template_patterns": list(no_template),
    }
    text = json.dumps(body, indent=2, sort_keys=True, ensure_ascii=False)
    path.write_text(text + "\n")
