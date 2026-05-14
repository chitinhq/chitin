"""Log-derived detectors (Slice 3).

Detectors implemented:
    hermes_standup_gap        — > N hours between consecutive standups
    openclaw_dispatch_failure — any openclaw_dispatch_fail in the window

Invariants:
    - Pure functions over (xs_db, now_ts). No side effects.
    - Deterministic ordering: ts desc, then subject.
    - Missing source data (no standup events at all) ≠ failure: yields zero findings.
"""
from __future__ import annotations

import sqlite3
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from argus.cross_detectors import CrossFinding


def _ro(db_path: Path) -> sqlite3.Connection:
    conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    conn.execute("PRAGMA query_only = ON")
    conn.row_factory = sqlite3.Row
    return conn


def detect_hermes_standup_gap(
    xs_db: Path,
    max_gap_seconds: int = 8 * 3600,
    now_ts: Optional[int] = None,
) -> list[CrossFinding]:
    """Pairs of adjacent hermes_standup events more than max_gap_seconds apart.

    The standup cron is meant to fire at fixed intervals — a gap that
    exceeds `max_gap_seconds` (default 8h, matching the spec's
    cron-misfire threshold) is a signal that the standup job is dead.
    """
    now_ts = now_ts or int(datetime.now(timezone.utc).timestamp())
    findings: list[CrossFinding] = []
    if not xs_db.exists():
        return findings

    conn = _ro(xs_db)
    try:
        rows = conn.execute(
            """
            SELECT id, ts_unix, subject
            FROM cross_source_events
            WHERE source='hermes' AND kind='hermes_standup'
            ORDER BY ts_unix ASC
            """
        ).fetchall()
    finally:
        conn.close()

    for i in range(1, len(rows)):
        prev = rows[i - 1]
        curr = rows[i]
        gap = curr["ts_unix"] - prev["ts_unix"]
        if gap > max_gap_seconds:
            findings.append(CrossFinding(
                detector="hermes_standup_gap",
                severity="warning",
                title=(
                    f"Hermes standup gap: {gap // 3600}h between "
                    f"consecutive standups (threshold {max_gap_seconds // 3600}h)"
                ),
                ts_unix=curr["ts_unix"],
                subject=curr["subject"],
                details={
                    "gap_seconds": gap,
                    "gap_hours": gap // 3600,
                    "previous_ts_unix": prev["ts_unix"],
                    "current_ts_unix": curr["ts_unix"],
                    "threshold_hours": max_gap_seconds // 3600,
                },
                evidence=[f"hermes_event#{prev['id']}", f"hermes_event#{curr['id']}"],
            ))

    findings.sort(key=lambda f: (-f.ts_unix, f.subject))
    return findings


def detect_openclaw_dispatch_failures(
    xs_db: Path,
    window_seconds: int = 24 * 3600,
    now_ts: Optional[int] = None,
) -> list[CrossFinding]:
    """All openclaw_dispatch_fail events in the rolling window."""
    now_ts = now_ts or int(datetime.now(timezone.utc).timestamp())
    since_ts = now_ts - window_seconds
    findings: list[CrossFinding] = []
    if not xs_db.exists():
        return findings

    conn = _ro(xs_db)
    try:
        rows = conn.execute(
            """
            SELECT id, ts_unix, subject, payload_json, actor
            FROM cross_source_events
            WHERE source='openclaw' AND kind='openclaw_dispatch_fail'
              AND ts_unix >= ?
            ORDER BY ts_unix DESC
            """,
            (since_ts,),
        ).fetchall()
    finally:
        conn.close()

    for row in rows:
        findings.append(CrossFinding(
            detector="openclaw_dispatch_failure",
            severity="warning",
            title=f"OpenClaw dispatch failure in {row['subject']}",
            ts_unix=row["ts_unix"],
            subject=row["subject"],
            details={
                "file": row["subject"],
                "logger": row["actor"],
                "payload": row["payload_json"],
                "window_hours": window_seconds // 3600,
            },
            evidence=[f"openclaw_event#{row['id']}"],
        ))

    findings.sort(key=lambda f: (-f.ts_unix, f.subject))
    return findings


def run_all_log_detectors(xs_db: Path, now_ts: Optional[int] = None) -> list[CrossFinding]:
    """Run every log-derived detector and return findings, deterministically sorted."""
    findings: list[CrossFinding] = []
    findings.extend(detect_hermes_standup_gap(xs_db, now_ts=now_ts))
    findings.extend(detect_openclaw_dispatch_failures(xs_db, now_ts=now_ts))
    findings.sort(key=lambda f: (-f.ts_unix, f.subject, f.detector))
    return findings
