"""Deterministic detectors over the event index."""
from __future__ import annotations

import json
import re
import sqlite3
from collections import defaultdict
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from typing import Any, Optional

from argus import migrations


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
            WHERE source = 'chain' AND allowed = 0
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
            WHERE source = 'chain' AND ts_unix >= ? AND ts_unix <= ?
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
                WHERE source = 'chain' AND agent = ?
                ORDER BY agent ASC, ts_unix ASC, id ASC
            """
            rows = conn.execute(query, (agent_name,)).fetchall()
        else:
            query = """
                SELECT ts_unix, agent, rule_id, action_type, allowed
                FROM events
                WHERE source = 'chain'
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
                WHERE source = 'chain' AND agent = ?
                GROUP BY agent
            """
            rows = conn.execute(query, (agent_name,)).fetchall()
        else:
            query = """
                SELECT agent, MAX(ts_unix) as last_ts
                FROM events
                WHERE source = 'chain'
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


def detect_hermes_standup_gap(
    db_path: str,
    *,
    max_gap_hours: int = 8,
) -> list[Finding]:
    """Detect >8h gaps between consecutive Hermes standups."""
    conn = _get_conn(db_path)
    findings = []
    try:
        rows = conn.execute(
            """
            SELECT ts_unix, source_ref
            FROM events
            WHERE source = 'hermes' AND kind = 'hermes_standup'
            ORDER BY ts_unix ASC, id ASC
            """
        ).fetchall()
        if len(rows) < 2:
            return findings
        max_gap_seconds = max_gap_hours * 3600
        for prev, curr in zip(rows, rows[1:]):
            gap = int(curr["ts_unix"]) - int(prev["ts_unix"])
            if gap > max_gap_seconds:
                findings.append(
                    Finding(
                        detector="hermes_standup_gap",
                        ts=_from_unix(curr["ts_unix"]),
                        severity="warning",
                        title=f"Hermes standup gap: {gap // 3600}h without a standup",
                        details={
                            "gap_seconds": gap,
                            "threshold_seconds": max_gap_seconds,
                            "previous_source_ref": prev["source_ref"],
                            "current_source_ref": curr["source_ref"],
                        },
                    )
                )
    finally:
        conn.close()
    return findings


def detect_openclaw_workflow_failure_correlation(
    db_path: str,
    *,
    block_window_seconds: int = 3600,
) -> list[Finding]:
    """Detect workflow failures that did or did not correlate to kanban-flow block."""
    conn = _get_conn(db_path)
    findings = []
    try:
        failures = conn.execute(
            """
            SELECT ts_unix, subject, payload_json, source_ref
            FROM events
            WHERE source = 'openclaw' AND kind = 'openclaw_workflow_failure'
            ORDER BY ts_unix DESC, id DESC
            """
        ).fetchall()
        for failure in failures:
            subject = failure["subject"] or ""
            block = conn.execute(
                """
                SELECT id, ts_unix, source_ref
                FROM events
                WHERE source = 'chain'
                  AND action_target LIKE '%kanban-flow block%'
                  AND (? = '' OR action_target LIKE '%' || ? || '%')
                  AND ts_unix BETWEEN ? AND ?
                ORDER BY ts_unix ASC, id ASC
                LIMIT 1
                """,
                (
                    subject,
                    subject,
                    int(failure["ts_unix"]),
                    int(failure["ts_unix"]) + block_window_seconds,
                ),
            ).fetchone()
            severity = "info" if block else "warning"
            title = (
                f"Openclaw workflow failure correlated to kanban-flow block for {subject or 'unknown ticket'}"
                if block
                else f"Openclaw workflow failure without kanban-flow block for {subject or 'unknown ticket'}"
            )
            findings.append(
                Finding(
                    detector="openclaw_workflow_failure_correlation",
                    ts=_from_unix(failure["ts_unix"]),
                    severity=severity,
                    title=title,
                    details={
                        "ticket": subject or None,
                        "source_ref": failure["source_ref"],
                        "block_found": bool(block),
                        "block_source_ref": block["source_ref"] if block else None,
                        "block_window_seconds": block_window_seconds,
                    },
                )
            )
    finally:
        conn.close()
    return findings


