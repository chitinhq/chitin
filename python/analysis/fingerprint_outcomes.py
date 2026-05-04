"""P3 — fingerprint × outcome join: chain dispatches × swarm-runs results.

Materializes a sqlite table keyed by ``(fingerprint, role, task_class)``
with the outcome metrics that downstream ELO computation (P4) consumes.
Also emits a markdown report grouped per-role so the operator can read
"haiku-4-5 + reviewer + small-PR = 92% consensus" without translating
opaque hashes.

Why per-role aggregation: different roles have different definitions of
"success." Programmer = first-pass merge rate. Reviewer = consensus-with-
final-merge rate. Researcher = entry-acceptance rate. A flat global
table averages out the very signal we want.

Idempotent: re-running picks up new dispatches by re-deriving the table
each time. The sqlite output is materialized-view-shaped (DROP TABLE +
recreate), not append-only, so duplicate runs never inflate counts.

Usage::

    python -m analysis.fingerprint_outcomes \\
        --decisions-dir ~/.chitin \\
        --state-dir ~/.cache/chitin/swarm-state \\
        --tmp-dir ~/workspace/chitin/tmp \\
        --since 7d \\
        --out python/analysis/out/fingerprint-outcomes-2026-05-04.md \\
        --sqlite python/analysis/out/fingerprint-outcomes.sqlite

When ``--out`` and ``--sqlite`` are omitted, defaults to today's dated
filename in ``python/analysis/out/``.

Schema (sqlite):

    fingerprint_outcomes(
      fingerprint   TEXT,    -- 12-char hex hash from libs/contracts/src/fingerprint.ts
      model         TEXT,    -- driver-resolved model id
      role          TEXT,    -- programmer | reviewer | researcher | ...
      task_class    TEXT,    -- refactor | doc_update | bug_fix | ... (NULL when unknown)
      dispatch_count INT,
      allow_count   INT,
      deny_count    INT,
      lockdown_count INT,
      total_cost_usd REAL,
      pr_opened_count INT,
      pr_merged_count INT,
      bucket_b_count INT,
      mean_duration_ms REAL,
      first_seen_ts TEXT,    -- ISO-8601
      last_seen_ts  TEXT,
      PRIMARY KEY(fingerprint, role, task_class)
    )

P2 (chain-fingerprint-tagging) is the upstream prereq. Until that
merges + dogfoods 24h, this recipe runs over a chain whose fingerprint
columns are mostly empty — output is sparse but the recipe doesn't
crash. Empty fingerprint groups everything under fingerprint=''
which surfaces as one big "untagged" bucket for now.
"""
from __future__ import annotations

import argparse
import sqlite3
import sys
from collections import defaultdict
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from analysis.loaders import load_gov_decisions, parse_window_str
from analysis.swarm_runs import load_swarm_runs


# ─── Aggregation key + accumulator ────────────────────────────────────────


@dataclass(frozen=True)
class FingerprintKey:
    """Composite group-by key. All four dims, with empty string for
    pre-fingerprint dispatches (so the row still aggregates rather than
    being silently dropped)."""

    fingerprint: str
    model: str
    role: str
    task_class: str


@dataclass
class Accumulator:
    """Mutable per-key accumulator. Filled by both chain-side iteration
    (Decisions) and swarm-runs-side iteration (workflow outcomes)."""

    dispatch_count: int = 0
    allow_count: int = 0
    deny_count: int = 0
    lockdown_count: int = 0
    total_cost_usd: float = 0.0
    pr_opened_count: int = 0
    pr_merged_count: int = 0
    bucket_b_count: int = 0
    duration_samples_ms: list[int] = field(default_factory=list)
    first_seen_ts: Optional[datetime] = None
    last_seen_ts: Optional[datetime] = None

    def observe_ts(self, ts: datetime) -> None:
        if self.first_seen_ts is None or ts < self.first_seen_ts:
            self.first_seen_ts = ts
        if self.last_seen_ts is None or ts > self.last_seen_ts:
            self.last_seen_ts = ts

    @property
    def mean_duration_ms(self) -> float:
        return (
            sum(self.duration_samples_ms) / len(self.duration_samples_ms)
            if self.duration_samples_ms
            else 0.0
        )


# ─── Build the aggregation table ──────────────────────────────────────────


