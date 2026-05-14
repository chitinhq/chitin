"""Deterministic detectors over the event index."""
from __future__ import annotations

import json
import re
import sqlite3
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Optional


def _utc_now() -> datetime:
    """Timezone-aware UTC now. Replaces the deprecated datetime.utcnow()."""
    return datetime.now(timezone.utc)


def _from_unix(ts_unix: int) -> datetime:
    """Timezone-aware UTC datetime from unix epoch."""
    return datetime.fromtimestamp(ts_unix, timezone.utc)


@dataclass(frozen=True)
class Finding:
    """A detector finding."""

    detector: str
    ts: datetime
    severity: str  # "info" | "warning" | "critical"
    title: str
    details: dict


def _get_conn(db_path: str) -> sqlite3.Connection:
    """Open connection to index.db."""
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    return conn


_TICKET_RE = re.compile(r"\bt_[a-z0-9]+\b", re.IGNORECASE)
_PRIORITY_RE = re.compile(r"\bp\s*([0-9]{1,3})\b", re.IGNORECASE)


def _table_exists(conn: sqlite3.Connection, table_name: str) -> bool:
    row = conn.execute(
        "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?",
        (table_name,),
    ).fetchone()
    return row is not None


def _extract_subjects(text: str | None) -> list[str]:
    if not text:
        return []
    return sorted({m.group(0) for m in _TICKET_RE.finditer(text)})


def _normalize_claim(claim: str) -> str:
    priority = _priority_from_claim(claim)
    if priority is not None:
        return f"priority:P{priority}"
    status = re.search(r"\b(status|lane)\s*:\s*([a-z_]+)\b", claim, re.IGNORECASE)
    if status:
        return f"status:{status.group(2).lower()}"
    return re.sub(r"\s+", " ", claim.strip().lower())[:120]


def _priority_from_claim(claim: str) -> Optional[int]:
    match = _PRIORITY_RE.search(claim)
    if match:
        try:
            return int(match.group(1))
        except ValueError:
            return None
    explicit = re.search(r"\bpriority\s*:\s*([0-9]{1,3})\b", claim, re.IGNORECASE)
    if explicit:
        try:
            return int(explicit.group(1))
        except ValueError:
            return None
    return None


def _recent_ticket_activity(conn: sqlite3.Connection, subject: str, *, active_window_days: int = 30) -> bool:
    now_ts = int(_utc_now().timestamp())
    window_start = now_ts - active_window_days * 86400
    row = conn.execute(
        """
        SELECT 1
        FROM events
        WHERE ts_unix >= ?
          AND (
                action_target LIKE ?
             OR reason LIKE ?
             OR payload_json LIKE ?
          )
        LIMIT 1
        """,
        (window_start, f"%{subject}%", f"%{subject}%", f"%{subject}%"),
    ).fetchone()
    if row:
        return True
    return _ticket_snapshot(subject) is not None


def _ticket_snapshot(subject: str) -> Optional[dict]:
    roots = [
        Path.home() / ".hermes" / "kanban" / "boards",
        Path.home() / ".hermes" / "kanban",
    ]
    candidates: list[Path] = []
    for root in roots:
        if root.is_dir():
            candidates.extend(sorted(root.rglob("kanban.db")))
            candidates.extend(sorted(root.rglob("*.sqlite")))
        elif root.is_file() and root.name.endswith((".db", ".sqlite")):
            candidates.append(root)

    for db_path in candidates:
        try:
            ext = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
            ext.row_factory = sqlite3.Row
        except sqlite3.DatabaseError:
            continue
        try:
            row = ext.execute(
                """
                SELECT id, status, priority,
                       COALESCE(last_heartbeat_at, completed_at, started_at, created_at) AS updated_at
                FROM tasks
                WHERE id = ?
                LIMIT 1
                """,
                (subject,),
            ).fetchone()
            if row:
                return dict(row)
        except sqlite3.DatabaseError:
            pass
        finally:
            ext.close()
    return None


