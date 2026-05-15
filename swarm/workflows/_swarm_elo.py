#!/usr/bin/env python3
"""Shared library for the swarm ELO leaderboard.

Two tables in ~/.openclaw/data/clawta.db:

  swarm_elo               aggregate rating per outcome bucket
  swarm_dispatch_scores   one row per judged dispatch — the audit trail

The schema is created on first use (open_db). Older v1 tables are
upgraded in place so existing scores are preserved while the richer v2
dimensions are added.

Public API (used by the judge + CLI):

  open_db()                                  -> sqlite3.Connection
  record_score(...)
  update_elo(...)
  leaderboard(conn, task_class=None, limit=10) -> list[dict]
  aggregate_scores(conn, group_by=(...), ...)   -> list[dict]
  recent_scores(conn, limit=20)              -> list[dict]
"""

from __future__ import annotations

import json
import os
import sqlite3
import time
from pathlib import Path
from typing import Iterable


DB_PATH = Path(os.path.expanduser("~/.openclaw/data/clawta.db"))
BASE_ELO = 1500.0
BASE_K = 32.0
K_DECAY_AT_N = 50  # K halves at this dispatches_count

KEY_FIELDS = (
    "driver",
    "model",
    "role",
    "task_class",
    "complexity_bucket",
    "capabilities_key",
    "pr_outcome",
    "ci_outcome",
    "review_outcome",
)
AGGREGATE_GROUPS = {
    "driver_model": ("driver", "model"),
    "driver_model_task_class": ("driver", "model", "task_class"),
    "task_class": ("task_class",),
}


def _normalize_dimension(value: str | None) -> str:
    return str(value or "").strip()


def normalize_capabilities(capabilities: Iterable[str] | None) -> list[str]:
    vals = {
        str(cap).strip().lower()
        for cap in (capabilities or [])
        if str(cap).strip()
    }
    return sorted(vals)


def capabilities_key(capabilities: Iterable[str] | None) -> str:
    vals = normalize_capabilities(capabilities)
    return ",".join(vals)


def _column_names(conn: sqlite3.Connection, table: str) -> set[str]:
    rows = conn.execute(f"PRAGMA table_info({table})").fetchall()
    return {str(row[1]) for row in rows}


def _migrate_swarm_elo(conn: sqlite3.Connection) -> None:
    columns = _column_names(conn, "swarm_elo")
    if {
        "role",
        "complexity_bucket",
        "capabilities_key",
        "pr_outcome",
        "ci_outcome",
        "review_outcome",
        "first_scored_at",
    }.issubset(columns):
        return

    conn.executescript(
        """
        CREATE TABLE swarm_elo_v2 (
            id INTEGER PRIMARY KEY,
            driver TEXT NOT NULL,
            model TEXT NOT NULL,
            role TEXT NOT NULL DEFAULT '',
            task_class TEXT NOT NULL DEFAULT '',
            complexity_bucket TEXT NOT NULL DEFAULT '',
            capabilities_key TEXT NOT NULL DEFAULT '',
            pr_outcome TEXT NOT NULL DEFAULT '',
            ci_outcome TEXT NOT NULL DEFAULT '',
            review_outcome TEXT NOT NULL DEFAULT '',
            elo_score REAL NOT NULL,
            dispatches_count INTEGER NOT NULL DEFAULT 0,
            last_dispatch_id TEXT,
            first_scored_at INTEGER NOT NULL,
            last_updated INTEGER NOT NULL,
            UNIQUE(
                driver, model, role, task_class, complexity_bucket,
                capabilities_key, pr_outcome, ci_outcome, review_outcome
            )
        );

        INSERT INTO swarm_elo_v2 (
            id, driver, model, role, task_class, complexity_bucket,
            capabilities_key, pr_outcome, ci_outcome, review_outcome,
            elo_score, dispatches_count, last_dispatch_id, first_scored_at,
            last_updated
        )
        SELECT
            id,
            driver,
            model,
            '',
            COALESCE(task_class, ''),
            '',
            '',
            '',
            '',
            '',
            elo_score,
            dispatches_count,
            last_dispatch_id,
            COALESCE(last_updated, CAST(strftime('%s', 'now') AS INTEGER)),
            COALESCE(last_updated, CAST(strftime('%s', 'now') AS INTEGER))
        FROM swarm_elo;

        DROP TABLE swarm_elo;
        ALTER TABLE swarm_elo_v2 RENAME TO swarm_elo;
        """
    )