def detect_discord_narration_gap(
    db_path: str,
    *,
    announce_window_seconds: int = 900,
) -> list[Finding]:
    """Detect dispatched tickets with no `#clawta` announce."""
    conn = _get_conn(db_path)
    findings = []
    try:
        dispatches = conn.execute(
            """
            SELECT ts_unix, subject, source_ref
            FROM events
            WHERE source = 'openclaw' AND kind = 'openclaw_dispatch'
            ORDER BY ts_unix DESC, id DESC
            """
        ).fetchall()
        for dispatch in dispatches:
            subject = dispatch["subject"] or ""
            announce = conn.execute(
                """
                SELECT id, source_ref
                FROM events
                WHERE source = 'discord'
                  AND kind = 'discord_clawta_announce'
                  AND subject = ?
                  AND ts_unix BETWEEN ? AND ?
                ORDER BY ts_unix ASC, id ASC
                LIMIT 1
                """,
                (
                    subject,
                    int(dispatch["ts_unix"]),
                    int(dispatch["ts_unix"]) + announce_window_seconds,
                ),
            ).fetchone()
            if announce:
                continue
            findings.append(
                Finding(
                    detector="discord_narration_gap",
                    ts=_from_unix(dispatch["ts_unix"]),
                    severity="warning",
                    title=f"Dispatch {subject or 'unknown'} missing #clawta announce",
                    details={
                        "ticket": subject or None,
                        "dispatch_source_ref": dispatch["source_ref"],
                        "announce_window_seconds": announce_window_seconds,
                    },
                )
            )
    finally:
        conn.close()
    return findings


def _parse_payload(row: sqlite3.Row) -> dict:
    try:
        return json.loads(row["payload_json"] or "{}")
    except json.JSONDecodeError:
        return {}


def _event_ref(row: sqlite3.Row) -> str:
    return row["external_id"] or f"{row['source']}:{row['id']}"


def detect_demote_loop(
    db_path: str,
    window_hours: int = 24,
    threshold_count: int = 2,
) -> list[Finding]:
    """Detect repeated ready/todo -> triage demotions in a rolling window."""
    conn = _get_conn(db_path)
    findings = []
    try:
        since_ts = int((_utc_now() - timedelta(hours=window_hours)).timestamp())
        rows = conn.execute(
            """
            SELECT id, external_id, ts_unix, ticket_id, board, status, payload_json
              FROM events
             WHERE source = 'kanban'
               AND kind = 'kanban_status_transition'
               AND ts_unix >= ?
             ORDER BY ticket_id ASC, ts_unix ASC, id ASC
            """,
            (since_ts,),
        ).fetchall()
        by_ticket: dict[str, list[sqlite3.Row]] = defaultdict(list)
        for row in rows:
            payload = _parse_payload(row)
            if row["ticket_id"] and row["status"] == "triage" and payload.get("from") in {"ready", "todo"}:
                by_ticket[row["ticket_id"]].append(row)
        for ticket_id, events in by_ticket.items():
            if len(events) < threshold_count:
                continue
            findings.append(Finding(
                detector="demote_loop",
                ts=_from_unix(events[-1]["ts_unix"]),
                severity="warning",
                title=f"Ticket {ticket_id} demoted to triage {len(events)} times in {window_hours}h",
                details={
                    "ticket_id": ticket_id,
                    "board": events[-1]["board"],
                    "demote_count": len(events),
                    "window_hours": window_hours,
                    "citations": [_event_ref(event) for event in events],
                },
            ))
    finally:
        conn.close()
    return findings


def detect_stuck_pr_green_ci(
    db_path: str,
    min_open_seconds: int = 24 * 3600,
) -> list[Finding]:
    """Detect open PRs with green CI that have sat untouched for >24h."""
    conn = _get_conn(db_path)
    findings = []
    try:
        now_ts = int(_utc_now().timestamp())
        prs = conn.execute(
            """
            SELECT id, external_id, ts_unix, last_seen_ts, repo, pr_number, ticket_id, status, subject, payload_json
              FROM events
             WHERE source = 'git' AND kind = 'git_pr_opened'
             ORDER BY ts_unix ASC
            """
        ).fetchall()
        for pr in prs:
            if pr["status"] not in {"OPEN", "open"}:
                continue
            merged = conn.execute(
                """
                SELECT 1 FROM events
                 WHERE source = 'git' AND kind = 'git_pr_merged' AND repo = ? AND pr_number = ?
                 LIMIT 1
                """,
                (pr["repo"], pr["pr_number"]),
            ).fetchone()
            if merged:
                continue
            payload = _parse_payload(pr)
            ci_state = payload.get("ci_state")
            age = now_ts - int(pr["ts_unix"])
            idle = now_ts - int(pr["last_seen_ts"] or pr["ts_unix"])
            if ci_state != "green" or age <= min_open_seconds or idle <= min_open_seconds:
                continue
            citations = [_event_ref(pr)]
            ticket_ref = None
            if pr["ticket_id"]:
                ticket_ref = conn.execute(
                    """
                    SELECT external_id, id
                      FROM events
                     WHERE source = 'kanban' AND ticket_id = ?
                     ORDER BY ts_unix ASC, id ASC LIMIT 1
                    """,
                    (pr["ticket_id"],),
                ).fetchone()
            if ticket_ref:
                citations.append(ticket_ref["external_id"] or f"kanban:{ticket_ref['id']}")
            findings.append(Finding(
                detector="stuck_pr_green_ci",
                ts=_from_unix(pr["ts_unix"]),
                severity="critical",
                title=f"PR #{pr['pr_number']} in {pr['repo']} is green but stuck >24h",
                details={
                    "repo": pr["repo"],
                    "pr_number": pr["pr_number"],
                    "ticket_id": pr["ticket_id"],
                    "title": pr["subject"],
                    "open_seconds": age,
                    "idle_seconds": idle,
                    "ci_state": ci_state,
                    "citations": citations,
                },
            ))
    finally:
        conn.close()
    return findings


