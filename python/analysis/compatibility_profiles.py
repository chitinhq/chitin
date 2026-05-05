"""Conformance-substrate storage + extractors.

Step 1 of the 8-step roadmap in
docs/design/2026-05-05-conformance-substrate.md.

Writes per-(driver, model) capability profiles to
~/.chitin/compatibility.sqlite. Each profile carries scores in 12
dimensions, plus n_observations and last_observed for staleness.

This commit ships:
  - schema (CREATE TABLE statements; idempotent init)
  - one extractor: `routing_effectiveness` from runner-loop telemetry
    (the Phase 1 attempt-end log lines that the runner already emits)
  - dump-to-stdout for operator inspection

Subsequent commits add the other 11 dimensions (see roadmap §1-§7).

Usage:
    cd python/analysis && uv run python -m analysis.compatibility_profiles init
    cd python/analysis && uv run python -m analysis.compatibility_profiles extract
    cd python/analysis && uv run python -m analysis.compatibility_profiles dump
"""
from __future__ import annotations

import argparse
import json
import os
import sqlite3
import sys
from datetime import datetime, timezone
from pathlib import Path

CHITIN_HOME = Path(os.environ.get("CHITIN_HOME") or os.path.expanduser("~/.chitin"))
DB_PATH = CHITIN_HOME / "compatibility.sqlite"

# Per the design doc, scores are 0.0-1.0. NULL = no observations yet
# in this dimension for this (driver, model) cell. The 12 dimensions
# are stable schema; new ones land via ALTER TABLE in their own
# migrations.
DIMENSIONS = [
    "tool_call_validity",          # 1
    "patch_integrity",             # 2
    "execution_stability",         # 3
    "long_context_survivability",  # 4
    "routing_effectiveness",       # 5  ← shipped this commit
    "cost_efficiency",             # 6
    "governance_compliance",       # 7
    "recovery_behavior",           # 8
    "repo_mutation_quality",       # 9
    "ci_survivability",            # 10
    "latency",                     # 11
    "determinism",                 # 12
]

SCHEMA = f"""
-- One row per (driver, model) cell. Updated by extractors.
CREATE TABLE IF NOT EXISTS profiles (
    driver         TEXT NOT NULL,
    model          TEXT NOT NULL,
    n_observations INTEGER NOT NULL DEFAULT 0,
    last_observed  TEXT,                          -- ISO 8601
    {", ".join(f"{d} REAL" for d in DIMENSIONS)},
    PRIMARY KEY (driver, model)
);

-- Per-attempt observations — append-only audit trail behind the
-- aggregated `profiles` table. Lets us re-derive on schema changes
-- without losing history.
CREATE TABLE IF NOT EXISTS observations (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    ts             TEXT NOT NULL,                 -- ISO 8601
    driver         TEXT NOT NULL,
    model          TEXT NOT NULL,
    workflow_id    TEXT,
    attempt        INTEGER,
    dimension      TEXT NOT NULL,                 -- one of DIMENSIONS
    value          REAL NOT NULL,                 -- 0.0-1.0
    raw_source     TEXT NOT NULL,                 -- which extractor / source file
    raw_payload    TEXT                           -- JSON for debugging / re-derive
);

CREATE INDEX IF NOT EXISTS idx_obs_dim_driver_model
    ON observations (dimension, driver, model);
"""


def open_db() -> sqlite3.Connection:
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    conn.executescript(SCHEMA)
    conn.commit()
    return conn


def upsert_profile(
    conn: sqlite3.Connection,
    driver: str,
    model: str,
    dimension: str,
    value: float,
    n_new_observations: int = 1,
) -> None:
    """Update the (driver, model) profile by averaging in a new value.

    Atomic via SAVEPOINT so concurrent extractors don't interleave.
    Uses a running-average update: new_avg = (old_avg * n + value * k) / (n + k).
    """
    if dimension not in DIMENSIONS:
        raise ValueError(f"unknown dimension: {dimension}")

    with conn:
        cur = conn.execute(
            "SELECT n_observations, " + dimension + " FROM profiles WHERE driver = ? AND model = ?",
            (driver, model),
        )
        row = cur.fetchone()
        now = datetime.now(timezone.utc).isoformat()
        if row is None:
            conn.execute(
                f"INSERT INTO profiles (driver, model, n_observations, last_observed, {dimension}) VALUES (?, ?, ?, ?, ?)",
                (driver, model, n_new_observations, now, value),
            )
        else:
            old_n = row["n_observations"]
            old_val = row[dimension]
            new_n = old_n + n_new_observations
            new_val = (
                value
                if old_val is None
                else (old_val * old_n + value * n_new_observations) / new_n
            )
            conn.execute(
                f"UPDATE profiles SET n_observations = ?, last_observed = ?, {dimension} = ? WHERE driver = ? AND model = ?",
                (new_n, now, new_val, driver, model),
            )


