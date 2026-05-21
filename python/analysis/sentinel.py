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
from analysis.proposals.models import (
    Attribution,
    BuildEvidence,
    DispatchPolicyUpdate,
    ProposalStatus,
    ThresholdStatus,
    new_proposal_id,
)
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
    p.add_argument("--config", default="chitin.yaml", help="Path to chitin.yaml")
    p.add_argument("--now", default=None, help="ISO-8601 to fix the clock")
    return p.parse_args(argv)


def _now(value: str | None) -> datetime:
    if not value:
        return datetime.now(tz=timezone.utc)
    parsed = datetime.fromisoformat(value)
    if parsed.tzinfo is None:
        return parsed.replace(tzinfo=timezone.utc)
    return parsed


def _promotion_threshold(config_path: Path) -> tuple[int, list[str]]:
    default = 5
    warnings: list[str] = []
    if not config_path.exists():
        return default, warnings
    try:
        config_text = config_path.read_text()
    except OSError as exc:
        warnings.append(f"Warning: could not read sentinel.promotion_threshold from {config_path}: {exc}")
        return default, warnings

    value: str | int = default
    in_sentinel = False
    for raw_line in config_text.splitlines():
        line = raw_line.split("#", 1)[0].rstrip()
        if not line.strip():
            continue
        if not line.startswith((" ", "\t")):
            in_sentinel = line.strip() == "sentinel:"
            continue
        if in_sentinel:
            key, sep, raw_value = line.strip().partition(":")
            if sep and key == "promotion_threshold":
                value = raw_value.strip().strip("'\"")
                break

    try:
        threshold = int(value)
    except (TypeError, ValueError):
        warnings.append(f"Warning: invalid sentinel.promotion_threshold={value!r}; using default {default}")
        return default, warnings
    if threshold < 3:
        warnings.append(
            f"Warning: sentinel.promotion_threshold={threshold} below minimum 3; clamped to 3"
        )
        return 3, warnings
    return threshold, warnings


def _proposal_for_finding(finding, *, threshold: int, now: datetime) -> dict[str, Any] | None:
    draft = finding.draft
    if draft is None or not draft.rule_yaml.strip():
        return None
    pattern = finding.pattern
    threshold_status = (
        ThresholdStatus.ABOVE_THRESHOLD
        if pattern.count >= threshold
        else ThresholdStatus.BELOW_THRESHOLD
    )
    status = (
        ProposalStatus.PROPOSED
        if threshold_status == ThresholdStatus.ABOVE_THRESHOLD
        else ProposalStatus.BELOW_THRESHOLD
    )
    impact = draft.predicted_impact
    proposal = DispatchPolicyUpdate(
        id=new_proposal_id("sentinel"),
        attribution=Attribution(
            spec_provenance="spec:062-attribution TBD",
            sentinel_source=f"analysis.sentinel:{now.date().isoformat()}",
        ),
        evidence=BuildEvidence(build_grounding="spec:063-build TBD"),
        threshold_status=threshold_status,
        status=status,
        policy_path=PROPOSAL_PATH,
        update_summary=f"Candidate invariant for {pattern.rule_id}/{pattern.action_type}",
    )
    return {
        "id": proposal.id,
        "kind": proposal.kind,
        "rank": finding.rank,
        "rule_id": pattern.rule_id,
        "action_type": pattern.action_type,
        "agent_id": pattern.agent_id,
        "count": pattern.count,
        "confidence": draft.confidence,
        "proposal_path": proposal.policy_path,
        "threshold_status": str(proposal.threshold_status),
        "status": str(proposal.status),
        "attribution": {
            "spec_provenance": proposal.attribution.spec_provenance,
            "sentinel_source": proposal.attribution.sentinel_source,
        },
        "evidence": {
            "build_grounding": proposal.evidence.build_grounding,
        },
        "predicted_impact": None if impact is None else {
            "samples_evaluated": impact.samples_evaluated,
            "would_allow": impact.would_allow,
            "would_still_deny": impact.would_still_deny,
            "method": impact.method,
        },
    }


def _promotion_metadata(findings, *, threshold: int, now: datetime) -> dict[str, Any]:
    proposals: list[dict[str, Any]] = []
    for finding in findings:
        proposal = _proposal_for_finding(finding, threshold=threshold, now=now)
        if proposal is not None:
            proposals.append(proposal)

    review_queue = [p for p in proposals if p["threshold_status"] == str(ThresholdStatus.ABOVE_THRESHOLD)]
    status = "candidate" if proposals else "no-candidate"
    return {
        "promotion": {
            "proposal_path": PROPOSAL_PATH,
            "proposal_count": len(proposals),
            "review_queue_count": len(review_queue),
            "min_evidence_threshold": threshold,
            "status": status,
            "proposals": proposals,
            "review_queue": review_queue,
        }
    }


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.top_n < 1:
        print("Error: --top-n must be >= 1", file=sys.stderr)
        return 2

    now = _now(args.now)
    threshold, threshold_warnings = _promotion_threshold(Path(args.config))
    for warning in threshold_warnings:
        print(warning, file=sys.stderr)
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
    metadata = _promotion_metadata(findings, threshold=threshold, now=now)

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