def _ensure_column(
    conn: sqlite3.Connection,
    table: str,
    column: str,
    definition: str,
) -> None:
    if column not in _column_names(conn, table):
        conn.execute(f"ALTER TABLE {table} ADD COLUMN {column} {definition}")


def _migrate_dispatch_scores(conn: sqlite3.Connection) -> None:
    _ensure_column(conn, "swarm_dispatch_scores", "role", "TEXT NOT NULL DEFAULT ''")
    _ensure_column(conn, "swarm_dispatch_scores", "complexity_bucket", "TEXT NOT NULL DEFAULT ''")
    _ensure_column(conn, "swarm_dispatch_scores", "capabilities_json", "TEXT NOT NULL DEFAULT '[]'")
    _ensure_column(conn, "swarm_dispatch_scores", "capabilities_key", "TEXT NOT NULL DEFAULT ''")
    _ensure_column(conn, "swarm_dispatch_scores", "pr_outcome", "TEXT NOT NULL DEFAULT ''")
    _ensure_column(conn, "swarm_dispatch_scores", "ci_outcome", "TEXT NOT NULL DEFAULT ''")
    _ensure_column(conn, "swarm_dispatch_scores", "review_outcome", "TEXT NOT NULL DEFAULT ''")
    _ensure_column(conn, "swarm_dispatch_scores", "pr_created_at", "INTEGER")
    _ensure_column(conn, "swarm_dispatch_scores", "pr_updated_at", "INTEGER")
    _ensure_column(conn, "swarm_dispatch_scores", "pr_merged_at", "INTEGER")
    _ensure_column(conn, "swarm_dispatch_scores", "inferred", "INTEGER NOT NULL DEFAULT 0")
    conn.execute("UPDATE swarm_dispatch_scores SET task_class = '' WHERE task_class IS NULL")


