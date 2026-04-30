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
    window_size: str,
) -> None:
    """Write the canonical analysis JSON. Deterministic given fixed inputs (I4).

    `window_size` is the original CLI string (e.g. "7d", "60m"). `total_seconds`
    is the precise span; consumers that need numeric width should use that.
    """
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    total_seconds = int((window_until - window_since).total_seconds())
    body = {
        "schema_version": "1",
        "stream": "decisions",
        "generated_at": generated_at.isoformat(),
        "window": {
            "size": window_size,
            "since": window_since.isoformat(),
            "until": window_until.isoformat(),
            "total_seconds": total_seconds,
        },
        "input_summary": dict(input_summary),
        "patterns": [_finding_to_json(f) for f in findings],
        "no_template_patterns": list(no_template),
    }
    text = json.dumps(body, indent=2, sort_keys=True, ensure_ascii=False)
    path.write_text(text + "\n")


def write_markdown_from_json(json_path: Path, md_path: Path) -> None:
    """Render the markdown projection from a canonical JSON file (I2).

    Markdown is non-authoritative — JSON is the contract. Reads the JSON and
    produces a deterministic markdown rendering.
    """
    json_path = Path(json_path)
    md_path = Path(md_path)
    md_path.parent.mkdir(parents=True, exist_ok=True)

    data = json.loads(json_path.read_text())
    lines: list[str] = []

    until = data["window"]["until"][:10]
    lines.append(f"# Decisions Analysis — {until}")
    lines.append("")
    lines.append(f"**Window:** {data['window']['size']} "
                 f"({data['window']['since'][:10]} → {until})")
    summary = data["input_summary"]
    missing_id = summary.get("decisions_missing_envelope_id", 0)
    missing_str = f", {missing_id} missing envelope_id" if missing_id else ""
    lines.append(f"**Input:** {summary['total_decisions']} decisions "
                 f"({summary['allows']} allowed, {summary['denies']} denied), "
                 f"{summary['distinct_rule_ids']} distinct rule_ids, "
                 f"{summary['parse_errors']} parse errors{missing_str}")
    lines.append("")
    lines.append("---")
    lines.append("")

    if not data["patterns"]:
        lines.append("_No deny patterns in this window._")
        lines.append("")
    else:
        lines.append("## Top patterns")
        lines.append("")
        for p in data["patterns"]:
            _render_pattern(p, lines)

    if data.get("no_template_patterns"):
        lines.append("---")
        lines.append("")
        lines.append("## Patterns without a template")
        lines.append("")
        lines.append("These deny patterns were observed but no heuristic template "
                     "knew how to draft a candidate rule. Surfaced for human review.")
        lines.append("")
        for nt in data["no_template_patterns"]:
            lines.append(f"- **{nt['rule_id']}** × {nt['action_type']} × "
                         f"{nt['agent_id']} — {nt['count']} denies "
                         f"({nt['reason_no_template']})")
        lines.append("")

    md_path.write_text("\n".join(lines))


def _render_pattern(p: dict[str, Any], lines: list[str]) -> None:
    header = (f"### #{p['rank']} — {p['rule_id']} × {p['action_type']} × "
              f"{p['agent_id']} ({p['count']} denies)")
    lines.append(header)
    lines.append("")
    lines.append(f"First seen: {p['first_seen']}. Last seen: {p['last_seen']}.")
    lines.append("")
    if p["draft"] is None:
        lines.append("_No candidate rule drafted._")
        lines.append("")
        return

    d = p["draft"]
    lines.append(f"**Candidate rule** ({d['kind']}, {d['confidence']} confidence, "
                 f"template `{d['template']}`):")
    lines.append("")
    lines.append("```yaml")
    lines.append(d["rule_yaml"].rstrip())
    lines.append("```")
    lines.append("")
    impact = d["predicted_impact"]
    lines.append(f"**Predicted impact:** samples_evaluated: {impact['samples_evaluated']}, "
                 f"would_allow: {impact['would_allow']}, "
                 f"would_still_deny: {impact['would_still_deny']} "
                 f"(method: {impact['method']})")
    lines.append("")
    if d["notes"]:
        lines.append(f"_Notes: {d['notes']}_")
        lines.append("")
    sample_ids = ", ".join(p["sample_envelope_ids"])
    lines.append(f"_Sample envelope ids:_ {sample_ids}")
    lines.append("")
