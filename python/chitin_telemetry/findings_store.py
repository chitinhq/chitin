"""Findings persistence layer.

Bridges the legacy in-memory `Finding` dataclass (detectors.py) and the
new `findings` table. Detectors keep emitting dataclass instances; the
kernel persists them here with idempotent finding_hash dedup.

The `findings_cli` and `reporter` modules read from this layer instead
of re-running detectors at report time.
"""
from __future__ import annotations

import hashlib
import json
import sqlite3
import time
from dataclasses import dataclass
from typing import Optional

from chitin_telemetry.detectors import Finding


def _finding_hash(f: Finding, bucket_s: int = 3600) -> str:
    """Bucket-based hash for idempotency.

    Two emits of the same identity within the same bucket window collapse
    to a single row. Bucket size defaults to 1h so re-runs within the hour
    don't multiply rows but new occurrences past the hour do.

    The identity is extracted from a stable subset of `details` — the entity
    identifiers, not the numeric measurements that tick between runs. This
    lets `stuck_flow` keep ticking up `idle_seconds` in the title without
    defeating dedup.
    """
    bucket = int(f.ts.timestamp()) // bucket_s
    d = f.details if isinstance(f.details, dict) else {}
    # Entity identifiers — stable across detector re-runs.
    identity_keys = (
        "agent", "rules", "start_ts_unix", "first_ts_unix",
        "last_event_ts_unix",
    )
    parts = [f.detector, str(bucket)]
    found_any_identity = False
    for k in identity_keys:
        v = d.get(k)
        if isinstance(v, list):
            v = "[" + ",".join(sorted(str(x) for x in v)) + "]"
        if v is not None:
            parts.append(f"{k}={v}")
            found_any_identity = True
    # Fall back to title-based identity if details has no entity keys.
    # Title may contain live-updating numbers, but that's better than
    # collapsing distinct findings that share only a detector + bucket.
    if not found_any_identity:
        parts.append(f"title={f.title}")
    payload = "|".join(parts)
    return hashlib.sha256(payload.encode()).hexdigest()


def persist(
    conn: sqlite3.Connection,
    findings: list[Finding],
    *,
    citations_by_detector: Optional[dict[str, list[str]]] = None,
) -> tuple[int, int]:
    """Insert findings; returns (inserted, skipped_duplicates).

    `citations_by_detector` is an optional map of detector_name → list
    of expected citation tokens to attach. Used later by the judge pass
    when LLM-narrated bodies are added.
    """
    inserted = 0
    skipped = 0
    citations_by_detector = citations_by_detector or {}
    for f in findings:
        fhash = _finding_hash(f)
        body = json.dumps(f.details, indent=2, default=str)
        detail_cites = []
        if isinstance(f.details, dict):
            detail_cites = list(f.details.get("citations") or [])
        merged_cites = list(dict.fromkeys([*citations_by_detector.get(f.detector, []), *detail_cites]))
        cites = json.dumps(merged_cites)
        try:
            conn.execute(
                """
                INSERT INTO findings (
                    finding_hash, ts_unix, detector, severity, title,
                    body, citations
                ) VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    fhash,
                    int(f.ts.timestamp()),
                    f.detector,
                    f.severity,
                    f.title,
                    body,
                    cites,
                ),
            )
            inserted += 1
        except sqlite3.IntegrityError:
            skipped += 1
    conn.commit()
    return inserted, skipped


@dataclass(frozen=True)
class StoredFinding:
    id: int
    finding_hash: str
    ts_unix: int
    detector: str
    severity: str
    title: str
    body: str
    citations: list[str]
    superseded_by: Optional[int]
    operator_action: Optional[str]
    operator_action_ts: Optional[int]
    pushed_ts: Optional[int]


def since(
    conn: sqlite3.Connection,
    since_ts_unix: int,
    *,
    severity: Optional[str] = None,
    include_acked: bool = False,
    limit: Optional[int] = None,
) -> list[StoredFinding]:
    """Read findings emitted since `since_ts_unix`."""
    sql = "SELECT * FROM findings WHERE ts_unix >= ?"
    params: list = [since_ts_unix]
    if severity:
        sql += " AND severity = ?"
        params.append(severity)
    if not include_acked:
        sql += " AND (operator_action IS NULL OR operator_action = 'flag')"
    sql += " ORDER BY ts_unix DESC"
    if limit:
        sql += f" LIMIT {int(limit)}"
    return [_row_to_stored(r) for r in conn.execute(sql, params).fetchall()]


def mark_pushed(conn: sqlite3.Connection, finding_ids: list[int]) -> None:
    if not finding_ids:
        return
    now = int(time.time())
    qmarks = ",".join("?" for _ in finding_ids)
    conn.execute(
        f"UPDATE findings SET pushed_ts = ? WHERE id IN ({qmarks})",
        [now, *finding_ids],
    )
    conn.commit()


def set_operator_action(
    conn: sqlite3.Connection, finding_id: int, action: str
) -> bool:
    """Set ack | snooze | flag | apply on a finding. Returns True if row found."""
    if action not in {"ack", "snooze", "flag", "apply"}:
        raise ValueError(f"invalid action: {action!r}")
    now = int(time.time())
    cur = conn.execute(
        "UPDATE findings SET operator_action = ?, operator_action_ts = ? WHERE id = ?",
        (action, now, finding_id),
    )
    conn.commit()
    return cur.rowcount > 0


def action_rate(conn: sqlite3.Connection, window_days: int = 7) -> dict:
    """Operator engagement metric over a window.

    Returns:
        {surfaced, acked, applied, snoozed, flagged, ignored, action_rate}
    """
    since_ts = int(time.time()) - window_days * 86400
    row = conn.execute(
        """
        SELECT
            COUNT(*) AS surfaced,
            SUM(CASE WHEN operator_action = 'ack' THEN 1 ELSE 0 END) AS acked,
            SUM(CASE WHEN operator_action = 'apply' THEN 1 ELSE 0 END) AS applied,
            SUM(CASE WHEN operator_action = 'snooze' THEN 1 ELSE 0 END) AS snoozed,
            SUM(CASE WHEN operator_action = 'flag' THEN 1 ELSE 0 END) AS flagged,
            SUM(CASE WHEN operator_action IS NULL THEN 1 ELSE 0 END) AS ignored
        FROM findings WHERE ts_unix >= ?
        """,
        (since_ts,),
    ).fetchone()
    s = int(row["surfaced"] or 0)
    if s == 0:
        return {
            "surfaced": 0, "acked": 0, "applied": 0, "snoozed": 0,
            "flagged": 0, "ignored": 0, "action_rate": 0.0,
            "window_days": window_days,
        }
    acted = int((row["acked"] or 0) + (row["applied"] or 0))
    return {
        "surfaced": s,
        "acked": int(row["acked"] or 0),
        "applied": int(row["applied"] or 0),
        "snoozed": int(row["snoozed"] or 0),
        "flagged": int(row["flagged"] or 0),
        "ignored": int(row["ignored"] or 0),
        "action_rate": acted / s,
        "window_days": window_days,
    }


def _row_to_stored(r: sqlite3.Row) -> StoredFinding:
    return StoredFinding(
        id=int(r["id"]),
        finding_hash=r["finding_hash"],
        ts_unix=int(r["ts_unix"]),
        detector=r["detector"],
        severity=r["severity"],
        title=r["title"],
        body=r["body"],
        citations=json.loads(r["citations"] or "[]"),
        superseded_by=r["superseded_by"],
        operator_action=r["operator_action"],
        operator_action_ts=r["operator_action_ts"],
        pushed_ts=r["pushed_ts"],
    )