def _init_schema(conn: sqlite3.Connection) -> None:
    conn.executescript(
        """
        CREATE TABLE IF NOT EXISTS swarm_elo (
            id INTEGER PRIMARY KEY,
            driver TEXT NOT NULL,
            model TEXT NOT NULL,
            role TEXT NOT NULL DEFAULT '',
            task_class TEXT NOT NULL DEFAULT '',
            complexity_bucket TEXT NOT NULL DEFAULT '',
            capabilities_key TEXT NOT NULL DEFAULT '',
            pr_outcome TEXT NOT NULL DEFAULT '',
            ci_outcome TEXT NOT NULL DEFAULT '',
            review_outcome TEXT NOT NULL DEFAULT '',
            elo_score REAL NOT NULL,
            dispatches_count INTEGER NOT NULL DEFAULT 0,
            last_dispatch_id TEXT,
            first_scored_at INTEGER NOT NULL,
            last_updated INTEGER NOT NULL,
            UNIQUE(
                driver, model, role, task_class, complexity_bucket,
                capabilities_key, pr_outcome, ci_outcome, review_outcome
            )
        );

        CREATE TABLE IF NOT EXISTS swarm_dispatch_scores (
            id INTEGER PRIMARY KEY,
            ticket_id TEXT NOT NULL,
            pr_url TEXT,
            driver TEXT NOT NULL,
            model TEXT NOT NULL,
            role TEXT NOT NULL DEFAULT '',
            task_class TEXT NOT NULL DEFAULT '',
            complexity_bucket TEXT NOT NULL DEFAULT '',
            capabilities_json TEXT NOT NULL DEFAULT '[]',
            capabilities_key TEXT NOT NULL DEFAULT '',
            pr_outcome TEXT NOT NULL DEFAULT '',
            ci_outcome TEXT NOT NULL DEFAULT '',
            review_outcome TEXT NOT NULL DEFAULT '',
            code_quality INTEGER,
            test_coverage INTEGER,
            scope_adherence INTEGER,
            efficiency INTEGER,
            review_friendliness INTEGER,
            total_score INTEGER,
            judge_model TEXT NOT NULL,
            judge_reasoning TEXT,
            inferred INTEGER NOT NULL DEFAULT 0,
            pr_created_at INTEGER,
            pr_updated_at INTEGER,
            pr_merged_at INTEGER,
            scored_at INTEGER NOT NULL
        );

        CREATE INDEX IF NOT EXISTS idx_scores_ticket
            ON swarm_dispatch_scores(ticket_id);
        CREATE INDEX IF NOT EXISTS idx_scores_driver_model_class
            ON swarm_dispatch_scores(driver, model, task_class);
        """
    )

    _migrate_swarm_elo(conn)
    _migrate_dispatch_scores(conn)
    conn.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_scores_dimensions
            ON swarm_dispatch_scores(
                driver, model, role, task_class, complexity_bucket,
                pr_outcome, ci_outcome, review_outcome
            )
        """
    )
    conn.commit()


def open_db() -> sqlite3.Connection:
    """Open + initialize the swarm ELO database."""
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(DB_PATH))
    conn.row_factory = sqlite3.Row
    _init_schema(conn)
    return conn


def _dimension_payload(
    *,
    driver: str,
    model: str,
    role: str | None = None,
    task_class: str | None = None,
    complexity_bucket: str | None = None,
    capabilities: Iterable[str] | None = None,
    pr_outcome: str | None = None,
    ci_outcome: str | None = None,
    review_outcome: str | None = None,
) -> dict[str, str]:
    caps = normalize_capabilities(capabilities)
    return {
        "driver": _normalize_dimension(driver),
        "model": _normalize_dimension(model),
        "role": _normalize_dimension(role),
        "task_class": _normalize_dimension(task_class),
        "complexity_bucket": _normalize_dimension(complexity_bucket),
        "capabilities_key": capabilities_key(caps),
        "pr_outcome": _normalize_dimension(pr_outcome),
        "ci_outcome": _normalize_dimension(ci_outcome),
        "review_outcome": _normalize_dimension(review_outcome),
    }


def record_score(
    conn: sqlite3.Connection,
    ticket_id: str,
    pr_url: str | None,
    driver: str,
    model: str,
    task_class: str | None,
    scores: dict,
    judge_model: str,
    reasoning: str,
    *,
    role: str | None = None,
    complexity_bucket: str | None = None,
    capabilities: Iterable[str] | None = None,
    pr_outcome: str | None = None,
    ci_outcome: str | None = None,
    review_outcome: str | None = None,
    pr_created_at: int | None = None,
    pr_updated_at: int | None = None,
    pr_merged_at: int | None = None,
    scored_at: int | None = None,
    inferred: bool = False,
) -> int:
    """Insert one row in swarm_dispatch_scores. Returns the new row id."""
    total = sum(
        int(scores.get(k, 0))
        for k in (
            "code_quality",
            "test_coverage",
            "scope_adherence",
            "efficiency",
            "review_friendliness",
        )
    )
    dims = _dimension_payload(
        driver=driver,
        model=model,
        role=role,
        task_class=task_class,
        complexity_bucket=complexity_bucket,
        capabilities=capabilities,
        pr_outcome=pr_outcome,
        ci_outcome=ci_outcome,
        review_outcome=review_outcome,
    )
    caps = normalize_capabilities(capabilities)
    cur = conn.execute(
        """
        INSERT INTO swarm_dispatch_scores (
            ticket_id, pr_url, driver, model, role, task_class,
            complexity_bucket, capabilities_json, capabilities_key,
            pr_outcome, ci_outcome, review_outcome,
            code_quality, test_coverage, scope_adherence,
            efficiency, review_friendliness, total_score,
            judge_model, judge_reasoning, inferred,
            pr_created_at, pr_updated_at, pr_merged_at, scored_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
        (
            ticket_id,
            pr_url,
            dims["driver"],
            dims["model"],
            dims["role"],
            dims["task_class"],
            dims["complexity_bucket"],
            json.dumps(caps),
            dims["capabilities_key"],
            dims["pr_outcome"],
            dims["ci_outcome"],
            dims["review_outcome"],
            int(scores.get("code_quality", 0)),
            int(scores.get("test_coverage", 0)),
            int(scores.get("scope_adherence", 0)),
            int(scores.get("efficiency", 0)),
            int(scores.get("review_friendliness", 0)),
            total,
            judge_model,
            reasoning,
            1 if inferred else 0,
            pr_created_at,
            pr_updated_at,
            pr_merged_at,
            int(scored_at or time.time()),
        ),
    )
    conn.commit()
    return int(cur.lastrowid)


