"""CLI entry: python -m chitin_telemetry.decisions ...

End-to-end driver for the decisions stream. Reads gov-decisions JSONL,
detects deny patterns, drafts candidate rules, writes JSON + markdown.

Invariants (see SPEC.md):
    I1  Deterministic ranking via detect_patterns + sorted writers.
    I2  JSON + markdown both written; markdown rendered from JSON.
    I3  Default path is no-network. `--llm-draft` opts into ollama.
    I4  Output is byte-identical given identical input + identical `--now`.
    I5  Bad JSONL lines counted in parse_errors; never abort the run.
    I6  Writes go only to `--out-dir` (default python/analysis/out, gitignored).

Boundaries:
    - Missing `--decisions-dir` → exit 2 with stderr note.
    - Output-write failure → exit 3. JSON is written before markdown so
      partial state is "JSON exists, markdown missing", never the reverse.
    - Empty window → patterns=[], no_template_patterns=[], exit 0.
    - `--llm-draft` on but no findings → LLM call skipped (nothing to enrich).
"""
from __future__ import annotations

import argparse
import os
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

from chitin_telemetry.detect import detect_patterns
from chitin_telemetry.draft import draft_for_pattern, reason_no_template
from chitin_telemetry.loaders import load_gov_decisions, parse_window_str
from chitin_telemetry.writers import build_finding, write_json, write_markdown_from_json
import chitin_telemetry.templates.all  # noqa: F401  (registers all templates)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="chitin_telemetry.decisions",
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

    if args.llm_draft and findings:
        from chitin_telemetry.llm_draft import enrich_with_llm
        pairs = [(f.pattern, f.draft) for f in findings]
        enriched = enrich_with_llm(pairs)
        findings = [
            build_finding(f.pattern, new_draft, rank=f.rank)
            for f, new_draft in zip(findings, enriched)
        ]

    distinct_rule_ids = len(Counter(d.rule_id for d in decisions))
    denies = sum(1 for d in decisions if not d.allowed)
    allows = len(decisions) - denies
    missing_envelope_id = sum(1 for d in decisions if not d.envelope_id)
    summary = {
        "total_decisions": len(decisions),
        "denies": denies,
        "allows": allows,
        "files_read": load_result.files_read,
        "parse_errors": load_result.parse_errors,
        "distinct_rule_ids": distinct_rule_ids,
        "decisions_missing_envelope_id": missing_envelope_id,
    }

    date_str = now.date().isoformat()
    json_path = out_dir / f"decisions-{date_str}.json"
    md_path = out_dir / f"decisions-{date_str}.md"

    try:
        write_json(json_path, findings=findings, no_template=no_template,
                   input_summary=summary, generated_at=now,
                   window_since=window.since, window_until=window.until,
                   window_size=args.window)
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
        if top1.draft and top1.draft.predicted_impact is not None:
            imp = top1.draft.predicted_impact
            print(f"  → predicted: {imp.would_allow} allows, "
                  f"{imp.would_still_deny} still deny", file=sys.stderr)
        elif top1.draft:
            print(f"  → diagnostic/{top1.draft.kind} draft (no predicted impact)",
                  file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
