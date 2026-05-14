"""Cross-source detectors over the kanban + git index.

Detectors implemented in this slice:
    demote_loop          — ticket bounced ready→triage ≥2× in window
    stuck_pr_green_ci    — PR open >24h with no merge event
    follow_up_clustering — N kanban tickets filed shortly after a merge

Each Finding carries explicit `evidence` listing the kanban event ids
and git PR/commit references it joined on (spec § Slice 2 source
attribution requirement).

Invariants:
    - Pure functions over (db_path, now_ts). No side effects.
    - Deterministic tie-breaker on finding ordering: (ts desc, subject).
    - One missing source ≠ failure: missing-side joins still emit
      findings from the present side (caller can decide severity).
"""
from __future__ import annotations

import json
import sqlite3
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional


@dataclass(frozen=True)
class CrossFinding:
    detector: str
    severity: str  # info | warning | critical
    title: str
    ts_unix: int
    subject: str
    details: dict
    evidence: list[str] = field(default_factory=list)


def _ro(db_path: Path) -> sqlite3.Connection:
    conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    conn.execute("PRAGMA query_only = ON")
    conn.row_factory = sqlite3.Row
    return conn


def detect_demote_loops(
    xs_db: Path,
    window_seconds: int = 24 * 3600,
    min_bounces: int = 2,
    now_ts: Optional[int] = None,
) -> list[CrossFinding]:
    """Tickets that bounced ready→triage ≥min_bounces times in window.

    A "bounce" is a kanban_status_transition whose payload encodes
    from='ready' and to='triage'. The detector groups bounces by
    task_id (subject) and flags any task whose bounce count in the
    rolling window meets or exceeds the threshold.

    Invariant: exactly `min_bounces` bounces DOES trigger;
    `min_bounces - 1` does NOT.
    """
    now_ts = now_ts or int(datetime.now(timezone.utc).timestamp())
    since_ts = now_ts - window_seconds

    findings: list[CrossFinding] = []
    if not xs_db.exists():
        return findings

    conn = _ro(xs_db)
    try:
        rows = conn.execute(
            """
            SELECT id, subject, ts_unix, payload_json
            FROM cross_source_events
            WHERE source='kanban'
              AND kind='kanban_status_transition'
              AND ts_unix >= ?
            ORDER BY ts_unix ASC
            """,
            (since_ts,),
        ).fetchall()
    finally:
        conn.close()

    by_task: dict[str, list[dict]] = {}
    for row in rows:
        try:
            payload = json.loads(row["payload_json"]) if row["payload_json"] else {}
        except (TypeError, ValueError):
            payload = {}
        if payload.get("from") == "ready" and payload.get("to") == "triage":
            by_task.setdefault(row["subject"], []).append({
                "id": row["id"],
                "ts_unix": row["ts_unix"],
                "by": payload.get("by"),
            })

    for task_id, bounces in by_task.items():
        if len(bounces) >= min_bounces:
            last_ts = bounces[-1]["ts_unix"]
            findings.append(CrossFinding(
                detector="demote_loop",
                severity="warning",
                title=f"Demote loop: {task_id} bounced ready→triage {len(bounces)}× in {window_seconds // 3600}h",
                ts_unix=last_ts,
                subject=task_id,
                details={
                    "task_id": task_id,
                    "bounce_count": len(bounces),
                    "window_hours": window_seconds // 3600,
                    "demoted_by": list({b["by"] for b in bounces if b["by"]}),
                },
                evidence=[f"kanban_event#{b['id']}" for b in bounces],
            ))

    findings.sort(key=lambda f: (-f.ts_unix, f.subject))
    return findings