def build_table(
    decisions_dir: Path,
    state_dir: Path,
    tmp_dir: Path,
    window_str: str = "7d",
    now: Optional[datetime] = None,
) -> dict[FingerprintKey, Accumulator]:
    """Run the full join + aggregate. Returns the in-memory table that
    write_sqlite + render_markdown consume. Pure function modulo
    filesystem reads; safe to unit-test against a tmpdir.
    """
    if now is None:
        now = datetime.now(timezone.utc)
    window = parse_window_str(window_str, now)

    table: dict[FingerprintKey, Accumulator] = defaultdict(Accumulator)

    # Side 1: chain decisions. Dispatch counts, cost, allow/deny/lockdown
    # tallies. workflow_id ties these to swarm-runs.
    chain_load = load_gov_decisions(decisions_dir, window)
    # workflow_id → set of (fingerprint, model, role) tuples seen on the chain.
    # Used in side-2 to back-fill swarm-runs that don't carry fingerprint.
    wf_index: dict[str, set[tuple[str, str, str]]] = defaultdict(set)

    for d in chain_load.decisions:
        # task_class isn't on the gov-decision row (it's in the
        # ExecutionRequest, not the chain). Mark unknown for now;
        # P3.5 follow-up could plumb task_class via env the same way
        # P2 plumbs the fingerprint.
        key = FingerprintKey(
            fingerprint=d.fingerprint or "",
            model=d.model or "",
            role=d.role or "",
            task_class="",
        )
        acc = table[key]
        acc.dispatch_count += 1
        if d.allowed:
            acc.allow_count += 1
        else:
            acc.deny_count += 1
        if d.escalation == "lockdown":
            acc.lockdown_count += 1
        acc.total_cost_usd += d.cost_usd
        acc.observe_ts(d.ts)
        if d.workflow_id:
            wf_index[d.workflow_id].add((key.fingerprint, key.model, key.role))

    # Side 2: swarm runs. PR-opened, PR-merged, bucket-B, duration.
    # Joins to chain-side via workflow_id. swarm-runs has its own driver
    # field but no fingerprint — use the chain's fingerprint when we can
    # find a matching workflow_id, else fall through to an untagged bucket.
    runs = load_swarm_runs(state_dir, tmp_dir, window)

    for run in runs:
        # Build a sentinel "swarm-<entry_id>-<ts>" → workflow_id from
        # the run's marker. The marker stores it directly in the
        # SwarmRun, but we also have wf_index from the chain side.
        # SwarmRun doesn't expose workflow_id directly in its public
        # shape, so fall back to wf_index by entry-id substring.
        match_keys: set[tuple[str, str, str]] = set()
        for wf_id, dims_set in wf_index.items():
            if run.entry_id in wf_id:
                match_keys |= dims_set

        # If chain-side never tagged this workflow, surface under an
        # empty-fingerprint bucket so the row isn't silently dropped.
        if not match_keys:
            match_keys = {("", "", "")}

        for fp, model, role in match_keys:
            key = FingerprintKey(
                fingerprint=fp,
                model=model or run.model or "",
                role=role,
                task_class="",
            )
            acc = table[key]
            if run.pr_url:
                acc.pr_opened_count += 1
            if run.bucket_b:
                acc.bucket_b_count += 1
            if run.duration_ms:
                acc.duration_samples_ms.append(run.duration_ms)
            # PR-merged signal: SwarmRun doesn't currently distinguish
            # opened-vs-merged. P3.5 follow-up wires `gh pr view --json
            # mergedAt` into the swarm-runs loader. For now, pr_merged_count
            # stays 0 — the field is reserved but un-populated.
            acc.observe_ts(run.dispatched_at)

    return table


# ─── Sqlite write ─────────────────────────────────────────────────────────


