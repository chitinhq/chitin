"""Core types for the analysis layer."""
from __future__ import annotations

import json
from dataclasses import dataclass, field
from datetime import datetime
from typing import Optional


@dataclass(frozen=True)
class Decision:
    """One row from gov-decisions-*.jsonl."""

    ts: datetime
    allowed: bool
    mode: Optional[str] = None
    rule_id: Optional[str] = None
    reason: Optional[str] = None
    escalation: bool = False
    agent: Optional[str] = None
    action_type: Optional[str] = None
    action_target: Optional[str] = None
    envelope_id: Optional[str] = None
    tier: Optional[str] = None
    cost_usd: float = 0.0
    input_bytes: int = 0
    tool_calls: int = 0


def parse_decision_line(line: str) -> Optional[Decision]:
    """Parse a single JSONL line. Returns None on any error.

    Bad input never raises — analysis tolerates audit-log corruption (I5).
    """
    line = line.strip()
    if not line:
        return None
    try:
        raw = json.loads(line)
    except json.JSONDecodeError:
        return None
    if not isinstance(raw, dict):
        return None
    ts_str = raw.get("ts")
    if not isinstance(ts_str, str):
        return None
    try:
        ts = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
    except (ValueError, TypeError):
        return None
    return Decision(
        ts=ts,
        allowed=bool(raw.get("allowed", False)),
        mode=raw.get("mode"),
        rule_id=raw.get("rule_id"),
        reason=raw.get("reason"),
        escalation=bool(raw.get("escalation", False)),
        agent=raw.get("agent"),
        action_type=raw.get("action_type"),
        action_target=raw.get("action_target"),
        envelope_id=raw.get("envelope_id"),
        tier=raw.get("tier"),
        cost_usd=float(raw.get("cost_usd", 0.0)),
        input_bytes=int(raw.get("input_bytes", 0)),
        tool_calls=int(raw.get("tool_calls", 0)),
    )


@dataclass(frozen=True)
class Pattern:
    """A repeat (rule_id, action_type, agent_id) tuple over the window."""

    rule_id: str
    action_type: str
    agent_id: str
    count: int
    first_seen: datetime
    last_seen: datetime
    decision_class: str  # "deny" | "allow"
    sample_envelope_ids: tuple[str, ...]
    decisions: tuple[Decision, ...] = field(repr=False, default=())


@dataclass(frozen=True)
class PredictedImpact:
    samples_evaluated: int
    would_allow: int
    would_still_deny: int
    method: str


@dataclass(frozen=True)
class RuleDraft:
    kind: str  # "heuristic" | "heuristic-fallback" | "llm"
    template: str
    confidence: str  # "low" | "medium" | "high"
    rule_yaml: str
    predicted_impact: PredictedImpact
    notes: str = ""
