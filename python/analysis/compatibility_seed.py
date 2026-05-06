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


# ─── SWE-bench (verified, lite, multimodal, bash-only, multilingual) ───────
#
# Pattern: download github.com/SWE-bench/experiments as a tarball ONCE
# (cached for 24h), walk evaluation/<variant>/<submission>/{metadata.yaml,
# results/results.json}, compute per-submission resolved_rate +
# attempts_count, write one model_seeds row per submission.
#
# Tarball is the right primitive vs hitting the GitHub API per-file
# (134+ submissions in `verified/` alone × 2 fetches = 268 API calls;
# unauthenticated quota is 60/hr, so per-file walk hits the wall fast).
# The tarball is ~12 MB, downloads in seconds.

import tarfile
import io
import re
from urllib.error import HTTPError, URLError

SWEBENCH_TARBALL_URL = (
    "https://codeload.github.com/SWE-bench/experiments/tar.gz/refs/heads/main"
)
SWEBENCH_CACHE = CHITIN_HOME / "cache" / "swebench-experiments-main.tar.gz"
SWEBENCH_CACHE_TTL_SECONDS = 24 * 60 * 60  # 24h
SWEBENCH_VARIANTS = ["verified", "lite", "multimodal", "bash-only", "multilingual"]


def _swebench_fetch_tarball() -> bytes:
    """Return the experiments tarball; serves from cache when fresh."""
    if SWEBENCH_CACHE.exists():
        age = datetime.now(timezone.utc).timestamp() - SWEBENCH_CACHE.stat().st_mtime
        if age < SWEBENCH_CACHE_TTL_SECONDS:
            return SWEBENCH_CACHE.read_bytes()
    print(f"[mine] fetching {SWEBENCH_TARBALL_URL} ...")
    blob = http_get(SWEBENCH_TARBALL_URL, timeout=120)
    SWEBENCH_CACHE.parent.mkdir(parents=True, exist_ok=True)
    SWEBENCH_CACHE.write_bytes(blob)
    return blob


# Tarball top-level dir name varies with branch hash; match anything.
_SWEBENCH_PATH_RE = re.compile(
    r"^[^/]+/evaluation/(?P<variant>[^/]+)/(?P<submission>[^/]+)/"
    r"(?P<file>metadata\.yaml|results/results\.json)$"
)


def _swebench_walk(blob: bytes):
    """Yield (variant, submission, filename, content_bytes) per matched file."""
    with tarfile.open(fileobj=io.BytesIO(blob), mode="r:gz") as tf:
        for member in tf:
            if not member.isfile():
                continue
            m = _SWEBENCH_PATH_RE.match(member.name)
            if not m:
                continue
            variant = m["variant"]
            if variant not in SWEBENCH_VARIANTS:
                continue
            f = tf.extractfile(member)
            if f is None:
                continue
            yield variant, m["submission"], m["file"], f.read()


def _swebench_parse_metadata(text: str) -> dict[str, Any]:
    """Tiny YAML walker for the metadata.yaml shape we care about:
    info.name, tags.model[0], tags.org. Avoids a yaml dep.
    """
    out: dict[str, Any] = {"name": None, "model": None, "org": None}
    in_info = in_tags = False
    in_model_list = False
    for raw in text.splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        if raw.startswith("info:"):
            in_info = True; in_tags = False; in_model_list = False
            continue
        if raw.startswith("tags:"):
            in_info = False; in_tags = True; in_model_list = False
            continue
        if in_info and raw.startswith("  name:"):
            out["name"] = raw.split(":", 1)[1].strip().strip('"').strip("'")
        if in_tags and raw.startswith("  model:"):
            in_model_list = True
            tail = raw.split(":", 1)[1].strip()
            if tail and tail != "":
                out["model"] = tail.strip('"').strip("'")
                in_model_list = False
            continue
        if in_model_list and raw.startswith("  - ") and out["model"] is None:
            out["model"] = raw[4:].strip().strip('"').strip("'")
            in_model_list = False
        if in_tags and raw.startswith("  org:"):
            out["org"] = raw.split(":", 1)[1].strip().strip('"').strip("'")
    return out


def _swebench_score_results(results: dict) -> dict[str, float]:
    """Project the results.json buckets into normalized metrics.

    SWE-bench results.json carries lists keyed by outcome:
      - resolved          : tasks the agent fixed
      - no_generation     : agent didn't produce a patch
      - no_logs           : log capture failed (treat as no-result, not failure)
      - failed_to_apply / failed_to_resolve / etc. (variant-dependent)

    resolved_rate = len(resolved) / total. total = sum of all top-level lists
    (those are mutually-exclusive task buckets per submission).
    """
    total = 0
    for v in results.values():
        if isinstance(v, list):
            total += len(v)
    if total == 0:
        return {}
    resolved = len(results.get("resolved", []) or [])
    no_generation = len(results.get("no_generation", []) or [])
    return {
        "resolved_rate": (resolved / total) * 100.0,  # keep 0-100 like Aider
        "no_generation_rate": (no_generation / total) * 100.0,
        "total_tasks": float(total),
    }


