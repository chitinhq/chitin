"""P4 — Pairwise ELO leaderboard per (role, task_class).

Reads the materialized sqlite from ``analysis.fingerprint_outcomes`` (P3)
and computes an ELO score per fingerprint within each role × task_class
bucket. Each dispatch is a "match" against a virtual baseline opponent
at rating 1500; success → win, failure → loss. Standard chess ELO update
(K=32) with deterministic match ordering so re-runs over the same input
produce identical scores.

Why ELO and not flat success-rate ranking: different fingerprints work
different task-shapes. A flat ``success_rate`` table averages out
strengths. ELO via per-match updates surfaces "fingerprint A converged
to 1620 over 50 matches" vs "fingerprint B is at 1500 (unproven)" —
sample size is part of the signal, not just the rate.

Drift behavior: when the static-default baseline changes (e.g., a routing
reshuffle promotes haiku-4-5 to a tier where sonnet was), absolute ELO
numbers reset for fingerprints that fall out of the leaderboard, but
relative ranking among the remaining fingerprints carries forward
because their match histories don't change.

Usage::

    python -m analysis.routing_elo \\
        --sqlite python/analysis/out/fingerprint-outcomes.sqlite \\
        --role reviewer \\
        --out python/analysis/out/routing-elo-2026-05-04.md

When ``--role`` is omitted, the report covers all roles. Output is
markdown grouped by (role, task_class).

Match-outcome rules (per-row in the fingerprint_outcomes table):
- ``wins = allow_count + pr_opened_count`` (chain successes + swarm-runs successes)
- ``losses = deny_count + lockdown_count + bucket_b_count``

Each integer in those counts contributes one match to the
fingerprint's tournament. A row with allow_count=10 + pr_opened_count=3
+ deny_count=2 yields 13 wins and 2 losses → 15 total matches.

Win/loss order within a fingerprint is deterministic — Bresenham-style
interleaving so a 50/50 record stays near baseline rather than
swinging through wins-first-then-losses. See ``compute_elo`` docstring.
"""
from __future__ import annotations

import argparse
import math
import sqlite3
import sys
from collections import defaultdict
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional


# ─── Constants ────────────────────────────────────────────────────────────

# Standard chess ELO baseline. Means a brand-new fingerprint with zero
# matches is rated as "average" against the baseline opponent at 1500.
BASELINE_RATING = 1500.0

# K factor — how much each match moves the rating. 32 is the chess-USCF
# default for new players; 16 for established. We use 32 to stay
# responsive to recent shifts (fingerprints that start failing quickly
# fall in the leaderboard) at the cost of some noise.
K_FACTOR = 32.0

# The virtual opponent every fingerprint plays against. Static at the
# baseline rating so all fingerprints are evaluated against the same
# anchor. Per the design's drift-behavior note, when this anchor's
# semantics change (e.g., the static-default routing shifts), absolute
# numbers reset but relative ranking among non-resetting fingerprints
# carries forward.
OPPONENT_RATING = BASELINE_RATING


# ─── Match derivation ─────────────────────────────────────────────────────


@dataclass(frozen=True)
class FingerprintRow:
    """One row from the fingerprint_outcomes sqlite materialized view."""

    fingerprint: str
    model: str
    role: str
    task_class: str
    dispatch_count: int
    allow_count: int
    deny_count: int
    lockdown_count: int
    pr_opened_count: int
    bucket_b_count: int
    total_cost_usd: float
    mean_duration_ms: float
    first_seen_ts: Optional[str]
    last_seen_ts: Optional[str]


def derive_match_record(row: FingerprintRow) -> tuple[int, int]:
    """Return (wins, losses) for the fingerprint's pseudo-tournament.

    Wins: ``allow_count + pr_opened_count`` — chain successes plus
    swarm-runs successes. Each side is its own count of distinct
    matches; we deliberately do not de-duplicate, because a chain
    decision and a swarm-runs PR-open are different events even when
    they belong to the same workflow_id.

    Losses: ``deny_count + lockdown_count + bucket_b_count``.

    Pre-P2 rows where dispatch_count is zero but pr_opened_count is
    non-zero (swarm-only bucket) are honored: the swarm-runs successes
    still count as wins. The previous cap that mistakenly used
    dispatch_count would have under-counted these — fixed per Copilot
    review on #296.
    """
    successes = row.allow_count + row.pr_opened_count
    failures = row.deny_count + row.lockdown_count + row.bucket_b_count
    return (successes, failures)


# ─── ELO computation ──────────────────────────────────────────────────────


def expected_score(my_rating: float, opp_rating: float) -> float:
    """Standard ELO expected score: probability of winning against an
    opponent with the given rating. Range [0, 1]."""
    return 1.0 / (1.0 + math.pow(10.0, (opp_rating - my_rating) / 400.0))


def apply_match(rating: float, outcome: float, opponent: float) -> float:
    """Standard ELO update. ``outcome`` is 1.0 for a win, 0.0 for a loss,
    0.5 for a draw."""
    return rating + K_FACTOR * (outcome - expected_score(rating, opponent))