def write_sqlite(table: dict[FingerprintKey, Accumulator], db_path: Path) -> None:
    """Materialized view: drop + recreate. Idempotent over re-runs."""
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(db_path)
    try:
        cur = conn.cursor()
        cur.execute("DROP TABLE IF EXISTS fingerprint_outcomes")
        cur.execute(
            """
            CREATE TABLE fingerprint_outcomes (
              fingerprint TEXT NOT NULL,
              model TEXT NOT NULL,
              role TEXT NOT NULL,
              task_class TEXT NOT NULL,
              dispatch_count INTEGER NOT NULL,
              allow_count INTEGER NOT NULL,
              deny_count INTEGER NOT NULL,
              lockdown_count INTEGER NOT NULL,
              total_cost_usd REAL NOT NULL,
              pr_opened_count INTEGER NOT NULL,
              pr_merged_count INTEGER NOT NULL,
              bucket_b_count INTEGER NOT NULL,
              mean_duration_ms REAL NOT NULL,
              first_seen_ts TEXT,
              last_seen_ts TEXT,
              PRIMARY KEY (fingerprint, model, role, task_class)
            )
            """
        )
        for key, acc in table.items():
            cur.execute(
                """
                INSERT INTO fingerprint_outcomes VALUES
                (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    key.fingerprint,
                    key.model,
                    key.role,
                    key.task_class,
                    acc.dispatch_count,
                    acc.allow_count,
                    acc.deny_count,
                    acc.lockdown_count,
                    acc.total_cost_usd,
                    acc.pr_opened_count,
                    acc.pr_merged_count,
                    acc.bucket_b_count,
                    acc.mean_duration_ms,
                    acc.first_seen_ts.isoformat() if acc.first_seen_ts else None,
                    acc.last_seen_ts.isoformat() if acc.last_seen_ts else None,
                ),
            )
        conn.commit()
    finally:
        conn.close()


# ─── Markdown report ──────────────────────────────────────────────────────


def render_markdown(
    table: dict[FingerprintKey, Accumulator], window_str: str, generated_at: datetime
) -> str:
    """Group rows by role; show fingerprint dimensions + outcome metrics
    in human-readable form. Per-role only — no global average row, since
    averaging across roles dilutes the signal we exist to surface."""
    lines: list[str] = []
    lines.append("# Fingerprint × outcomes report")
    lines.append("")
    lines.append(f"Window: last {window_str} (generated at {generated_at.isoformat()})")
    lines.append("")
    lines.append(
        "Each row aggregates chain dispatches × swarm-runs outcomes by "
        "the dispatch's fingerprint dimensions. Empty fingerprint = "
        "pre-P2-fingerprint dispatches; will shrink as P2 dogfooding "
        "tags new rows."
    )
    lines.append("")

    by_role: dict[str, list[tuple[FingerprintKey, Accumulator]]] = defaultdict(list)
    for key, acc in table.items():
        by_role[key.role or "(untagged)"].append((key, acc))

    # Stable role order: known roles first (programmer, reviewer, etc.),
    # untagged last.
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
    role_keys = sorted(
        by_role.keys(),
        key=lambda r: (
            known_role_order.index(r) if r in known_role_order else len(known_role_order),
            r,
        ),
    )

    for role in role_keys:
        rows = by_role[role]
        # Sort within role by dispatch_count desc — most-active fingerprint
        # surfaces first.
        rows.sort(key=lambda kv: -kv[1].dispatch_count)

        lines.append(f"## Role: `{role}`")
        lines.append("")
        lines.append(
            "| fingerprint | model | dispatches | allow% | deny | lockdown | "
            "PRs opened | bucket-B | $ cost | mean dur (s) |"
        )
        lines.append(
            "|---|---|---|---|---|---|---|---|---|---|"
        )
        for key, acc in rows:
            allow_pct = (
                100.0 * acc.allow_count / acc.dispatch_count
                if acc.dispatch_count > 0
                else 0.0
            )
            fp_short = key.fingerprint or "(none)"
            model_short = key.model or "(none)"
            lines.append(
                f"| `{fp_short}` | `{model_short}` | "
                f"{acc.dispatch_count} | {allow_pct:.1f}% | "
                f"{acc.deny_count} | {acc.lockdown_count} | "
                f"{acc.pr_opened_count} | {acc.bucket_b_count} | "
                f"${acc.total_cost_usd:.4f} | "
                f"{acc.mean_duration_ms / 1000:.1f} |"
            )
        lines.append("")

    lines.append("---")
    lines.append("")
    lines.append(
        "Source: `python -m analysis.fingerprint_outcomes`. "
        "Schema in module docstring. Materialized to "
        "`fingerprint-outcomes.sqlite`. ELO board (P4) consumes the same table."
    )
    return "\n".join(lines)


# ─── CLI entry ────────────────────────────────────────────────────────────


def main(argv: Optional[list[str]] = None) -> int:
    parser = argparse.ArgumentParser(
        description="Build the fingerprint × outcomes table from chain + swarm-runs.",
    )
    parser.add_argument(
        "--decisions-dir",
        type=Path,
        default=Path.home() / ".chitin",
        help="Directory containing gov-decisions-*.jsonl files.",
    )
    parser.add_argument(
        "--state-dir",
        type=Path,
        default=Path.home() / ".cache" / "chitin" / "swarm-state" / "dispatched",
        help="Dispatcher marker directory (contains <entry-id>.json files).",
    )
    parser.add_argument(
        "--tmp-dir",
        type=Path,
        default=Path.cwd() / "tmp",
        help="Repo-relative tmp/ where workflow result envelopes live.",
    )
    parser.add_argument(
        "--since",
        default="7d",
        help="Window: Nd / Nh / Nm. Default 7d.",
    )
    parser.add_argument(
        "--out",
        type=Path,
        default=None,
        help="Markdown report path. Defaults to python/analysis/out/fingerprint-outcomes-<YYYY-MM-DD>.md.",
    )
    parser.add_argument(
        "--sqlite",
        type=Path,
        default=None,
        help="Sqlite materialized-view path. Defaults to python/analysis/out/fingerprint-outcomes.sqlite.",
    )
    args = parser.parse_args(argv)

    now = datetime.now(timezone.utc)
    table = build_table(
        decisions_dir=args.decisions_dir,
        state_dir=args.state_dir,
        tmp_dir=args.tmp_dir,
        window_str=args.since,
        now=now,
    )

    out_dir = Path(__file__).parent / "out"
    out_dir.mkdir(parents=True, exist_ok=True)
    md_path = args.out or (
        out_dir / f"fingerprint-outcomes-{now.strftime('%Y-%m-%d')}.md"
    )
    sqlite_path = args.sqlite or (out_dir / "fingerprint-outcomes.sqlite")

    md_path.parent.mkdir(parents=True, exist_ok=True)
    md_path.write_text(render_markdown(table, args.since, now))
    write_sqlite(table, sqlite_path)

    print(
        f"fingerprint_outcomes: {len(table)} rows "
        f"→ md={md_path} sqlite={sqlite_path}",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
