"""Mine public benchmark leaderboards into the compatibility-substrate db.

Companion to compatibility_profiles.py. Where that module captures
chitin's OWN observed (driver, model) compatibility, this module
captures published model-only baselines from external leaderboards —
the cold-start data that lets the routing query rank a model the
operator has never dispatched themselves.

The split is deliberate:
  - model_seeds: model → published score (one source per row)
  - profiles: (driver, model) → operator-observed dimension (driver-
    specific behavior chitin sees in its own runs)

Routing query joins both. New models that show up on a leaderboard
get a baseline score immediately; the operator's own observations
refine that score per-driver as dispatches accrue.

Usage:
    cd python/analysis && uv run python -m analysis.compatibility_seed mine --source aider
    cd python/analysis && uv run python -m analysis.compatibility_seed mine --source all
    cd python/analysis && uv run python -m analysis.compatibility_seed dump

Sources implemented:
  ✅ aider-edit       — github.com/Aider-AI/aider edit_leaderboard.yml
  ✅ aider-polyglot   — github.com/Aider-AI/aider polyglot_leaderboard.yml
  ⏳ swebench-verified — needs tarball walk pattern (134 submissions × 2
                         file fetches blows GitHub API rate limit; use
                         git clone --depth=1 instead — TODO)
  ⏳ lmarena          — public API redirects + 307s; needs follow-up
                         to pin the right endpoint (TODO)
  ⏳ artificial-analysis — 401-gates the API; need auth or pull from
                            their CSV exports — TODO
"""
from __future__ import annotations

import argparse
import json
import os
import sqlite3
import sys
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

CHITIN_HOME = Path(os.environ.get("CHITIN_HOME") or os.path.expanduser("~/.chitin"))
DB_PATH = CHITIN_HOME / "compatibility.sqlite"

SCHEMA_ADDITIONS = """
-- Public-leaderboard scored model baselines. One row per
-- (model, source, metric); upsert on re-pull.
CREATE TABLE IF NOT EXISTS model_seeds (
    model       TEXT NOT NULL,
    source      TEXT NOT NULL,    -- 'aider-edit', 'aider-polyglot', 'swebench-verified', etc.
    metric      TEXT NOT NULL,    -- 'pass_rate_2', 'cost_per_run_usd', 'seconds_per_case', etc.
    value       REAL NOT NULL,
    scored_at   TEXT,             -- date the leaderboard recorded this run (when known)
    pulled_at   TEXT NOT NULL,    -- when chitin mined this row
    raw_payload TEXT,             -- JSON of the original entry, for re-derive
    PRIMARY KEY (model, source, metric)
);

CREATE INDEX IF NOT EXISTS idx_model_seeds_source ON model_seeds (source);
CREATE INDEX IF NOT EXISTS idx_model_seeds_metric ON model_seeds (source, metric);
"""


def open_db() -> sqlite3.Connection:
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    conn.executescript(SCHEMA_ADDITIONS)
    conn.commit()
    return conn


def upsert_seed(
    conn: sqlite3.Connection,
    model: str,
    source: str,
    metric: str,
    value: float,
    scored_at: str | None = None,
    raw_payload: dict | None = None,
) -> None:
    conn.execute(
        "INSERT INTO model_seeds (model, source, metric, value, scored_at, pulled_at, raw_payload) "
        "VALUES (?, ?, ?, ?, ?, ?, ?) "
        "ON CONFLICT(model, source, metric) DO UPDATE SET "
        "value = excluded.value, scored_at = excluded.scored_at, "
        "pulled_at = excluded.pulled_at, raw_payload = excluded.raw_payload",
        (
            model, source, metric, value, scored_at,
            datetime.now(timezone.utc).isoformat(),
            json.dumps(raw_payload) if raw_payload is not None else None,
        ),
    )