def detect_follow_up_clustering(
    db_path: str,
    window_days: int = 7,
    threshold_count: int = 2,
) -> list[Finding]:
    """Detect repeated follow-up tickets on files touched by recently merged PRs."""
    conn = _get_conn(db_path)
    findings = []
    try:
        since_ts = int((_utc_now() - timedelta(days=30)).timestamp())
        merged_prs = conn.execute(
            """
            SELECT id, external_id, repo, pr_number, ticket_id, ts_unix, payload_json
              FROM events
             WHERE source = 'git' AND kind = 'git_pr_merged' AND ts_unix >= ?
             ORDER BY ts_unix DESC
            """,
            (since_ts,),
        ).fetchall()
        for pr in merged_prs:
            payload = _parse_payload(pr)
            files = [f for f in payload.get("files", []) if isinstance(f, str)]
            if not files:
                continue
            window_end = pr["ts_unix"] + (window_days * 86400)
            for path in files:
                pattern = f"%{path}%"
                tickets = conn.execute(
                    """
                    SELECT id, external_id, ticket_id, ts_unix
                      FROM events
                     WHERE source = 'kanban'
                       AND kind = 'kanban_ticket_create'
                       AND ts_unix > ? AND ts_unix <= ?
                       AND (subject LIKE ? OR COALESCE(payload_json, '') LIKE ?)
                     ORDER BY ts_unix ASC
                    """,
                    (pr["ts_unix"], window_end, pattern, pattern),
                ).fetchall()
                if len(tickets) < threshold_count:
                    continue
                findings.append(Finding(
                    detector="follow_up_clustering",
                    ts=_from_unix(tickets[-1]["ts_unix"]),
                    severity="warning",
                    title=f"Follow-up cluster on {path}: {len(tickets)} tickets after PR #{pr['pr_number']}",
                    details={
                        "repo": pr["repo"],
                        "pr_number": pr["pr_number"],
                        "file_path": path,
                        "ticket_ids": [t["ticket_id"] for t in tickets],
                        "window_days": window_days,
                        "citations": [_event_ref(pr), *[_event_ref(t) for t in tickets]],
                    },
                ))
    finally:
        conn.close()
    return findings


def detect_lore_drift(
    db_path: str,
    window_days: int = 30,
    threshold_count: int = 2,
) -> list[Finding]:
    """Detect repeated corrective comments pointing at the same misconception."""
    conn = _get_conn(db_path)
    findings = []
    try:
        since_ts = int((_utc_now() - timedelta(days=window_days)).timestamp())
        rows = conn.execute(
            """
            SELECT id, external_id, ticket_id, ts_unix, payload_json
              FROM events
             WHERE source = 'kanban' AND kind = 'kanban_comment' AND ts_unix >= ?
             ORDER BY ts_unix ASC
            """,
            (since_ts,),
        ).fetchall()
        buckets: dict[str, list[sqlite3.Row]] = defaultdict(list)
        for row in rows:
            payload = _parse_payload(row)
            body = str(payload.get("body") or "")
            clue = None
            if "`" in body:
                code_terms = re.findall(r"`([^`]+)`", body)
                if code_terms:
                    clue = code_terms[0].lower()
            if clue is None:
                lowered = body.lower()
                if "correction" in lowered or "clarif" in lowered or "actually" in lowered:
                    words = [w for w in re.findall(r"[a-zA-Z_]{4,}", lowered) if w not in {"actually", "correction", "clarify", "clarification", "because", "should", "there", "their"}]
                    if words:
                        clue = " ".join(words[:4])
            if clue:
                buckets[clue].append(row)
        for clue, comments in buckets.items():
            if len(comments) < threshold_count:
                continue
            findings.append(Finding(
                detector="lore_drift",
                ts=_from_unix(comments[-1]["ts_unix"]),
                severity="warning",
                title=f"Lore drift around '{clue}' repeated {len(comments)} times",
                details={
                    "topic": clue,
                    "ticket_ids": [c["ticket_id"] for c in comments],
                    "comment_count": len(comments),
                    "citations": [_event_ref(c) for c in comments],
                },
            ))
    finally:
        conn.close()
    return findings


