#!/usr/bin/env python3
"""Shared library for the swarm ELO leaderboard (Slice 5 of Hermes/Clawta
architecture epic, 2026-05-12).

Two tables in ~/.openclaw/data/clawta.db:

  swarm_elo               aggregate rating per (driver, model, task_class)
  swarm_dispatch_scores   one row per judged dispatch — the audit trail

The schema is created on first use (open_db). All writers use a single
shared connection — SQLite serializes writes naturally and there's no
contention pressure at this volume (≤100 dispatches/day).

ELO update:
  K-factor starts at 32 and decays as dispatches_count grows so a
  mature (driver, model, task_class) doesn't lurch on outliers. The
  judge produces a total_score in [5, 25]; we map that to a virtual
  "expected vs actual" using a uniform baseline (15 = par; >15 wins,
  <15 loses). Two competitors per dispatch don't exist — this is a
  single-agent rating against the rubric, not head-to-head. So the
  update is one-sided: a single-player ELO.

Public API (used by the judge + CLI):

  open_db()                                  -> sqlite3.Connection
  record_score(conn, ticket_id, pr_url, driver, model, task_class,
               scores: dict, judge_model, reasoning)
  update_elo(conn, driver, model, task_class, total_score)
  leaderboard(conn, task_class=None, limit=10) -> list[dict]
  recent_scores(conn, limit=20)              -> list[dict]
"""

from __future__ import annotations

import os
import sqlite3
import time
from pathlib import Path


DB_PATH = Path(os.path.expanduser("~/.openclaw/data/clawta.db"))
BASE_ELO = 1500.0
BASE_K = 32.0
K_DECAY_AT_N = 50  # K halves at this dispatches_count


def open_db() -> sqlite3.Connection:
    """Open + initialize the swarm ELO database."""
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(DB_PATH))
    conn.row_factory = sqlite3.Row
    conn.executescript(
        """
        CREATE TABLE IF NOT EXISTS swarm_elo (
            id INTEGER PRIMARY KEY,
            driver TEXT NOT NULL,
            model TEXT NOT NULL,
            task_class TEXT,
            elo_score REAL NOT NULL,
            dispatches_count INTEGER NOT NULL DEFAULT 0,
            last_dispatch_id TEXT,
            last_updated INTEGER NOT NULL,
            UNIQUE(driver, model, task_class)
        );

        CREATE TABLE IF NOT EXISTS swarm_dispatch_scores (
            id INTEGER PRIMARY KEY,
            ticket_id TEXT NOT NULL,
            pr_url TEXT,
            driver TEXT NOT NULL,
            model TEXT NOT NULL,
            task_class TEXT,
            code_quality INTEGER,
            test_coverage INTEGER,
            scope_adherence INTEGER,
            efficiency INTEGER,
            review_friendliness INTEGER,
            total_score INTEGER,
            judge_model TEXT NOT NULL,
            judge_reasoning TEXT,
            scored_at INTEGER NOT NULL
        );

        CREATE INDEX IF NOT EXISTS idx_scores_ticket
            ON swarm_dispatch_scores(ticket_id);
        CREATE INDEX IF NOT EXISTS idx_scores_driver_model_class
            ON swarm_dispatch_scores(driver, model, task_class);
        """
    )
    conn.commit()
    return conn


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
    cur = conn.execute(
        """
        INSERT INTO swarm_dispatch_scores (
            ticket_id, pr_url, driver, model, task_class,
            code_quality, test_coverage, scope_adherence,
            efficiency, review_friendliness, total_score,
            judge_model, judge_reasoning, scored_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
        (
            ticket_id,
            pr_url,
            driver,
            model,
            task_class,
            int(scores.get("code_quality", 0)),
            int(scores.get("test_coverage", 0)),
            int(scores.get("scope_adherence", 0)),
            int(scores.get("efficiency", 0)),
            int(scores.get("review_friendliness", 0)),
            total,
            judge_model,
            reasoning,
            int(time.time()),
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
) -> float:
    """Apply a single-player ELO update. Returns the new ELO."""
    row = conn.execute(
        """
        SELECT elo_score, dispatches_count
          FROM swarm_elo
         WHERE driver = ? AND model = ? AND (task_class IS ? OR task_class = ?)
        """,
        (driver, model, task_class, task_class),
    ).fetchone()

    if row:
        current_elo = float(row["elo_score"])
        count = int(row["dispatches_count"])
    else:
        current_elo = BASE_ELO
        count = 0

    # K decays as the rating matures. count=0 -> K=BASE_K; count=K_DECAY_AT_N -> K=BASE_K/2.
    k = BASE_K / (1.0 + count / K_DECAY_AT_N)

    # Single-player ELO: total_score range is [5,25]; par is 15.
    # Map to a "result" in [-1, 1] where +1 is a clean win.
    par = 15.0
    norm = max(-1.0, min(1.0, (total_score - par) / 10.0))

    new_elo = current_elo + k * norm

    conn.execute(
        """
        INSERT INTO swarm_elo (
            driver, model, task_class, elo_score, dispatches_count,
            last_dispatch_id, last_updated
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(driver, model, task_class) DO UPDATE SET
            elo_score = excluded.elo_score,
            dispatches_count = swarm_elo.dispatches_count + 1,
            last_dispatch_id = excluded.last_dispatch_id,
            last_updated = excluded.last_updated
        """,
        (
            driver,
            model,
            task_class,
            new_elo,
            count + 1 if row else 1,
            last_dispatch_id,
            int(time.time()),
        ),
    )
    conn.commit()
    return new_elo


def leaderboard(
    conn: sqlite3.Connection,
    task_class: str | None = None,
    limit: int = 10,
) -> list[dict]:
    """Return top (driver, model, task_class) rows by ELO."""
    if task_class is not None:
        rows = conn.execute(
            """
            SELECT driver, model, task_class, elo_score, dispatches_count,
                   last_dispatch_id, last_updated
              FROM swarm_elo
             WHERE task_class = ?
             ORDER BY elo_score DESC
             LIMIT ?
            """,
            (task_class, limit),
        ).fetchall()
    else:
        rows = conn.execute(
            """
            SELECT driver, model, task_class, elo_score, dispatches_count,
                   last_dispatch_id, last_updated
              FROM swarm_elo
             ORDER BY elo_score DESC
             LIMIT ?
            """,
            (limit,),
        ).fetchall()
    return [dict(r) for r in rows]


def recent_scores(conn: sqlite3.Connection, limit: int = 20) -> list[dict]:
    """Return the most recent N judged dispatches."""
    rows = conn.execute(
        """
        SELECT ticket_id, pr_url, driver, model, task_class,
               total_score, judge_model, judge_reasoning, scored_at
          FROM swarm_dispatch_scores
         ORDER BY id DESC
         LIMIT ?
        """,
        (limit,),
    ).fetchall()
    return [dict(r) for r in rows]


def lookup_elo(
    conn: sqlite3.Connection,
    driver: str,
    model: str,
    task_class: str | None = None,
) -> dict | None:
    """Look up a single (driver, model, task_class) row. Returns None if absent.

    Used by Slice 2.5 — Clawta's LLM router reads ELO before deciding.
    """
    row = conn.execute(
        """
        SELECT driver, model, task_class, elo_score, dispatches_count,
               last_updated
          FROM swarm_elo
         WHERE driver = ? AND model = ? AND (task_class IS ? OR task_class = ?)
        """,
        (driver, model, task_class, task_class),
    ).fetchone()
    return dict(row) if row else None