def _evidence_exists(conn: sqlite3.Connection, subject: str) -> bool:
    if subject:
        event_row = conn.execute(
            """
            SELECT 1
            FROM events
            WHERE action_target LIKE ?
               OR reason LIKE ?
               OR payload_json LIKE ?
            LIMIT 1
            """,
            (f"%{subject}%", f"%{subject}%", f"%{subject}%"),
        ).fetchone()
        if event_row:
            return True
    snapshot = _ticket_snapshot(subject)
    return snapshot is not None


def _recently_reported_disagreement(conn: sqlite3.Connection, subject: str, claim_set: list[str], remind_every_days: int) -> bool:
    if not _table_exists(conn, "findings"):
        return False
    since_ts = int(_utc_now().timestamp()) - remind_every_days * 86400
    payload = json.dumps(sorted(claim_set))
    row = conn.execute(
        """
        SELECT 1
        FROM findings
        WHERE detector = 'belief_cross_agent_disagreement'
          AND ts_unix >= ?
          AND body LIKE ?
          AND body LIKE ?
        LIMIT 1
        """,
        (since_ts, f'%"{subject}"%', f"%{payload}%"),
    ).fetchone()
    return row is not None


def detect_deny_cluster(
    db_path: str,
    window_seconds: int = 300,
    threshold_count: int = 4,
) -> list[Finding]:
    """Detect N deny events within M-second window.

    Invariant: A cluster is ≥N events at timestamps within window_seconds.
    Boundary: N-1 events at boundary does NOT trigger; N events DOES trigger.
    """
    conn = _get_conn(db_path)
    findings = []

    try:
        rows = conn.execute("""
            SELECT ts_unix, rule_id, agent, action_type
            FROM events
            WHERE allowed = 0
            ORDER BY ts_unix ASC
        """).fetchall()

        if not rows:
            return findings

        i = 0
        while i < len(rows):
            window_start = rows[i]["ts_unix"]
            window_end = window_start + window_seconds

            cluster = []
            j = i
            while j < len(rows) and rows[j]["ts_unix"] < window_end:
                cluster.append(rows[j])
                j += 1

            if len(cluster) >= threshold_count:
                ts = _from_unix(cluster[0]["ts_unix"])
                rule_ids = [r["rule_id"] for r in cluster]
                agents = [r["agent"] for r in cluster]

                findings.append(Finding(
                    detector="deny_cluster",
                    ts=ts,
                    severity="warning",
                    title=f"Deny cluster: {len(cluster)} denies in {window_seconds}s",
                    details={
                        "count": len(cluster),
                        "window_seconds": window_seconds,
                        "rules": list(set(rule_ids)),
                        "agents": list(set(agents)),
                        "start_ts_unix": window_start,
                        "end_ts_unix": window_end,
                    }
                ))

            i = j if j > i + 1 else i + 1

    finally:
        conn.close()

    return findings


def detect_unknown_rate_spike(
    db_path: str,
    window_hours: int = 24,
    threshold_percent: float = 1.0,
) -> list[Finding]:
    """Detect unknown action_type rate >threshold% over window_hours.

    Invariant: (unknown_count / total_count) * 100 > threshold_percent
    Boundary: exactly threshold% does NOT trigger; >threshold% DOES trigger.
    """
    conn = _get_conn(db_path)
    findings = []

    try:
        now_ts = int(_utc_now().timestamp())
        window_start = now_ts - (window_hours * 3600)

        row = conn.execute("""
            SELECT
                COUNT(*) as total,
                SUM(CASE WHEN action_type IS NULL OR action_type = '' THEN 1 ELSE 0 END) as unknown
            FROM events
            WHERE ts_unix >= ? AND ts_unix <= ?
        """, (window_start, now_ts)).fetchone()

        if row and row["total"] > 0:
            total = row["total"]
            unknown = row["unknown"] or 0
            pct = (unknown / total) * 100

            if pct > threshold_percent:
                findings.append(Finding(
                    detector="unknown_rate_spike",
                    ts=_utc_now(),
                    severity="info",
                    title=f"Unknown rate spike: {pct:.2f}% > {threshold_percent}%",
                    details={
                        "unknown_count": unknown,
                        "total_count": total,
                        "unknown_percent": pct,
                        "threshold_percent": threshold_percent,
                        "window_hours": window_hours,
                    }
                ))

    finally:
        conn.close()

    return findings


