"""CLI entry: python -m analysis.decisions ..."""
from __future__ import annotations

import argparse
import os
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

from analysis.detect import detect_patterns
from analysis.draft import draft_for_pattern, reason_no_template
from analysis.loaders import load_gov_decisions, parse_window_str
from analysis.writers import build_finding, write_json, write_markdown_from_json
import analysis.templates.all  # noqa: F401  (registers all templates)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="analysis.decisions",
                                description="Rank deny patterns and draft candidate rules.")
    p.add_argument("--window", default="7d", help="Window: e.g., 7d, 24h, 60m")
    p.add_argument("--top-n", type=int, default=10, help="Top N patterns to keep")
    p.add_argument("--out-dir", default="python/analysis/out",
                   help="Output directory")
    p.add_argument("--decisions-dir",
                   default=os.environ.get("HOME", "/") + "/.chitin",
                   help="Directory containing gov-decisions-*.jsonl")
    p.add_argument("--now", default=None,
                   help="ISO-8601 to fix the clock (deterministic tests)")
    p.add_argument("--llm-draft", action="store_true",
                   help="Enable Layer 2 LLM-drafted rules (opt-in)")
    return p.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)

    if args.now:
        now = datetime.fromisoformat(args.now)
        if now.tzinfo is None:
            now = now.replace(tzinfo=timezone.utc)
    else:
        now = datetime.now(tz=timezone.utc)

    decisions_dir = Path(args.decisions_dir)
    if not decisions_dir.exists():
        print(f"Error: decisions-dir does not exist: {decisions_dir}", file=sys.stderr)
        return 2

    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    window = parse_window_str(args.window, now)
    print(f"Loading decisions from {decisions_dir} (window: {args.window})...",
          file=sys.stderr)
    load_result = load_gov_decisions(decisions_dir, window)
    decisions = load_result.decisions
    print(f"  Loaded {len(decisions)} decisions from {load_result.files_read} files "
          f"({load_result.parse_errors} parse errors).", file=sys.stderr)

    patterns = detect_patterns(decisions)
    top = patterns[: args.top_n]
    rest = patterns[args.top_n:]
    print(f"Detected {len(patterns)} deny patterns; keeping top {len(top)}.",
          file=sys.stderr)

    findings = []
    no_template = []
    for i, p in enumerate(top, start=1):
        d = draft_for_pattern(p)
        if d is None:
            no_template.append({
                "rule_id": p.rule_id,
                "action_type": p.action_type,
                "agent_id": p.agent_id,
                "count": p.count,
                "reason_no_template": reason_no_template(p),
            })
        else:
            findings.append(build_finding(p, d, rank=i))

    for p in rest:
        no_template.append({
            "rule_id": p.rule_id,
            "action_type": p.action_type,
            "agent_id": p.agent_id,
            "count": p.count,
            "reason_no_template": "below top-N cutoff",
        })

    distinct_rule_ids = len(Counter(d.rule_id for d in decisions))
    denies = sum(1 for d in decisions if not d.allowed)
    allows = len(decisions) - denies
    summary = {
        "total_decisions": len(decisions),
        "denies": denies,
        "allows": allows,
        "files_read": load_result.files_read,
        "parse_errors": load_result.parse_errors,
        "distinct_rule_ids": distinct_rule_ids,
    }

    date_str = now.date().isoformat()
    json_path = out_dir / f"decisions-{date_str}.json"
    md_path = out_dir / f"decisions-{date_str}.md"

    try:
        write_json(json_path, findings=findings, no_template=no_template,
                   input_summary=summary, generated_at=now,
                   window_since=window.since, window_until=window.until,
                   window_days=int((window.until - window.since).total_seconds() // 86400) or 1)
        write_markdown_from_json(json_path, md_path)
    except OSError as e:
        print(f"Error writing output: {e}", file=sys.stderr)
        return 3

    print(f"Wrote {json_path}", file=sys.stderr)
    print(f"Wrote {md_path}", file=sys.stderr)
    if findings:
        top1 = findings[0]
        print(f"\nTop finding: {top1.pattern.rule_id} × {top1.pattern.action_type} "
              f"× {top1.pattern.agent_id} — {top1.pattern.count} denies", file=sys.stderr)
        if top1.draft:
            imp = top1.draft.predicted_impact
            print(f"  → predicted: {imp.would_allow} allows, "
                  f"{imp.would_still_deny} still deny", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