def compute_elo(wins: int, losses: int) -> float:
    """Iterate ELO updates with interleaved wins/losses for path-fairness.

    Why deterministic ordering: the table input doesn't carry per-match
    timestamps (only first/last_seen on the bucket), so any ordering
    we pick is somewhat arbitrary. Interleaving wins and losses
    proportional to their counts gives the path-fairest progression —
    a 50/50 record converges very close to baseline rather than
    swinging wildly through wins-first-then-losses. Re-running on the
    same (W, L) counts produces identical output, which is the
    testability invariant.

    Algorithm: walk N total matches; at each step, emit a "win" if
    we've drawn fewer wins than expected proportionally, else a
    "loss." This is the same pattern as Bresenham's line algorithm
    for proportional event placement. Equivalent to sorted-by-fraction
    interleave but cheaper and integer-only.

    Edge cases (per Copilot review on #296): when one side is zero,
    the proportional-emit condition can mis-classify because both sides
    of the inequality go to zero. Explicit handling: if losses==0,
    emit only wins; if wins==0, emit only losses. Verified by test
    cases at (N, 0), (0, N), (1, 0), (0, 1).
    """
    rating = BASELINE_RATING
    total = wins + losses
    if total == 0:
        return rating
    if losses == 0:
        for _ in range(wins):
            rating = apply_match(rating, 1.0, OPPONENT_RATING)
        return rating
    if wins == 0:
        for _ in range(losses):
            rating = apply_match(rating, 0.0, OPPONENT_RATING)
        return rating
    wins_emitted = 0
    losses_emitted = 0
    for _ in range(total):
        # Emit a win if we're "behind" on wins, else a loss.
        if wins_emitted * losses < losses_emitted * wins:
            rating = apply_match(rating, 1.0, OPPONENT_RATING)
            wins_emitted += 1
        else:
            rating = apply_match(rating, 0.0, OPPONENT_RATING)
            losses_emitted += 1
    return rating


# ─── Sqlite read ──────────────────────────────────────────────────────────


def load_rows(db_path: Path, role_filter: Optional[str] = None) -> list[FingerprintRow]:
    """Load all rows from the fingerprint_outcomes table.

    Missing DB or empty table → empty list (analysis tolerates absent
    inputs, mirroring the loaders convention).
    """
    if not db_path.exists():
        return []
    rows: list[FingerprintRow] = []
    conn = sqlite3.connect(db_path)
    try:
        cur = conn.cursor()
        # Confirm the table exists before querying.
        cur.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name='fingerprint_outcomes'"
        )
        if cur.fetchone() is None:
            return []
        # Explicit ORDER BY so re-runs over the same DB yield identical
        # markdown output. Without this, sqlite returns rows in
        # insertion-order which is fine but not contractual; tests + the
        # determinism guarantee in the module docstring rely on a stable
        # order. Sort by (role, task_class, fingerprint, model) — a
        # composite key guaranteed unique by the table's PRIMARY KEY.
        sql = (
            "SELECT fingerprint, model, role, task_class, "
            "dispatch_count, allow_count, deny_count, lockdown_count, "
            "pr_opened_count, bucket_b_count, total_cost_usd, mean_duration_ms, "
            "first_seen_ts, last_seen_ts FROM fingerprint_outcomes"
        )
        if role_filter is not None:
            sql += " WHERE role = ?"
            sql += " ORDER BY role, task_class, fingerprint, model"
            cur.execute(sql, (role_filter,))
        else:
            sql += " ORDER BY role, task_class, fingerprint, model"
            cur.execute(sql)
        for r in cur.fetchall():
            rows.append(FingerprintRow(*r))
    finally:
        conn.close()
    return rows


# ─── Leaderboard build ────────────────────────────────────────────────────


@dataclass(frozen=True)
class LeaderboardEntry:
    fingerprint: str
    model: str
    role: str
    task_class: str
    dispatch_count: int
    wins: int
    losses: int
    elo: float
    success_rate: float
    cost_per_success: Optional[float]


def build_leaderboard(rows: list[FingerprintRow]) -> list[LeaderboardEntry]:
    """Convert rows into ELO-scored entries. Sortable by elo desc."""
    entries: list[LeaderboardEntry] = []
    for row in rows:
        wins, losses = derive_match_record(row)
        total_matches = wins + losses
        elo = compute_elo(wins, losses)
        success_rate = (wins / total_matches) if total_matches > 0 else 0.0
        cost_per_success = (row.total_cost_usd / wins) if wins > 0 else None
        entries.append(
            LeaderboardEntry(
                fingerprint=row.fingerprint,
                model=row.model,
                role=row.role,
                task_class=row.task_class,
                dispatch_count=row.dispatch_count,
                wins=wins,
                losses=losses,
                elo=elo,
                success_rate=success_rate,
                cost_per_success=cost_per_success,
            )
        )
    return entries


# ─── Markdown render ──────────────────────────────────────────────────────


