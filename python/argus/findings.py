"""`argus findings` — structured findings stream for Hermes standup-fold.

Slice 5 contract: produce a JSON list Hermes' standup can consume:

    [
        {
            "kind":            "demote_loop" | "stuck_pr_green_ci" | ...,
            "severity":        "info" | "warning" | "critical",
            "summary":         "<one-line headline>",
            "evidence_links":  ["kanban_event#N", "git_event#M", ...],
            "suggested_action": "file_ticket" | "archive_belief" | "dispatch_fix"
                                 | "investigate",
            "ts_unix":         <when the finding fired>,
            "subject":         "<task_id | #pr_num | other primary key>"
        },
        ...
    ]

The shape is intentionally small and stable — Hermes' standup cron
parses this output line-by-line and folds the top N into its
report. New fields can be added (Hermes ignores unknown keys).

Invariants:
    - Pure function over (xs_db, chain_db, since_ts). No side effects.
    - Deterministic ordering: severity desc (critical, warning, info),
      then ts_unix desc.
    - One-way flow: this CLI is the egress; Hermes never writes back.
"""
from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from argus.cross_detectors import CrossFinding, run_all_cross_detectors
from argus.detectors import Finding, run_all_detectors
from argus.drift_detectors import run_all_drift_detectors


_SEVERITY_RANK = {"critical": 0, "warning": 1, "info": 2}


_SUGGESTED_ACTION = {
    "demote_loop": "investigate",
    "stuck_pr_green_ci": "dispatch_fix",
    "follow_up_clustering": "investigate",
    "hermes_standup_gap": "investigate",
    "openclaw_dispatch_failure": "dispatch_fix",
    "stale_belief": "archive_belief",
    "belief_without_evidence": "archive_belief",
    "capability_without_belief": "file_ticket",
    # chain detectors
    "deny_cluster": "investigate",
    "unknown_rate_spike": "investigate",
    "agent_failure_run": "dispatch_fix",
    "stuck_flow": "investigate",
}


def collect_findings(
    chain_db: Path,
    xs_db: Path,
    since_ts: int = 0,
    now_ts: Optional[int] = None,
) -> list[dict]:
    """Run every detector class and produce the standup-fold JSON list."""
    now_ts = now_ts or int(datetime.now(timezone.utc).timestamp())
    out: list[dict] = []

    if chain_db.exists():
        for f in run_all_detectors(str(chain_db)):
            ts = int(f.ts.timestamp()) if hasattr(f.ts, "timestamp") else 0
            if ts < since_ts:
                continue
            out.append({
                "kind": f.detector,
                "severity": f.severity,
                "summary": f.title,
                "evidence_links": [],
                "suggested_action": _SUGGESTED_ACTION.get(f.detector, "investigate"),
                "ts_unix": ts,
                "subject": str(f.details.get("agent", "")) or "",
            })

    if xs_db.exists():
        for f in run_all_cross_detectors(xs_db, now_ts=now_ts):
            if f.ts_unix < since_ts:
                continue
            out.append({
                "kind": f.detector,
                "severity": f.severity,
                "summary": f.title,
                "evidence_links": list(f.evidence),
                "suggested_action": _SUGGESTED_ACTION.get(f.detector, "investigate"),
                "ts_unix": f.ts_unix,
                "subject": f.subject,
            })
        if chain_db.exists():
            for f in run_all_drift_detectors(xs_db, chain_db, now_ts=now_ts):
                if f.ts_unix and f.ts_unix < since_ts:
                    continue
                out.append({
                    "kind": f.detector,
                    "severity": f.severity,
                    "summary": f.title,
                    "evidence_links": list(f.evidence),
                    "suggested_action": _SUGGESTED_ACTION.get(f.detector, "investigate"),
                    "ts_unix": f.ts_unix,
                    "subject": f.subject,
                })

    out.sort(
        key=lambda d: (_SEVERITY_RANK.get(d["severity"], 99), -d["ts_unix"], d["subject"])
    )
    return out


def render_findings_json(findings: list[dict], indent: Optional[int] = None) -> str:
    """JSON projection of the findings list."""
    return json.dumps(findings, indent=indent, sort_keys=False)