def mine_swebench(conn: sqlite3.Connection) -> int:
    blob = _swebench_fetch_tarball()
    by_submission: dict[tuple[str, str], dict[str, Any]] = {}
    for variant, submission, filename, content in _swebench_walk(blob):
        key = (variant, submission)
        s = by_submission.setdefault(key, {})
        if filename == "metadata.yaml":
            s["meta"] = _swebench_parse_metadata(content.decode("utf-8", errors="replace"))
        elif filename == "results/results.json":
            try:
                s["results"] = json.loads(content)
            except json.JSONDecodeError:
                continue

    n = 0
    for (variant, submission), s in by_submission.items():
        meta = s.get("meta") or {}
        results = s.get("results")
        if not results:
            continue
        # Use the human-readable submission display name (info.name)
        # as the model-row key — preserves tool+model combo when the
        # same base model is submitted by multiple tools.
        display = meta.get("name") or submission
        scores = _swebench_score_results(results)
        # Submission folder name encodes a date prefix: YYYYMMDD_*
        scored_at = None
        m = re.match(r"^(\d{4})(\d{2})(\d{2})_", submission)
        if m:
            scored_at = f"{m.group(1)}-{m.group(2)}-{m.group(3)}"
        for metric, value in scores.items():
            upsert_seed(
                conn,
                model=display,
                source=f"swebench-{variant}",
                metric=metric,
                value=value,
                scored_at=scored_at,
                raw_payload={
                    "submission": submission,
                    "model": meta.get("model"),
                    "org": meta.get("org"),
                },
            )
            n += 1
    conn.commit()
    return n


# ─── OpenRouter model catalog (pricing + context_length) ───────────────────
#
# OpenRouter aggregates 369+ models from many providers. Free,
# unauthenticated catalog API. Per-model: pricing.prompt (USD per
# token in), pricing.completion (USD per token out), context_length
# (max tokens). These let the routing layer answer "what does this
# model cost per call?" without per-provider scraping.

OPENROUTER_CATALOG_URL = "https://openrouter.ai/api/v1/models"


def mine_openrouter(conn: sqlite3.Connection) -> int:
    blob = http_get(OPENROUTER_CATALOG_URL, timeout=30)
    data = json.loads(blob)
    models = data.get("data") or []
    n = 0
    for m in models:
        model_id = m.get("id")
        if not model_id:
            continue
        pricing = m.get("pricing") or {}
        ctx = m.get("context_length")
        rows = []
        for k in ("prompt", "completion"):
            v = pricing.get(k)
            if v is None:
                continue
            try:
                fv = float(v)
            except (TypeError, ValueError):
                continue
            rows.append((f"{k}_token_cost_usd", fv))
        if isinstance(ctx, (int, float)):
            rows.append(("context_length", float(ctx)))
        for metric, value in rows:
            upsert_seed(
                conn,
                model=model_id,
                source="openrouter",
                metric=metric,
                value=value,
                scored_at=None,
                raw_payload={"id": model_id, "name": m.get("name")},
            )
            n += 1
    conn.commit()
    return n


# ─── LMArena — defer ───────────────────────────────────────────────────────
#
# Public API at /api/leaderboard 301→/arena.ai/api/leaderboard which 403s
# unauthenticated. The HF-hosted chatbot-arena-leaderboard Space renders
# its data at runtime and doesn't expose a static CSV/JSON. Two paths
# forward (deferred):
#   (a) Find the HF Space's data file in its git repo (some Spaces ship
#       leaderboard_table.csv but lmarena's didn't on first probe).
#   (b) Pull from arena-hard-auto-v0.1 dataset on HF (different
#       benchmark; auth-gated as of 2026-05-05).

def mine_lmarena(conn: sqlite3.Connection) -> int:  # noqa: ARG001
    print("[mine] lmarena: TODO — public endpoints 403/auth; needs HF Space data path")
    return 0


# ─── CLI ───────────────────────────────────────────────────────────────────

SOURCES = {
    "aider-edit": mine_aider_edit,
    "aider-polyglot": mine_aider_polyglot,
    "swebench": mine_swebench,        # all 5 variants in one walk
    "openrouter": mine_openrouter,
    "lmarena": mine_lmarena,           # stub (auth-gated)
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