def render_leaderboard(
    entries: list[LeaderboardEntry], generated_at: datetime, role_filter: Optional[str]
) -> str:
    """One ## section per (role, task_class). Within each section,
    fingerprints are sorted by ELO descending."""
    lines: list[str] = []
    lines.append("# Routing ELO leaderboard")
    lines.append("")
    if role_filter:
        lines.append(f"Role filter: `{role_filter}`")
    else:
        lines.append("All roles included.")
    lines.append("")
    lines.append(f"Generated at {generated_at.isoformat()}.")
    lines.append("")
    lines.append(
        "Each row is a fingerprint's ELO score after playing wins+losses "
        f"matches against a virtual baseline opponent at rating "
        f"{int(BASELINE_RATING)}. K-factor is {int(K_FACTOR)} (chess-USCF "
        "default for new players). Higher ELO = the fingerprint outperforms "
        "the baseline; lower = underperforms. Sample size matters — a "
        "fingerprint with 5 wins and 0 losses can still be lower than one "
        "with 50 wins and 5 losses because ELO rewards consistency across "
        "many matches, not the rate alone."
    )
    lines.append("")

    by_bucket: dict[tuple[str, str], list[LeaderboardEntry]] = defaultdict(list)
    for e in entries:
        by_bucket[(e.role or "(untagged)", e.task_class or "(any)")].append(e)

    # Stable bucket order: known roles first, then alphabetical.
    known_role_order = [
        "programmer",
        "reviewer",
        "peer-reviewer",
        "comment-responder",
        "researcher",
        "groomer",
        "analyst",
        "tech-writer",
        "debt-curator",
        "(untagged)",
    ]
    bucket_keys = sorted(
        by_bucket.keys(),
        key=lambda rt: (
            known_role_order.index(rt[0]) if rt[0] in known_role_order else len(known_role_order),
            rt[0],
            rt[1],
        ),
    )

    for (role, task_class) in bucket_keys:
        bucket = by_bucket[(role, task_class)]
        # Stable sort: primary by ELO desc, then fingerprint + model + dispatch
        # count to break ties at baseline (where many entries cluster). Without
        # the tiebreaker, ties produced report churn between runs.
        bucket.sort(
            key=lambda e: (-e.elo, e.fingerprint, e.model, -e.dispatch_count)
        )

        lines.append(f"## `{role}` × `{task_class}`")
        lines.append("")
        lines.append(
            "| ELO | fingerprint | model | dispatches | W-L | success% | "
            "$/success |"
        )
        lines.append("|---|---|---|---|---|---|---|")
        for e in bucket:
            fp = e.fingerprint or "(none)"
            model = e.model or "(none)"
            # `is not None` (not truthiness) so a real $0.00 cost renders as
            # "$0.0000" rather than the missing-data dash — matters for free-
            # tier copilot runs where cost is genuinely zero.
            cost_per = (
                f"${e.cost_per_success:.4f}" if e.cost_per_success is not None else "—"
            )
            lines.append(
                f"| **{e.elo:.0f}** | `{fp}` | `{model}` | "
                f"{e.dispatch_count} | {e.wins}-{e.losses} | "
                f"{e.success_rate * 100:.1f}% | {cost_per} |"
            )
        lines.append("")

    if not by_bucket:
        lines.append("_No data — populate by running `python -m analysis.fingerprint_outcomes` first._")
        lines.append("")

    lines.append("---")
    lines.append("")
    lines.append(
        f"Source: `python -m analysis.routing_elo`. "
        f"Reads the sqlite materialized view from "
        "`analysis.fingerprint_outcomes`. Re-runs are deterministic "
        "given fixed match-derivation rules + opponent rating."
    )
    return "\n".join(lines)


# ─── CLI entry ────────────────────────────────────────────────────────────


def main(argv: Optional[list[str]] = None) -> int:
    parser = argparse.ArgumentParser(
        description="ELO leaderboard for fingerprint × role/task_class.",
    )
    parser.add_argument(
        "--sqlite",
        type=Path,
        default=None,
        help="fingerprint-outcomes.sqlite path (defaults to python/analysis/out/).",
    )
    parser.add_argument(
        "--role",
        default=None,
        help="Filter to one role (programmer, reviewer, ...). Default: all roles.",
    )
    parser.add_argument(
        "--out",
        type=Path,
        default=None,
        help="Markdown report path. Defaults to python/analysis/out/routing-elo-<YYYY-MM-DD>.md.",
    )
    args = parser.parse_args(argv)

    out_dir = Path(__file__).parent / "out"
    out_dir.mkdir(parents=True, exist_ok=True)

    sqlite_path = args.sqlite or (out_dir / "fingerprint-outcomes.sqlite")
    rows = load_rows(sqlite_path, role_filter=args.role)
    entries = build_leaderboard(rows)

    now = datetime.now(timezone.utc)
    md_path = args.out or (out_dir / f"routing-elo-{now.strftime('%Y-%m-%d')}.md")
    md_path.parent.mkdir(parents=True, exist_ok=True)
    md_path.write_text(render_leaderboard(entries, now, args.role))

    print(
        f"routing_elo: {len(entries)} entries → {md_path}",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