def detect_agent_failure_run(
    db_path: str,
    agent_name: Optional[str] = None,
    min_failures: int = 3,
) -> list[Finding]:
    """Detect agent with consecutive deny events (failure run).

    Invariant: agent has ≥min_failures deny events in a row (sorted by ts_unix).
    Boundary: min_failures-1 does NOT trigger; min_failures DOES trigger.
    """
    conn = _get_conn(db_path)
    findings = []

    try:
        # Walk the full per-agent timeline (allows + denies) so the
        # "consecutive" semantics are actually consecutive. The prior
        # version queried only denies and counted them across all of
        # time, which would flag an agent that had N denies interleaved
        # with M allows — not a failure run.
        if agent_name:
            query = """
                SELECT ts_unix, agent, rule_id, action_type, allowed
                FROM events
                WHERE agent = ?
                ORDER BY agent ASC, ts_unix ASC, id ASC
            """
            rows = conn.execute(query, (agent_name,)).fetchall()
        else:
            query = """
                SELECT ts_unix, agent, rule_id, action_type, allowed
                FROM events
                ORDER BY agent ASC, ts_unix ASC, id ASC
            """
            rows = conn.execute(query).fetchall()

        if not rows:
            return findings

        current_agent = None
        streak: list = []
        reported_run_keys: set = set()

        def emit(agent: str, events: list) -> None:
            if len(events) < min_failures:
                return
            run_key = (agent, events[0]["ts_unix"], events[-1]["ts_unix"])
            if run_key in reported_run_keys:
                return
            reported_run_keys.add(run_key)
            ts = _from_unix(events[0]["ts_unix"])
            rules = [e["rule_id"] for e in events]
            findings.append(Finding(
                detector="agent_failure_run",
                ts=ts,
                severity="warning",
                title=f"Agent {agent} failure run: {len(events)} consecutive denies",
                details={
                    "agent": agent,
                    "failure_count": len(events),
                    "rules": list(dict.fromkeys(rules)),
                    "min_threshold": min_failures,
                    "first_ts_unix": events[0]["ts_unix"],
                    "last_ts_unix": events[-1]["ts_unix"],
                },
            ))

        for row in rows:
            agent = row["agent"]
            if agent != current_agent:
                if current_agent is not None:
                    emit(current_agent, streak)
                current_agent = agent
                streak = []
            if row["allowed"] == 0:
                streak.append(row)
            else:
                emit(agent, streak)
                streak = []

        if current_agent is not None:
            emit(current_agent, streak)

    finally:
        conn.close()

    return findings