def update_elo(
    conn: sqlite3.Connection,
    driver: str,
    model: str,
    task_class: str | None,
    total_score: int,
    last_dispatch_id: str | None = None,
    *,
    role: str | None = None,
    complexity_bucket: str | None = None,
    capabilities: Iterable[str] | None = None,
    pr_outcome: str | None = None,
    ci_outcome: str | None = None,
    review_outcome: str | None = None,
    scored_at: int | None = None,
) -> float:
    """Apply a single-player ELO update. Returns the new ELO."""
    dims = _dimension_payload(
        driver=driver,
        model=model,
        role=role,
        task_class=task_class,
        complexity_bucket=complexity_bucket,
        capabilities=capabilities,
        pr_outcome=pr_outcome,
        ci_outcome=ci_outcome,
        review_outcome=review_outcome,
    )
    row = conn.execute(
        """
        SELECT id, elo_score, dispatches_count
          FROM swarm_elo
         WHERE driver = ? AND model = ? AND role = ? AND task_class = ?
           AND complexity_bucket = ? AND capabilities_key = ?
           AND pr_outcome = ? AND ci_outcome = ? AND review_outcome = ?
        """,
        tuple(dims[field] for field in KEY_FIELDS),
    ).fetchone()

    if row:
        current_elo = float(row["elo_score"])
        count = int(row["dispatches_count"])
    else:
        current_elo = BASE_ELO
        count = 0

    k = BASE_K / (1.0 + count / K_DECAY_AT_N)
    par = 15.0
    norm = max(-1.0, min(1.0, (total_score - par) / 10.0))
    new_elo = current_elo + k * norm
    now = int(scored_at or time.time())

    if row:
        conn.execute(
            """
            UPDATE swarm_elo
               SET elo_score = ?,
                   dispatches_count = dispatches_count + 1,
                   last_dispatch_id = ?,
                   last_updated = ?
             WHERE id = ?
            """,
            (new_elo, last_dispatch_id, now, int(row["id"])),
        )
    else:
        conn.execute(
            """
            INSERT INTO swarm_elo (
                driver, model, role, task_class, complexity_bucket,
                capabilities_key, pr_outcome, ci_outcome, review_outcome,
                elo_score, dispatches_count, last_dispatch_id,
                first_scored_at, last_updated
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                dims["driver"],
                dims["model"],
                dims["role"],
                dims["task_class"],
                dims["complexity_bucket"],
                dims["capabilities_key"],
                dims["pr_outcome"],
                dims["ci_outcome"],
                dims["review_outcome"],
                new_elo,
                1,
                last_dispatch_id,
                now,
                now,
            ),
        )
    conn.commit()
    return new_elo


def leaderboard(
    conn: sqlite3.Connection,
    task_class: str | None = None,
    limit: int = 10,
) -> list[dict]:
    """Return top outcome buckets by ELO."""
    params: tuple[object, ...]
    where = ""
    if task_class is not None:
        where = "WHERE task_class = ?"
        params = (_normalize_dimension(task_class), limit)
    else:
        params = (limit,)
    rows = conn.execute(
        f"""
        SELECT driver, model, role, task_class, complexity_bucket,
               capabilities_key, pr_outcome, ci_outcome, review_outcome,
               elo_score, dispatches_count, last_dispatch_id,
               first_scored_at, last_updated
          FROM swarm_elo
          {where}
         ORDER BY elo_score DESC
         LIMIT ?
        """,
        params,
    ).fetchall()
    return [dict(r) for r in rows]


def aggregate_scores(
    conn: sqlite3.Connection,
    group_by: str = "driver_model_task_class",
    limit: int = 10,
    task_class: str | None = None,
) -> list[dict]:
    """Aggregate outcome buckets into broader leaderboard cuts."""
    group_fields = AGGREGATE_GROUPS[group_by]
    select_fields = ", ".join(group_fields)
    params: list[object] = []
    where = ""
    if task_class is not None:
        where = "WHERE task_class = ?"
        params.append(_normalize_dimension(task_class))
    params.append(limit)
    rows = conn.execute(
        f"""
        SELECT {select_fields},
               COUNT(*) AS outcome_buckets,
               SUM(dispatches_count) AS dispatches_count,
               ROUND(SUM(elo_score * dispatches_count) / NULLIF(SUM(dispatches_count), 0), 2) AS weighted_elo,
               ROUND(AVG(elo_score), 2) AS mean_elo,
               MAX(last_updated) AS last_updated
          FROM swarm_elo
          {where}
         GROUP BY {select_fields}
         ORDER BY weighted_elo DESC, dispatches_count DESC
         LIMIT ?
        """,
        tuple(params),
    ).fetchall()
    return [dict(r) for r in rows]


def recent_scores(conn: sqlite3.Connection, limit: int = 20) -> list[dict]:
    """Return the most recent N judged dispatches."""
    rows = conn.execute(
        """
        SELECT ticket_id, pr_url, driver, model, role, task_class,
               complexity_bucket, capabilities_json, capabilities_key,
               pr_outcome, ci_outcome, review_outcome,
               total_score, judge_model, judge_reasoning, inferred,
               pr_created_at, pr_updated_at, pr_merged_at, scored_at
          FROM swarm_dispatch_scores
         ORDER BY id DESC
         LIMIT ?
        """,
        (limit,),
    ).fetchall()
    result = []
    for row in rows:
        item = dict(row)
        try:
            item["capabilities"] = json.loads(item.pop("capabilities_json"))
        except (TypeError, json.JSONDecodeError):
            item["capabilities"] = []
            item.pop("capabilities_json", None)
        result.append(item)
    return result


def lookup_elo(
    conn: sqlite3.Connection,
    driver: str,
    model: str,
    task_class: str | None = None,
    *,
    role: str | None = None,
    complexity_bucket: str | None = None,
    capabilities: Iterable[str] | None = None,
    pr_outcome: str | None = None,
    ci_outcome: str | None = None,
    review_outcome: str | None = None,
) -> dict | None:
    """Look up a single outcome bucket. Returns None if absent."""
    dims = _dimension_payload(
        driver=driver,
        model=model,
        role=role,
        task_class=task_class,
        complexity_bucket=complexity_bucket,
        capabilities=capabilities,
        pr_outcome=pr_outcome,
        ci_outcome=ci_outcome,
        review_outcome=review_outcome,
    )
    row = conn.execute(
        """
        SELECT driver, model, role, task_class, complexity_bucket,
               capabilities_key, pr_outcome, ci_outcome, review_outcome,
               elo_score, dispatches_count, first_scored_at, last_updated
          FROM swarm_elo
         WHERE driver = ? AND model = ? AND role = ? AND task_class = ?
           AND complexity_bucket = ? AND capabilities_key = ?
           AND pr_outcome = ? AND ci_outcome = ? AND review_outcome = ?
        """,
        tuple(dims[field] for field in KEY_FIELDS),
    ).fetchone()
    return dict(row) if row else None
