"""Deterministic detectors over the event index."""
from __future__ import annotations

import sqlite3
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
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


def run_all_detectors(db_path: str) -> list[Finding]:
    """Run all detectors and return all findings."""
    findings = []
    findings.extend(detect_deny_cluster(db_path, window_seconds=300, threshold_count=4))
    findings.extend(detect_unknown_rate_spike(db_path, window_hours=24, threshold_percent=1.0))
    findings.extend(detect_agent_failure_run(db_path, min_failures=3))
    findings.extend(detect_stuck_flow(db_path, min_idle_seconds=3600))
    return sorted(findings, key=lambda f: f.ts, reverse=True)