def http_get(url: str, timeout: int = 30) -> bytes:
    """Plain GET. Returns raw bytes; caller decodes."""
    req = urllib.request.Request(url, headers={"User-Agent": "chitin-compat-seed/0.1"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return resp.read()


# ─── Aider: edit + polyglot leaderboards ───────────────────────────────────
#
# Source: github.com/Aider-AI/aider, raw YAML files. One submission per
# YAML list entry; each carries `model`, `pass_rate_1`, `pass_rate_2`,
# `total_cost`, `seconds_per_case`, `error_outputs`, `_released`, etc.
#
# Score normalization:
#   - pass_rate_2 → 'pass_rate_2' as 0-100 (kept as published; routing
#     normalizer can divide by 100 if it wants 0.0-1.0)
#   - total_cost → 'cost_per_run_usd'
#   - seconds_per_case → 'seconds_per_case'
#   - well-formed % → 'percent_cases_well_formed'
#
# Each metric is a top-level row in model_seeds keyed by
# (model, source, metric). Re-pulls overwrite via ON CONFLICT.

AIDER_BASE = "https://raw.githubusercontent.com/Aider-AI/aider/main/aider/website/_data"
AIDER_EDIT_YAML = f"{AIDER_BASE}/edit_leaderboard.yml"
AIDER_POLYGLOT_YAML = f"{AIDER_BASE}/polyglot_leaderboard.yml"


def _parse_simple_yaml(text: str) -> list[dict[str, Any]]:
    """Tiny YAML list parser sufficient for Aider's leaderboard shape:
    a top-level list of dicts with flat scalar keys. Avoids adding a
    yaml dep just for this. Mirrors parseSimpleYaml in
    apps/runner/src/grooming/parse-backlog.ts.
    """
    entries: list[dict[str, Any]] = []
    cur: dict[str, Any] | None = None
    for raw in text.splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        if raw.startswith("- "):
            if cur:
                entries.append(cur)
            cur = {}
            line = raw[2:]  # strip leading "- "
        else:
            line = raw.strip()
        if cur is None:
            continue
        if ":" not in line:
            continue
        key, _, val = line.partition(":")
        key = key.strip()
        val = val.strip()
        if not key:
            continue
        # Tolerate quoted strings, numeric values, dates-as-strings.
        if val.startswith(('"', "'")) and val.endswith(('"', "'")):
            cur[key] = val[1:-1]
        else:
            try:
                cur[key] = int(val)
            except ValueError:
                try:
                    cur[key] = float(val)
                except ValueError:
                    cur[key] = val
    if cur:
        entries.append(cur)
    return entries


# Metrics we forward from each Aider entry. The list is intentionally
# narrow — we want comparable cells, not every column the YAML has.
AIDER_METRICS = {
    "pass_rate_2": "pass_rate_2",            # primary capability score (0-100)
    "pass_rate_1": "pass_rate_1",            # single-pass score (0-100)
    "percent_cases_well_formed": "percent_cases_well_formed",  # output-format compliance (0-100)
    "total_cost": "cost_per_run_usd",
    "seconds_per_case": "seconds_per_case",
    "error_outputs": "error_outputs_count",
}


def mine_aider_source(conn: sqlite3.Connection, source_name: str, url: str) -> int:
    text = http_get(url).decode("utf-8")
    entries = _parse_simple_yaml(text)
    n = 0
    for e in entries:
        model = e.get("model")
        if not isinstance(model, str) or not model:
            continue
        scored_at = e.get("date") or e.get("_released")
        if scored_at is not None and not isinstance(scored_at, str):
            scored_at = str(scored_at)
        for src_key, metric in AIDER_METRICS.items():
            v = e.get(src_key)
            if isinstance(v, (int, float)):
                upsert_seed(
                    conn,
                    model=model,
                    source=source_name,
                    metric=metric,
                    value=float(v),
                    scored_at=scored_at,
                    raw_payload=e,
                )
                n += 1
    conn.commit()
    return n


def mine_aider_edit(conn: sqlite3.Connection) -> int:
    return mine_aider_source(conn, "aider-edit", AIDER_EDIT_YAML)


def mine_aider_polyglot(conn: sqlite3.Connection) -> int:
    return mine_aider_source(conn, "aider-polyglot", AIDER_POLYGLOT_YAML)


# ─── SWE-bench Verified — TODO ─────────────────────────────────────────────
#
# Pattern that works without rate-limit pain: download the tarball of
# github.com/SWE-bench/experiments, walk evaluation/verified/*/
# {metadata.yaml, results/results.json}, aggregate.
#
# Stub returns 0 for now; flag CHITIN_COMPAT_SOURCE_SWEBENCH=1 will
# drive the implementation in the follow-up commit.

def mine_swebench_verified(conn: sqlite3.Connection) -> int:  # noqa: ARG001
    print("[mine] swebench-verified: TODO — needs tarball walk; stub returns 0")
    return 0


# ─── LMArena — TODO ────────────────────────────────────────────────────────
#
# Public ELO leaderboard. The /api/leaderboard URL 307-redirects; the
# actual data shape lives behind a different endpoint that needs probing.
# HuggingFace-hosted Space at
# huggingface.co/spaces/lmarena-ai/chatbot-arena-leaderboard is a
# fallback ingest path.

def mine_lmarena(conn: sqlite3.Connection) -> int:  # noqa: ARG001
    print("[mine] lmarena: TODO — endpoint shape needs follow-up")
    return 0


# ─── CLI ───────────────────────────────────────────────────────────────────

SOURCES = {
    "aider-edit": mine_aider_edit,
    "aider-polyglot": mine_aider_polyglot,
    "swebench-verified": mine_swebench_verified,
    "lmarena": mine_lmarena,
}


def cmd_mine(args) -> None:
    conn = open_db()
    sources = list(SOURCES) if args.source == "all" else [args.source]
    total = 0
    for s in sources:
        if s not in SOURCES:
            print(f"unknown source: {s}", file=sys.stderr)
            sys.exit(1)
        try:
            n = SOURCES[s](conn)
            print(f"[mine] {s}: {n} rows upserted")
            total += n
        except Exception as e:
            print(f"[mine] {s}: failed — {e}", file=sys.stderr)
    print(f"\ntotal: {total} rows across {len(sources)} source(s)")


def cmd_dump(args) -> None:
    conn = open_db()
    where = ""
    params: tuple = ()
    if args.source:
        where = "WHERE source = ?"
        params = (args.source,)
    cur = conn.execute(
        f"SELECT model, source, metric, value, scored_at FROM model_seeds {where} "
        f"ORDER BY model, source, metric",
        params,
    )
    rows = cur.fetchall()
    if not rows:
        print("(no seeds yet — run `mine --source all` first)")
        return
    cur_model = None
    for r in rows:
        if r["model"] != cur_model:
            cur_model = r["model"]
            print(f"\n{cur_model}")
        print(f"  {r['source']:20} {r['metric']:30} {r['value']:>10.3f}  scored_at={r['scored_at'] or '?'}")


def cmd_summary(args) -> None:
    conn = open_db()
    cur = conn.execute(
        "SELECT source, COUNT(DISTINCT model) AS n_models, COUNT(*) AS n_rows, "
        "MAX(pulled_at) AS last_pull FROM model_seeds GROUP BY source ORDER BY source"
    )
    print(f"{'source':<22} {'models':>6} {'rows':>6} {'last_pull':<27}")
    for r in cur:
        print(f"{r['source']:<22} {r['n_models']:>6} {r['n_rows']:>6} {r['last_pull'] or '?':<27}")


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest="cmd")
    m = sub.add_parser("mine")
    m.add_argument("--source", default="aider-edit",
                   help="aider-edit | aider-polyglot | swebench-verified | lmarena | all")
    m.set_defaults(func=cmd_mine)
    d = sub.add_parser("dump")
    d.add_argument("--source", default=None)
    d.set_defaults(func=cmd_dump)
    sub.add_parser("summary").set_defaults(func=cmd_summary)
    args = p.parse_args()
    if not getattr(args, "func", None):
        p.print_help()
        sys.exit(1)
    args.func(args)


if __name__ == "__main__":
    main()