def detect_stuck_flow(
    db_path: str,
    agent_name: Optional[str] = None,
    min_idle_seconds: int = 3600,
) -> list[Finding]:
    """Detect agent with no new events for min_idle_seconds.

    Invariant: agent's last event is >min_idle_seconds ago.
    Boundary: exactly min_idle_seconds ago does NOT trigger; >min_idle_seconds DOES.
    """
    conn = _get_conn(db_path)
    findings = []

    try:
        now_ts = int(_utc_now().timestamp())
        idle_threshold = now_ts - min_idle_seconds

        if agent_name:
            query = """
                SELECT agent, MAX(ts_unix) as last_ts
                FROM events
                WHERE agent = ?
                GROUP BY agent
            """
            rows = conn.execute(query, (agent_name,)).fetchall()
        else:
            query = """
                SELECT agent, MAX(ts_unix) as last_ts
                FROM events
                GROUP BY agent
            """
            rows = conn.execute(query).fetchall()

        for row in rows:
            agent = row["agent"]
            last_ts = row["last_ts"]

            if last_ts and last_ts < idle_threshold:
                idle_duration = now_ts - last_ts
                ts = _from_unix(last_ts)

                findings.append(Finding(
                    detector="stuck_flow",
                    ts=ts,
                    severity="info",
                    title=f"Stuck flow: {agent} idle for {idle_duration}s",
                    details={
                        "agent": agent,
                        "idle_seconds": idle_duration,
                        "idle_threshold_seconds": min_idle_seconds,
                        "last_event_ts_unix": last_ts,
                    }
                ))

    finally:
        conn.close()

    return findings


def detect_stale_belief(
    db_path: str,
    *,
    stale_days: int = 90,
    active_window_days: int = 30,
) -> list[Finding]:
    conn = _get_conn(db_path)
    findings = []
    try:
        if not _table_exists(conn, "beliefs"):
            return findings
        threshold_ts = int((_utc_now() - timedelta(days=stale_days)).timestamp())
        rows = conn.execute(
            """
            SELECT agent, subject, claim, ts_recorded, source_path, schema_version
            FROM beliefs b
            WHERE ts_recorded = (
                SELECT MAX(ts_recorded)
                FROM beliefs newer
                WHERE newer.agent = b.agent AND newer.subject = b.subject
            )
              AND ts_recorded < ?
            ORDER BY ts_recorded ASC
            """
            ,
            (threshold_ts,),
        ).fetchall()
        for row in rows:
            subject = row["subject"]
            if not _recent_ticket_activity(conn, subject, active_window_days=active_window_days):
                continue
            snapshot = _ticket_snapshot(subject)
            details = {
                "agent": row["agent"],
                "subject": subject,
                "claim": row["claim"],
                "source_path": row["source_path"],
                "schema_version": row["schema_version"],
                "stale_days": stale_days,
            }
            title = f"Stale belief: {row['agent']} on {subject}"
            if snapshot is None and subject.lower().startswith("t_"):
                title = f"Orphaned belief: {row['agent']} on deleted {subject}"
                details["orphan"] = True
            findings.append(
                Finding(
                    detector="belief_stale",
                    ts=_from_unix(int(row["ts_recorded"])),
                    severity="warning",
                    title=title,
                    details=details,
                )
            )
    finally:
        conn.close()
    return findings


def detect_cross_agent_disagreement(
    db_path: str,
    *,
    remind_every_days: int = 7,
) -> list[Finding]:
    conn = _get_conn(db_path)
    findings = []
    try:
        if not _table_exists(conn, "beliefs"):
            return findings
        rows = conn.execute(
            """
            SELECT agent, subject, claim, ts_recorded
            FROM beliefs b
            WHERE ts_recorded = (
                SELECT MAX(ts_recorded)
                FROM beliefs newer
                WHERE newer.agent = b.agent AND newer.subject = b.subject
            )
            ORDER BY subject ASC, agent ASC
            """
        ).fetchall()
        grouped: dict[str, list[sqlite3.Row]] = {}
        for row in rows:
            grouped.setdefault(row["subject"], []).append(row)
        for subject, members in grouped.items():
            claims = {}
            for row in members:
                claims[row["agent"]] = _normalize_claim(row["claim"])
            unique_claims = sorted(set(claims.values()))
            if len(unique_claims) < 2:
                continue
            if _recently_reported_disagreement(conn, subject, unique_claims, remind_every_days):
                continue
            kanban = _ticket_snapshot(subject)
            title = f"Cross-agent disagreement: {subject}"
            details = {
                "subject": subject,
                "claims": unique_claims,
                "agents": {row["agent"]: row["claim"] for row in members},
            }
            if kanban is not None:
                details["kanban"] = {
                    "status": kanban.get("status"),
                    "priority": f"P{kanban.get('priority')}" if kanban.get("priority") is not None else None,
                }
            findings.append(
                Finding(
                    detector="belief_cross_agent_disagreement",
                    ts=max(_from_unix(int(row["ts_recorded"])) for row in members),
                    severity="critical",
                    title=title,
                    details=details,
                )
            )
    finally:
        conn.close()
    return findings


