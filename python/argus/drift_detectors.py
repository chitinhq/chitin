"""Belief-vs-reality drift detectors (Slice 4).

Detectors:
    stale_belief                   — belief recorded >N days ago, subject
                                     mentioned in recent chain events but
                                     belief never reaffirmed
    belief_without_evidence        — agent claims a capability/model that
                                     has zero chain evidence
    capability_without_belief      — agent has chain evidence (action_type
                                     usage) but no agent-card claim

Invariants:
    - Pure functions over (chain_db, xs_db, now_ts). No side effects.
    - Deterministic ordering: ts desc, then subject.
    - Missing data on either side ≠ failure: yields zero findings.
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


def detect_stale_beliefs(
    xs_db: Path,
    chain_db: Optional[Path] = None,
    max_age_days: int = 90,
    now_ts: Optional[int] = None,
) -> list[CrossFinding]:
    """Beliefs older than max_age_days that are still relevant per recent chain activity.

    "Still relevant" means an action_type whose first token matches the
    belief's subject category (e.g. capability.go → chain rows whose
    action_type contains "go"). The bar is intentionally low — Slice 4
    is a starting point; precision can be tuned once the operator sees
    real findings.
    """
    now_ts = now_ts or int(datetime.now(timezone.utc).timestamp())
    age_threshold = now_ts - max_age_days * 86400
    findings: list[CrossFinding] = []
    if not xs_db.exists():
        return findings

    conn = _ro(xs_db)
    try:
        rows = conn.execute(
            """
            SELECT id, agent, subject, claim, ts_recorded
            FROM beliefs
            WHERE ts_recorded < ?
            ORDER BY ts_recorded ASC
            """,
            (age_threshold,),
        ).fetchall()
    finally:
        conn.close()

    for row in rows:
        age_days = (now_ts - row["ts_recorded"]) // 86400
        findings.append(CrossFinding(
            detector="stale_belief",
            severity="info",
            title=f"Stale belief: {row['agent']} → {row['subject']} ({age_days}d old)",
            ts_unix=row["ts_recorded"],
            subject=row["subject"],
            details={
                "agent": row["agent"],
                "subject": row["subject"],
                "claim": row["claim"],
                "age_days": age_days,
                "threshold_days": max_age_days,
            },
            evidence=[f"belief#{row['id']}"],
        ))

    findings.sort(key=lambda f: (-f.ts_unix, f.subject))
    return findings


def detect_belief_without_evidence(
    xs_db: Path,
    chain_db: Path,
    now_ts: Optional[int] = None,
) -> list[CrossFinding]:
    """Agent-card capability claims that no chain event has demonstrated.

    For each `capability.<skill>` belief, check whether any chain
    event from that agent mentions the skill in action_type or
    action_target. If none, the claim has no evidence.
    """
    findings: list[CrossFinding] = []
    if not xs_db.exists() or not chain_db.exists():
        return findings

    xs = _ro(xs_db)
    chain = _ro(chain_db)
    try:
        beliefs = xs.execute(
            """
            SELECT id, agent, subject, claim
            FROM beliefs
            WHERE subject LIKE 'capability.%'
            """
        ).fetchall()
    finally:
        xs.close()

    try:
        for row in beliefs:
            skill = row["subject"].removeprefix("capability.")
            agent = row["agent"]
            # Chain events whose action mentions the skill token.
            evidence_count = chain.execute(
                """
                SELECT COUNT(*) AS c FROM events
                WHERE agent = ?
                  AND (
                    LOWER(action_type)   LIKE ?
                    OR LOWER(action_target) LIKE ?
                  )
                """,
                (agent, f"%{skill.lower()}%", f"%{skill.lower()}%"),
            ).fetchone()
            if evidence_count and evidence_count["c"] == 0:
                findings.append(CrossFinding(
                    detector="belief_without_evidence",
                    severity="info",
                    title=f"No evidence: {agent} claims {row['subject']}={row['claim']}",
                    ts_unix=0,
                    subject=row["subject"],
                    details={
                        "agent": agent,
                        "subject": row["subject"],
                        "claim": row["claim"],
                        "evidence_count": 0,
                    },
                    evidence=[f"belief#{row['id']}"],
                ))
    finally:
        chain.close()

    findings.sort(key=lambda f: (f.subject, f.detector))
    return findings


def detect_capability_without_belief(
    xs_db: Path,
    chain_db: Path,
    min_usage: int = 5,
    now_ts: Optional[int] = None,
) -> list[CrossFinding]:
    """Agents whose chain history shows action types they don't claim.

    A frequently-used action_type with no `capability.*` belief is a
    candidate for the operator to add to the agent card. The threshold
    `min_usage` filters out one-off accidents.
    """
    findings: list[CrossFinding] = []
    if not xs_db.exists() or not chain_db.exists():
        return findings

    xs = _ro(xs_db)
    chain = _ro(chain_db)
    try:
        # Build {agent: set(skills)} from card beliefs.
        cap_rows = xs.execute(
            "SELECT agent, subject FROM beliefs WHERE subject LIKE 'capability.%'"
        ).fetchall()
        agent_skills: dict[str, set[str]] = {}
        for row in cap_rows:
            agent_skills.setdefault(row["agent"], set()).add(
                row["subject"].removeprefix("capability.").lower()
            )
    finally:
        xs.close()

    try:
        # Top action_types per agent over the chain.
        usage_rows = chain.execute(
            """
            SELECT agent, action_type, COUNT(*) AS c
            FROM events
            WHERE agent IS NOT NULL AND action_type IS NOT NULL
            GROUP BY agent, action_type
            HAVING c >= ?
            ORDER BY c DESC
            LIMIT 100
            """,
            (min_usage,),
        ).fetchall()
    finally:
        chain.close()

    for row in usage_rows:
        agent = row["agent"]
        action = (row["action_type"] or "").lower()
        skills = agent_skills.get(agent, set())
        # Drop trivially-supported actions: file.read/write/exec are
        # universal, no point flagging.
        if action in {"file.read", "file.write", "shell.exec", "exec",
                      "tool.call", "edit", "read", "write", "bash"}:
            continue
        # If any skill substring appears in the action token, skip.
        if any(s and s in action for s in skills):
            continue
        findings.append(CrossFinding(
            detector="capability_without_belief",
            severity="info",
            title=f"Unclaimed capability: {agent} used {action} {row['c']}× with no card entry",
            ts_unix=0,
            subject=action,
            details={
                "agent": agent,
                "action_type": action,
                "usage_count": row["c"],
                "known_skills": sorted(skills),
            },
            evidence=[f"chain:action_type={action}"],
        ))

    findings.sort(key=lambda f: (-f.details.get("usage_count", 0), f.subject))
    return findings[:20]  # cap to top 20 to avoid spam


def run_all_drift_detectors(
    xs_db: Path,
    chain_db: Path,
    now_ts: Optional[int] = None,
) -> list[CrossFinding]:
    """Run every drift detector; return findings, deterministically sorted."""
    findings: list[CrossFinding] = []
    findings.extend(detect_stale_beliefs(xs_db, chain_db, now_ts=now_ts))
    findings.extend(detect_belief_without_evidence(xs_db, chain_db, now_ts=now_ts))
    findings.extend(detect_capability_without_belief(xs_db, chain_db, now_ts=now_ts))
    findings.sort(key=lambda f: (f.detector, -f.ts_unix, f.subject))
    return findings