def detect_stuck_prs_green_ci(
    xs_db: Path,
    min_open_seconds: int = 24 * 3600,
    now_ts: Optional[int] = None,
) -> list[CrossFinding]:
    """PRs with git_pr_opened > min_open_seconds ago and no git_pr_merged.

    Slice 2 minimal: relies on the merged event being indexed (i.e.,
    if a PR has merged, we should already have its git_pr_merged row).
    Doesn't yet query CI state — that's a Slice 3 / GitHub-checks
    follow-up. Open PR + no merge event past the threshold is the
    cheapest cross-source proxy for "stuck PR" today.
    """
    now_ts = now_ts or int(datetime.now(timezone.utc).timestamp())
    threshold_ts = now_ts - min_open_seconds

    findings: list[CrossFinding] = []
    if not xs_db.exists():
        return findings

    conn = _ro(xs_db)
    try:
        opened_rows = conn.execute(
            """
            SELECT id, subject, ts_unix, payload_json, actor
            FROM cross_source_events
            WHERE source='git' AND kind='git_pr_opened'
              AND ts_unix <= ?
            """,
            (threshold_ts,),
        ).fetchall()
        merged_subjects = {
            row["subject"]
            for row in conn.execute(
                "SELECT subject FROM cross_source_events WHERE source='git' AND kind='git_pr_merged'"
            )
        }
    finally:
        conn.close()

    for row in opened_rows:
        if row["subject"] in merged_subjects:
            continue
        try:
            payload = json.loads(row["payload_json"]) if row["payload_json"] else {}
        except (TypeError, ValueError):
            payload = {}
        # Don't flag draft PRs — operators leave drafts open intentionally.
        if payload.get("draft"):
            continue
        age_hours = (now_ts - row["ts_unix"]) // 3600
        findings.append(CrossFinding(
            detector="stuck_pr_green_ci",
            severity="info",
            title=f"Stuck PR: {row['subject']} open for {age_hours}h with no merge event",
            ts_unix=row["ts_unix"],
            subject=row["subject"],
            details={
                "pr_number": row["subject"],
                "title": payload.get("title"),
                "age_hours": age_hours,
                "author": row["actor"],
            },
            evidence=[f"git_event#{row['id']}"],
        ))

    findings.sort(key=lambda f: (-f.ts_unix, f.subject))
    return findings


def detect_follow_up_clustering(
    xs_db: Path,
    window_seconds: int = 7 * 24 * 3600,
    min_tickets: int = 3,
    now_ts: Optional[int] = None,
) -> list[CrossFinding]:
    """N+ kanban tickets created within `window_seconds` after a PR merge.

    Heuristic for "the merge introduced debt": if a merge is followed
    by a burst of new tickets within the window, surface the merge
    as a candidate for review.

    Cross-source join: git_pr_merged → kanban_ticket_create within window.
    The kanban side joins by ticket-creation events (kind='created').
    """
    now_ts = now_ts or int(datetime.now(timezone.utc).timestamp())
    horizon_ts = now_ts - 30 * 24 * 3600

    findings: list[CrossFinding] = []
    if not xs_db.exists():
        return findings

    conn = _ro(xs_db)
    try:
        merges = conn.execute(
            """
            SELECT id, subject, ts_unix, payload_json
            FROM cross_source_events
            WHERE source='git' AND kind='git_pr_merged'
              AND ts_unix >= ?
            ORDER BY ts_unix ASC
            """,
            (horizon_ts,),
        ).fetchall()

        all_creates = conn.execute(
            """
            SELECT id, subject, ts_unix
            FROM cross_source_events
            WHERE source='kanban' AND kind IN ('kanban_event','kanban_ticket_create')
              AND ts_unix >= ?
            ORDER BY ts_unix ASC
            """,
            (horizon_ts,),
        ).fetchall()
    finally:
        conn.close()

    for merge in merges:
        merge_ts = merge["ts_unix"]
        window_end = merge_ts + window_seconds
        followups = [
            r for r in all_creates
            if merge_ts <= r["ts_unix"] <= window_end
        ]
        if len(followups) >= min_tickets:
            try:
                payload = json.loads(merge["payload_json"]) if merge["payload_json"] else {}
            except (TypeError, ValueError):
                payload = {}
            findings.append(CrossFinding(
                detector="follow_up_clustering",
                severity="info",
                title=(
                    f"Follow-up cluster: {len(followups)} tickets opened within "
                    f"{window_seconds // 86400}d of merging {merge['subject']}"
                ),
                ts_unix=merge_ts,
                subject=merge["subject"],
                details={
                    "merged_pr": merge["subject"],
                    "merged_title": payload.get("title"),
                    "follow_up_count": len(followups),
                    "window_days": window_seconds // 86400,
                },
                evidence=(
                    [f"git_event#{merge['id']}"]
                    + [f"kanban_event#{f['id']}" for f in followups[:5]]
                ),
            ))

    findings.sort(key=lambda f: (-f.ts_unix, f.subject))
    return findings


def run_all_cross_detectors(xs_db: Path, now_ts: Optional[int] = None) -> list[CrossFinding]:
    """Run every cross-source detector and return all findings."""
    findings: list[CrossFinding] = []
    findings.extend(detect_demote_loops(xs_db, now_ts=now_ts))
    findings.extend(detect_stuck_prs_green_ci(xs_db, now_ts=now_ts))
    findings.extend(detect_follow_up_clustering(xs_db, now_ts=now_ts))
    findings.sort(key=lambda f: (-f.ts_unix, f.subject, f.detector))
    return findings
