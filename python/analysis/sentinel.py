"""CLI entry: python -m analysis.sentinel ...

Sentinel consumes gov-decisions JSONL, mines repeat deny patterns, and emits
bounded invariant proposals for `chitin.yaml`. It reuses the canonical
decisions loader, detector, and rule templates; the sentinel-specific contract
is the promotion metadata that identifies candidate rules, confidence, and the
only policy file path a worker may patch.
"""
from __future__ import annotations

import argparse
import os
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from analysis.detect import detect_patterns
from analysis.draft import draft_for_pattern, reason_no_template
from analysis.loaders import load_gov_decisions, parse_window_str
from analysis.writers import build_finding, write_json, write_markdown_from_json
import analysis.templates.all  # noqa: F401  (registers all templates)


PROPOSAL_PATH = "chitin.yaml"


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        prog="analysis.sentinel",
        description="Mine chain decisions and propose bounded chitin.yaml invariants.",
    )
    p.add_argument("--window", default="7d", help="Window: e.g., 7d, 24h, 60m")
    p.add_argument("--top-n", type=int, default=10, help="Maximum patterns to inspect")
    p.add_argument("--out-dir", default="python/analysis/out", help="Output directory")
    p.add_argument(
        "--decisions-dir",
        default=os.environ.get("HOME", "/") + "/.chitin",
        help="Directory containing gov-decisions-*.jsonl",
    )
    p.add_argument("--now", default=None, help="ISO-8601 to fix the clock")
    return p.parse_args(argv)


def _now(value: str | None) -> datetime:
    if not value:
        return datetime.now(tz=timezone.utc)
    parsed = datetime.fromisoformat(value)
    if parsed.tzinfo is None:
        return parsed.replace(tzinfo=timezone.utc)
    return parsed


def _promotion_metadata(findings) -> dict[str, Any]:
    proposals: list[dict[str, Any]] = []
    for finding in findings:
        draft = finding.draft
        if draft is None or not draft.rule_yaml.strip():
            continue
        impact = draft.predicted_impact
        proposals.append(
            {
                "rank": finding.rank,
                "rule_id": finding.pattern.rule_id,
                "action_type": finding.pattern.action_type,
                "agent_id": finding.pattern.agent_id,
                "count": finding.pattern.count,
                "confidence": draft.confidence,
                "proposal_path": PROPOSAL_PATH,
                "predicted_impact": None if impact is None else {
                    "samples_evaluated": impact.samples_evaluated,
                    "would_allow": impact.would_allow,
                    "would_still_deny": impact.would_still_deny,
                    "method": impact.method,
                },
            }
        )

    status = "candidate" if proposals else "no-candidate"
    return {
        "promotion": {
            "proposal_path": PROPOSAL_PATH,
            "proposal_count": len(proposals),
            "status": status,
            "proposals": proposals,
        }
    }


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.top_n < 1:
        print("Error: --top-n must be >= 1", file=sys.stderr)
        return 2

    now = _now(args.now)
    decisions_dir = Path(args.decisions_dir)
    if not decisions_dir.exists():
        print(f"Error: decisions-dir does not exist: {decisions_dir}", file=sys.stderr)
        return 2

    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    window = parse_window_str(args.window, now)
    print(f"Loading decisions from {decisions_dir} (window: {args.window})...", file=sys.stderr)
    load_result = load_gov_decisions(decisions_dir, window)
    decisions = load_result.decisions
    print(
        f"  Loaded {len(decisions)} decisions from {load_result.files_read} files "
        f"({load_result.parse_errors} parse errors).",
        file=sys.stderr,
    )

    patterns = detect_patterns(decisions)
    top = patterns[: args.top_n]
    rest = patterns[args.top_n:]
    print(f"Detected {len(patterns)} deny patterns; inspecting max {len(top)}.", file=sys.stderr)

    findings = []
    no_template = []
    for i, pattern in enumerate(top, start=1):
        draft = draft_for_pattern(pattern)
        if draft is None:
            no_template.append({
                "rule_id": pattern.rule_id,
                "action_type": pattern.action_type,
                "agent_id": pattern.agent_id,
                "count": pattern.count,
                "reason_no_template": reason_no_template(pattern),
            })
        else:
            findings.append(build_finding(pattern, draft, rank=i))

    for pattern in rest:
        no_template.append({
            "rule_id": pattern.rule_id,
            "action_type": pattern.action_type,
            "agent_id": pattern.agent_id,
            "count": pattern.count,
            "reason_no_template": "below top-N cutoff",
        })

    distinct_rule_ids = len(Counter(d.rule_id for d in decisions))
    denies = sum(1 for d in decisions if not d.allowed)
    summary = {
        "total_decisions": len(decisions),
        "denies": denies,
        "allows": len(decisions) - denies,
        "files_read": load_result.files_read,
        "parse_errors": load_result.parse_errors,
        "distinct_rule_ids": distinct_rule_ids,
        "decisions_missing_envelope_id": sum(1 for d in decisions if not d.envelope_id),
    }

    date_str = now.date().isoformat()
    json_path = out_dir / f"sentinel-{date_str}.json"
    md_path = out_dir / f"sentinel-{date_str}.md"
    metadata = _promotion_metadata(findings)

    try:
        write_json(
            json_path,
            findings=findings,
            no_template=no_template,
            input_summary=summary,
            generated_at=now,
            window_since=window.since,
            window_until=window.until,
            window_size=args.window,
            stream="sentinel",
            metadata=metadata,
        )
        write_markdown_from_json(json_path, md_path)
    except OSError as e:
        print(f"Error writing output: {e}", file=sys.stderr)
        return 3

    proposal_count = metadata["promotion"]["proposal_count"]
    print(f"Wrote {json_path}", file=sys.stderr)
    print(f"Wrote {md_path}", file=sys.stderr)
    print(f"Candidate invariant proposals for {PROPOSAL_PATH}: {proposal_count}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