def detect_time_to_merge_regression(
    db_path: str,
    recent_days: int = 7,
    baseline_days: int = 30,
    regression_factor: float = 2.0,
) -> list[Finding]:
    """Detect driver-level merge latency regression without divide-by-zero."""
    conn = _get_conn(db_path)
    findings = []
    try:
        now_ts = int(_utc_now().timestamp())
        since_ts = now_ts - (baseline_days * 86400)
        rows = conn.execute(
            """
            SELECT opened.repo, opened.pr_number, opened.ticket_id, opened.payload_json, opened.ts_unix AS opened_ts,
                   merged.ts_unix AS merged_ts, merged.external_id AS merged_external_id
              FROM events opened
              JOIN events merged
                ON opened.repo = merged.repo
               AND opened.pr_number = merged.pr_number
             WHERE opened.source = 'git'
               AND opened.kind = 'git_pr_opened'
               AND merged.source = 'git'
               AND merged.kind = 'git_pr_merged'
               AND merged.ts_unix >= ?
            """,
            (since_ts,),
        ).fetchall()
        by_driver: dict[str, list[dict[str, Any]]] = defaultdict(list)
        for row in rows:
            payload = _parse_payload(row)
            driver = payload.get("driver") or "unknown"
            duration = int(row["merged_ts"]) - int(row["opened_ts"])
            by_driver[driver].append({"duration": duration, "row": row})
        for driver, samples in by_driver.items():
            recent = [s for s in samples if s["row"]["merged_ts"] >= now_ts - (recent_days * 86400)]
            baseline = samples
            if not recent or not baseline:
                continue
            recent_durations = sorted(s["duration"] for s in recent)
            baseline_durations = sorted(s["duration"] for s in baseline)
            recent_median = recent_durations[len(recent_durations) // 2]
            baseline_median = baseline_durations[len(baseline_durations) // 2]
            if baseline_median <= 0:
                continue
            if recent_median <= baseline_median * regression_factor:
                continue
            citations = [s["row"]["merged_external_id"] or f"git:{s['row']['repo']}:{s['row']['pr_number']}:merged" for s in recent[:5]]
            ticket_cites = []
            for s in recent[:5]:
                ticket_id = s["row"]["ticket_id"]
                if not ticket_id:
                    continue
                ticket = conn.execute(
                    """
                    SELECT external_id, id
                      FROM events
                     WHERE source = 'kanban' AND ticket_id = ?
                     ORDER BY ts_unix ASC, id ASC LIMIT 1
                    """,
                    (ticket_id,),
                ).fetchone()
                if ticket:
                    ticket_cites.append(ticket["external_id"] or f"kanban:{ticket['id']}")
            findings.append(Finding(
                detector="time_to_merge_regression",
                ts=_utc_now(),
                severity="warning",
                title=f"Driver {driver} merge time regressed: 7d median > {regression_factor}x 30d",
                details={
                    "driver": driver,
                    "recent_days": recent_days,
                    "baseline_days": baseline_days,
                    "recent_median_seconds": recent_median,
                    "baseline_median_seconds": baseline_median,
                    "citations": list(dict.fromkeys([*citations, *ticket_cites])),
                },
            ))
    finally:
        conn.close()
    return findings


def run_all_detectors(db_path: str) -> list[Finding]:
    """Run all detectors and return all findings.

    Several detectors query the `source` / `kind` columns added by
    Slice-3 migrations. A scheduled report against a pre-Slice-3 index
    would otherwise fail with `no such column: source`, so bring the
    schema current before any detector runs.
    """
    conn = sqlite3.connect(db_path)
    try:
        migrations.apply_pending(conn)
    finally:
        conn.close()

    findings = []
    findings.extend(detect_deny_cluster(db_path, window_seconds=300, threshold_count=4))
    findings.extend(detect_unknown_rate_spike(db_path, window_hours=24, threshold_percent=1.0))
    findings.extend(detect_agent_failure_run(db_path, min_failures=3))
    findings.extend(detect_stuck_flow(db_path, min_idle_seconds=3600))
    findings.extend(detect_hermes_standup_gap(db_path, max_gap_hours=8))
    findings.extend(detect_openclaw_workflow_failure_correlation(db_path, block_window_seconds=3600))
    findings.extend(detect_discord_narration_gap(db_path, announce_window_seconds=900))
    findings.extend(detect_demote_loop(db_path, window_hours=24, threshold_count=2))
    findings.extend(detect_stuck_pr_green_ci(db_path, min_open_seconds=24 * 3600))
    findings.extend(detect_follow_up_clustering(db_path, window_days=7, threshold_count=2))
    findings.extend(detect_lore_drift(db_path, window_days=30, threshold_count=2))
    findings.extend(detect_time_to_merge_regression(db_path, recent_days=7, baseline_days=30, regression_factor=2.0))
    return sorted(findings, key=lambda f: f.ts, reverse=True)