def detect_belief_without_evidence(db_path: str) -> list[Finding]:
    conn = _get_conn(db_path)
    findings = []
    try:
        if not _table_exists(conn, "beliefs"):
            return findings
        rows = conn.execute(
            """
            SELECT agent, subject, claim, ts_recorded, source_path
            FROM beliefs
            ORDER BY ts_recorded DESC
            """
        ).fetchall()
        seen: set[tuple[str, str]] = set()
        for row in rows:
            key = (row["agent"], row["subject"])
            if key in seen:
                continue
            seen.add(key)
            subject = row["subject"]
            if _evidence_exists(conn, subject):
                continue
            title = f"Belief without evidence: {row['agent']} on {subject}"
            details = {
                "agent": row["agent"],
                "subject": subject,
                "claim": row["claim"],
                "source_path": row["source_path"],
            }
            if subject.lower().startswith("t_") and _ticket_snapshot(subject) is None:
                details["orphan"] = True
            findings.append(
                Finding(
                    detector="belief_without_evidence",
                    ts=_from_unix(int(row["ts_recorded"])),
                    severity="warning",
                    title=title,
                    details=details,
                )
            )
    finally:
        conn.close()
    return findings


def detect_reality_without_belief(
    db_path: str,
    *,
    window_days: int = 30,
) -> list[Finding]:
    conn = _get_conn(db_path)
    findings = []
    try:
        if not _table_exists(conn, "beliefs"):
            return findings
        threshold_ts = int((_utc_now() - timedelta(days=window_days)).timestamp())
        rows = conn.execute(
            """
            SELECT ts_unix, action_target, reason, payload_json
            FROM events
            WHERE ts_unix >= ?
            ORDER BY ts_unix DESC
            """
            ,
            (threshold_ts,),
        ).fetchall()
        seen_subjects: set[str] = set()
        for row in rows:
            subjects = []
            for value in (row["action_target"], row["reason"], row["payload_json"]):
                subjects.extend(_extract_subjects(value))
            for subject in subjects:
                if subject in seen_subjects:
                    continue
                seen_subjects.add(subject)
                belief_row = conn.execute(
                    "SELECT 1 FROM beliefs WHERE subject = ? LIMIT 1",
                    (subject,),
                ).fetchone()
                if belief_row:
                    continue
                findings.append(
                    Finding(
                        detector="reality_without_belief",
                        ts=_from_unix(int(row["ts_unix"])),
                        severity="info",
                        title=f"Reality without belief: {subject}",
                        details={"subject": subject, "event_ts_unix": row["ts_unix"]},
                    )
                )
    finally:
        conn.close()
    return findings


def run_all_detectors(db_path: str) -> list[Finding]:
    """Run all detectors and return all findings."""
    findings = []
    findings.extend(detect_deny_cluster(db_path, window_seconds=300, threshold_count=4))
    findings.extend(detect_unknown_rate_spike(db_path, window_hours=24, threshold_percent=1.0))
    findings.extend(detect_agent_failure_run(db_path, min_failures=3))
    findings.extend(detect_stuck_flow(db_path, min_idle_seconds=3600))
    findings.extend(detect_stale_belief(db_path))
    findings.extend(detect_cross_agent_disagreement(db_path))
    findings.extend(detect_belief_without_evidence(db_path))
    findings.extend(detect_reality_without_belief(db_path))
    return sorted(findings, key=lambda f: f.ts, reverse=True)