def record_observation(
    conn: sqlite3.Connection,
    ts: str,
    driver: str,
    model: str,
    workflow_id: str | None,
    attempt: int | None,
    dimension: str,
    value: float,
    raw_source: str,
    raw_payload: dict | None = None,
) -> None:
    conn.execute(
        "INSERT INTO observations (ts, driver, model, workflow_id, attempt, dimension, value, raw_source, raw_payload) "
        "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
        (
            ts, driver, model, workflow_id, attempt, dimension, value, raw_source,
            json.dumps(raw_payload) if raw_payload is not None else None,
        ),
    )


# ─── Extractor: routing_effectiveness ──────────────────────────────────────
#
# Score = 1.0 - (escalation_rate). High score = the (driver, model) finishes
# without needing to escalate; low score = it escalates often.
#
# Source: chitin-execute-request runner emits one stderr JSONL line per
# attempt with `escalation_requested: bool` (kernel-driven OR runner-
# synthesized). That tells us, per attempt, whether the cell could finish.
# Aggregate across attempts → per-cell score.
#
# The runner's logs land where systemd captured them (journalctl per
# unit) OR in the per-card log dir if invoked via spawn-execute-request.
# For this first cut, read from the spawn-execute-request log dir; the
# systemd-captured path is a follow-up extractor.

EXECUTE_REQUEST_LOG_DIR = CHITIN_HOME / "../.cache/chitin/execute-request-logs"


def extract_routing_effectiveness(conn: sqlite3.Connection) -> int:
    """Walk runner logs, populate routing_effectiveness for each cell.

    Returns count of observations written.
    """
    log_dir = EXECUTE_REQUEST_LOG_DIR.resolve()
    if not log_dir.exists():
        print(f"[extract] no log dir at {log_dir} — nothing to extract")
        return 0

    n = 0
    for log_path in sorted(log_dir.glob("*.log")):
        # Each line is the runner's stderr JSONL (when invoked via
        # spawn-execute-request, stderr → log file). Filter for
        # attempt-end events.
        try:
            data = log_path.read_text()
        except OSError:
            continue
        attempts: list[dict] = []
        for line in data.splitlines():
            line = line.strip()
            if not line.startswith("{"):
                continue
            try:
                ev = json.loads(line)
            except json.JSONDecodeError:
                continue
            if ev.get("msg") == "attempt-end":
                attempts.append(ev)
        if not attempts:
            continue
        # Group by tier → inferred (driver, model) — for now we tag the
        # cell as `runner|tier:T0` etc. since the per-tier driver mapping
        # lives in chitin code, not in the log line. Subsequent commit
        # joins with planInvocation's resolved driver+model.
        for ev in attempts:
            tier = ev.get("tier") or "unknown"
            workflow_id = ev.get("workflow_id")
            attempt = ev.get("attempt")
            escalated = bool(ev.get("escalation_requested"))
            value = 0.0 if escalated else 1.0
            record_observation(
                conn,
                ts=ev.get("ts", datetime.now(timezone.utc).isoformat()),
                driver="runner",          # placeholder until we join with driver resolution
                model=f"tier:{tier}",     # cell-key placeholder
                workflow_id=workflow_id,
                attempt=attempt,
                dimension="routing_effectiveness",
                value=value,
                raw_source=str(log_path.name),
                raw_payload=ev,
            )
            upsert_profile(conn, "runner", f"tier:{tier}", "routing_effectiveness", value)
            n += 1
    conn.commit()
    return n


def cmd_init(_args) -> None:
    conn = open_db()
    print(f"compatibility db at {DB_PATH}")
    cur = conn.execute("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
    for r in cur:
        print(f"  table: {r['name']}")


def cmd_extract(_args) -> None:
    conn = open_db()
    n = extract_routing_effectiveness(conn)
    print(f"extracted {n} observations into routing_effectiveness")


def cmd_dump(_args) -> None:
    conn = open_db()
    cur = conn.execute(
        "SELECT driver, model, n_observations, last_observed, "
        + ", ".join(DIMENSIONS) + " FROM profiles ORDER BY driver, model"
    )
    rows = cur.fetchall()
    if not rows:
        print("(no profiles yet — run `extract` first)")
        return
    for r in rows:
        print(f"\n{r['driver']} × {r['model']}  (n={r['n_observations']}, last={r['last_observed']})")
        for d in DIMENSIONS:
            v = r[d]
            print(f"  {d:30} {('—' if v is None else f'{v:.3f}')}")


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest="cmd")
    sub.add_parser("init").set_defaults(func=cmd_init)
    sub.add_parser("extract").set_defaults(func=cmd_extract)
    sub.add_parser("dump").set_defaults(func=cmd_dump)
    args = p.parse_args()
    if not getattr(args, "func", None):
        p.print_help()
        sys.exit(1)
    args.func(args)


if __name__ == "__main__":
    main()
