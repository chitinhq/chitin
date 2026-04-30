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
    # gov/policy.go emits escalation as a string ("normal"/"elevated"/"high"/"lockdown")
    # — keep as Optional[str] not bool so we don't lose that signal.
    escalation: Optional[str] = None
    agent: Optional[str] = None
    action_type: Optional[str] = None
    action_target: Optional[str] = None
    envelope_id: Optional[str] = None
    tier: Optional[str] = None
    cost_usd: float = 0.0
    input_bytes: int = 0
    tool_calls: int = 0


def _coerce_float(v, default: float = 0.0) -> float:
    """Best-effort float coercion. Returns default on any failure (I5)."""
    if v is None:
        return default
    try:
        return float(v)
    except (TypeError, ValueError):
        return default


def _coerce_int(v, default: int = 0) -> int:
    """Best-effort int coercion. Returns default on any failure (I5)."""
    if v is None:
        return default
    try:
        return int(v)
    except (TypeError, ValueError):
        return default


def _coerce_escalation(v) -> Optional[str]:
    """Accept either string ('elevated') or bool/legacy. Returns Optional[str]."""
    if v is None or v is False:
        return None
    if v is True:
        return "elevated"  # legacy bool=true → mark as elevated, not silently dropped
    if isinstance(v, str):
        return v if v else None
    return None


def parse_decision_line(line: str) -> Optional[Decision]:
    """Parse a single JSONL line. Returns None on any error.

    Bad input never raises — analysis tolerates audit-log corruption (I5).
    Numeric coercion failures default to 0 rather than abort the line.
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
        escalation=_coerce_escalation(raw.get("escalation")),
        agent=raw.get("agent"),
        action_type=raw.get("action_type"),
        action_target=raw.get("action_target"),
        envelope_id=raw.get("envelope_id"),
        tier=raw.get("tier"),
        cost_usd=_coerce_float(raw.get("cost_usd")),
        input_bytes=_coerce_int(raw.get("input_bytes")),
        tool_calls=_coerce_int(raw.get("tool_calls")),
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
