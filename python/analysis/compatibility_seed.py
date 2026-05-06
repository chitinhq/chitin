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


# ─── EvalPlus (HumanEval+ / MBPP+) ─────────────────────────────────────────
#
# Source: github.com/evalplus/evalplus.github.io. results.json is a flat
# dict keyed by model name; per-model `pass@1` carries scores for
# humaneval, humaneval+, mbpp, mbpp+. Plus per-model `size` and `link`.
#
# These are the standard "code completion correctness" benchmarks. They
# don't measure agent / tool-use behavior — that's SWE-bench's job —
# but they're the cleanest measure of raw code generation capability.

EVALPLUS_RESULTS_URL = "https://raw.githubusercontent.com/evalplus/evalplus.github.io/main/results.json"


def mine_evalplus(conn: sqlite3.Connection) -> int:
    blob = http_get(EVALPLUS_RESULTS_URL, timeout=30)
    data = json.loads(blob)
    n = 0
    for model, entry in data.items():
        if not isinstance(entry, dict):
            continue
        scores = entry.get("pass@1") or {}
        for bench, value in scores.items():
            if not isinstance(value, (int, float)):
                continue
            upsert_seed(
                conn,
                model=model,
                source="evalplus",
                metric=f"pass@1_{bench}",     # → pass@1_humaneval, pass@1_humaneval+, pass@1_mbpp, pass@1_mbpp+
                value=float(value),
                scored_at=None,                # results.json doesn't carry a timestamp per model
                raw_payload={"size": entry.get("size"), "link": entry.get("link")},
            )
            n += 1
    conn.commit()
    return n


# ─── Arena Hard Auto (LMSYS-style head-to-head) ────────────────────────────
#
# Source: github.com/lmarena/arena-hard-auto/leaderboard/. CSV file
# named arena_hard_leaderboard_<DATE>.csv (latest dated 2024-07-31).
# Score is win-rate vs gpt-4-0314 baseline. Older than current models
# but the methodology generalizes; serves as a head-to-head signal.

ARENA_HARD_REPO_API = "https://api.github.com/repos/lmarena/arena-hard-auto/contents/leaderboard"
ARENA_HARD_RAW_BASE = "https://raw.githubusercontent.com/lmarena/arena-hard-auto/main/leaderboard"


def mine_arena_hard(conn: sqlite3.Connection) -> int:
    import csv
    import io as _io
    # Find latest dated CSV in the leaderboard/ dir.
    listing = json.loads(http_get(ARENA_HARD_REPO_API, timeout=30))
    csv_names = sorted([
        e["name"] for e in listing
        if isinstance(e, dict) and e.get("name", "").endswith(".csv")
    ], reverse=True)
    if not csv_names:
        print("[mine] arena-hard: no CSV files found in leaderboard/")
        return 0
    latest = csv_names[0]
    blob = http_get(f"{ARENA_HARD_RAW_BASE}/{latest}", timeout=30).decode("utf-8")
    # Use stdlib csv — arena-hard's CI column has embedded commas inside
    # quotes ("(-1.88, +1.97)"); a naive split breaks every row. csv.DictReader
    # handles quoted fields correctly.
    reader = csv.DictReader(_io.StringIO(blob))
    n = 0
    for rec in reader:
        model = rec.get("model") or rec.get("Model")
        if not model:
            continue
        for k, v in rec.items():
            if k is None or k.lower() == "model":
                continue
            try:
                fv = float(v)
            except (TypeError, ValueError):
                continue
            upsert_seed(
                conn,
                model=model,
                source="arena-hard",
                metric=k.replace(" ", "_").lower(),
                value=fv,
                scored_at=rec.get("date") or latest.replace("arena_hard_leaderboard_", "").replace(".csv", ""),
                raw_payload=rec,
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


# ─── LiveCodeBench / Terminal-Bench / OpenHands — defer ────────────────────
#
# LiveCodeBench: live-updated coding leaderboard at livecodebench.github.io
#   but the source repo doesn't ship a static results.json. Site fetches
#   from a backend; would need browser-rendering or a probe to find the
#   data endpoint. Defer.
# Terminal-Bench: github.com/laude-institute/terminal-bench has a
#   dashboard.py + sqlite-backed runs DB but no public results JSON in
#   the repo (the registry.json is a task-set catalog, not results).
#   Defer until a public results dump exists.
# OpenHands evaluation/benchmarks/: empty in their repo's contents API
#   call; their published SWE-bench numbers ARE in the swebench-* sources
#   above (they submit to SWE-bench under the OpenHands name). Don't
#   double-count.

def mine_livecodebench(conn: sqlite3.Connection) -> int:  # noqa: ARG001
    print("[mine] livecodebench: TODO — site is dynamic; no static JSON in repo")
    return 0


def mine_terminal_bench(conn: sqlite3.Connection) -> int:  # noqa: ARG001
    print("[mine] terminal-bench: TODO — repo has dashboard but no public results JSON")
    return 0


# ─── HuggingFace model cards ───────────────────────────────────────────────
#
# Per docs/design/2026-05-06-stale-seed-refresh.md update: structured
# sources before web-search. Vendors who write benchmarks in their HF
# README (z-ai/GLM-5.1, alibaba/Qwen3-Coder, meta-llama, etc.) publish
# rich rows here; vendors who defer to images (deepseek-ai) still produce
# 0 rows but cost almost nothing to probe. The pipeline is:
#
#   1. HF search API → canonical org/model id for each operator-reachable model
#   2. Fetch raw README from huggingface.co/<org>/<model>/raw/main/README.md
#   3. LLM-extract benchmark JSON via the operator's `claude` CLI
#   4. Upsert each (benchmark, score) into model_seeds with source='huggingface'
#
# Driven by operator_matrix.json — only mines models the operator can
# actually invoke. Skip models that don't appear in HF (404 on search).

HF_API_BASE = "https://huggingface.co/api"
HF_RAW_BASE = "https://huggingface.co"

# Locked extraction prompt — multi-benchmark variant of refresh_stale's
# single-score prompt. The model card is mostly prose + occasional
# tables; we want EVERY numeric benchmark mention, not the highest one.
HF_EXTRACTION_PROMPT = """You are extracting all benchmark scores from a HuggingFace model card.

Model under test: {model_id}

Card content (truncated to 8000 chars):
---
{readme}
---

Return a JSON array (no prose, no code fences). Each entry is one
benchmark score for THIS model:

[
  {{"benchmark":"<lowercase short-name like 'humaneval' or 'swe-bench-verified'>",
    "score":<number>,
    "score_unit":"percent"|"score"|"elo"|"dollars"|"points",
    "variant":"<which variant of the model, or null if only one model>",
    "metric_type":"pass@1"|"resolved"|"pass_rate"|null}}
]

Rules:
  - One entry per (benchmark, variant) pair. If the card lists the same
    benchmark on multiple variants (16B / 236B / etc.), include all.
  - "verifiable score": numeric, attached to a benchmark name, in the
    text. Inferred or projected scores are rejected.
  - Skip rows that compare against OTHER models (e.g. "X scored Y on
    HumanEval" where X is not {model_id}).
  - If the card has no benchmarks (only prose / images), return [].
"""


# Vendor-org allowlist. HF search returns community distillations
# ("Andycurrent/Gemma-3-1B-it-GLM-4.7-Flash-Heretic-Uncensored")
# before canonical vendor cards because fork names contain the model
# name verbatim. When a model query maps to a known vendor, scope the
# search with `&author=<org>`. Add new prefixes here as new vendors emerge.
HF_VENDOR_ORGS = {
    "qwen":         "Qwen",
    "glm":          "zai-org",
    "deepseek":     "deepseek-ai",
    "gemma":        "google",
    "mistral":      "mistralai",
    "llama":        "meta-llama",
    "phi":          "microsoft",
    "yi":           "01-ai",
}

# Deployment-form suffixes that aren't part of the model identity.
# `glm-5.1-cloud` is the cloud-served form of `glm-5.1`; HF only knows
# the latter. Strip these before searching.
HF_DEPLOYMENT_SUFFIXES = ("-cloud", "-fp8", "-int8", "-int4", "-gguf", "-mlx")


def _hf_search_query(model_query: str) -> tuple[str, str | None]:
    """Return (search_term, vendor_org_filter). Vendor filter is None
    if we don't recognize the family — broad search then."""
    q = model_query.lower()
    for suffix in HF_DEPLOYMENT_SUFFIXES:
        if q.endswith(suffix):
            q = q[: -len(suffix)]
            break
    vendor = None
    for prefix, org in HF_VENDOR_ORGS.items():
        if q.startswith(prefix):
            vendor = org
            break
    return q, vendor


def _hf_find_canonical(model_query: str) -> str | None:
    """HF search → canonical modelId. Scopes by vendor org when the
    family is known (Qwen, zai-org, etc.) — community distillations
    cannot win against the vendor's own card. Returns None on 404 /
    empty / no acceptable match."""
    import urllib.parse
    search_term, vendor = _hf_search_query(model_query)
    q = urllib.parse.quote(search_term)
    url = f"{HF_API_BASE}/models?search={q}&limit=10&sort=downloads&direction=-1"
    if vendor:
        url += f"&author={urllib.parse.quote(vendor)}"
    try:
        raw = http_get(url, timeout=15)
    except Exception:
        return None
    try:
        results = json.loads(raw)
    except json.JSONDecodeError:
        return None
    if not results:
        # Vendor-scoped search returned nothing — try unscoped as a fallback
        # so we don't lose models whose vendor we got wrong.
        if vendor:
            return _hf_find_canonical_unscoped(search_term, model_query)
        return None
    return _hf_pick_best(results, model_query)


def _hf_find_canonical_unscoped(search_term: str, original: str) -> str | None:
    import urllib.parse
    q = urllib.parse.quote(search_term)
    url = f"{HF_API_BASE}/models?search={q}&limit=10&sort=downloads&direction=-1"
    try:
        raw = http_get(url, timeout=15)
        results = json.loads(raw)
    except Exception:
        return None
    if not results:
        return None
    return _hf_pick_best(results, original)


def _hf_pick_best(results: list[dict], model_query: str) -> str | None:
    """Pick the best modelId from search results. Strict-match first
    (modelId must contain all needles ≥ 2 chars from query); fallback
    to first result. Vendor scoping already done upstream."""
    needles = {p.lower() for p in model_query.replace("_", "-").split("-") if len(p) >= 2}
    for r in results:
        mid = r.get("modelId") or r.get("id") or ""
        mid_lower = mid.lower().replace("_", "-")
        if all(n in mid_lower for n in needles):
            return mid
    return results[0].get("modelId") or results[0].get("id")


def _hf_fetch_readme(model_id: str) -> str | None:
    """Fetch raw README. Returns None on 404 or empty body."""
    url = f"{HF_RAW_BASE}/{model_id}/raw/main/README.md"
    try:
        return http_get(url, timeout=20).decode("utf-8", errors="ignore")
    except Exception:
        return None


def _hf_extract_benchmarks(model_id: str, readme: str) -> list[dict]:
    """Run the locked extraction prompt against the operator's `claude`
    CLI. Returns the parsed JSON array (possibly empty); raises on
    infrastructure failure."""
    # Local import — refresh_stale already wraps subprocess+JSON for us.
    from analysis.refresh_stale import extract_via_llm, ExtractorError
    import re

    prompt = HF_EXTRACTION_PROMPT.format(
        model_id=model_id, readme=readme[:8000]
    )
    # extract_via_llm returns dict-or-None, but we want an array. Bypass
    # its inner schema validation by replicating the subprocess call.
    try:
        # Reuse the LLM call pattern but parse as array
        import shutil, subprocess
        if not shutil.which("claude"):
            raise ExtractorError("claude CLI not on PATH")
        proc = subprocess.run(
            ["claude", "-p", "--model", "haiku", "--output-format", "text"],
            input=prompt, capture_output=True, text=True, timeout=90,
        )
        if proc.returncode != 0:
            raise ExtractorError(f"claude exit {proc.returncode}: {proc.stderr[:200]}")
        out = proc.stdout.strip()
    except subprocess.TimeoutExpired as e:
        raise ExtractorError(f"claude timed out after 90s") from e
    if not out:
        return []
    # Strip optional code fences; find first [ ... ]
    m = re.search(r"\[.*\]", out, re.DOTALL)
    if not m:
        return []
    try:
        parsed = json.loads(m.group(0))
    except json.JSONDecodeError:
        return []
    if not isinstance(parsed, list):
        return []
    return parsed


def _hf_canonical_metric(benchmark: str, metric_type: str | None) -> str:
    """Normalize the LLM-extracted benchmark name to a stable metric id.
    The matrix display indexes on metric prefix patterns
    ('aider*', 'swebench*'); keep names predictable."""
    b = benchmark.lower().strip().replace(" ", "-")
    # Common renames so similar benchmarks collapse on lookup
    aliases = {
        "humaneval+": "pass@1_humaneval+",
        "humaneval": "pass@1_humaneval",
        "mbpp+": "pass@1_mbpp+",
        "mbpp": "pass@1_mbpp",
        "swe-bench": "swebench-verified",
        "swe-bench-verified": "swebench-verified",
        "swe-bench-pro": "swebench-pro",
        "terminal-bench": "terminal-bench",
        "terminal-bench-2.0": "terminal-bench-2.0",
    }
    if b in aliases:
        return aliases[b]
    # Tag pass@1 / resolved variants when the LLM provided metric_type
    if metric_type and not b.startswith(metric_type):
        return f"{b}_{metric_type}"
    return b


# Closed-source vendors don't publish on HuggingFace. Their model names
# (claude-*, gpt-*, gemini-*) match weird community distillations like
# "Qwen3-27B-Claude-Opus-Distilled" that aren't actually those models.
# Skip these prefixes entirely — they should get scores from leaderboards
# (already mined) and refresh-stale (web-search) instead.
HF_CLOSED_SOURCE_PREFIXES = ("claude-", "gpt-", "gemini-", "o1-", "o3-")


def _hf_should_skip(model_query: str) -> bool:
    return any(model_query.startswith(p) for p in HF_CLOSED_SOURCE_PREFIXES)


def mine_huggingface(conn: sqlite3.Connection) -> int:
    """Walk operator_matrix.json's reachable models. For each, try to
    find a canonical HF id, fetch the README, LLM-extract benchmarks,
    upsert. Returns total rows written."""
    matrix_path = Path(os.path.expanduser("~/.chitin/operator_matrix.json"))
    if not matrix_path.exists():
        print(
            "[mine] huggingface: no operator_matrix.json — "
            "run `python -m analysis.operator_matrix detect` first",
            file=sys.stderr,
        )
        return 0

    matrix = json.loads(matrix_path.read_text())
    seen_queries: set[str] = set()
    total = 0
    for cli in matrix.get("clis", []):
        for raw_model in cli.get("models", []):
            # Same normalization the matrix lookup uses, so we mine
            # the SAME identifier the report later searches for.
            from analysis.operator_matrix import _resolve_lookup_token
            search_query = _resolve_lookup_token(raw_model)
            if not search_query or search_query in seen_queries:
                continue
            seen_queries.add(search_query)

            if _hf_should_skip(search_query):
                print(f"[mine] huggingface: {search_query:40} → skip (closed-source vendor; HF will only have community forks)")
                continue

            canonical = _hf_find_canonical(search_query)
            if not canonical:
                print(f"[mine] huggingface: {search_query:40} → no HF match")
                continue
            readme = _hf_fetch_readme(canonical)
            if not readme:
                print(f"[mine] huggingface: {search_query:40} → {canonical} (no README)")
                continue

            try:
                benchmarks = _hf_extract_benchmarks(canonical, readme)
            except Exception as e:
                print(f"[mine] huggingface: {search_query:40} → {canonical} (extractor failed: {e})")
                continue

            if not benchmarks:
                print(f"[mine] huggingface: {search_query:40} → {canonical} (no benchmarks in card)")
                continue

            # Write each benchmark as a separate seed row.
            n_this = 0
            for entry in benchmarks:
                bench = entry.get("benchmark")
                score = entry.get("score")
                if not bench or score is None:
                    continue
                try:
                    score_f = float(score)
                except (TypeError, ValueError):
                    continue
                metric = _hf_canonical_metric(bench, entry.get("metric_type"))
                upsert_seed(
                    conn=conn,
                    model=search_query,            # store under resolved name
                    source="huggingface",
                    metric=metric,
                    value=score_f,
                    raw_payload={
                        "hf_canonical": canonical,
                        "operator_alias": raw_model if raw_model != search_query else None,
                        "score_unit": entry.get("score_unit"),
                        "variant": entry.get("variant"),
                    },
                )
                n_this += 1
            print(f"[mine] huggingface: {search_query:40} → {canonical} ({n_this} benchmarks)")
            total += n_this
            conn.commit()
    return total


# ─── CLI ───────────────────────────────────────────────────────────────────

SOURCES = {
    "aider-edit": mine_aider_edit,
    "aider-polyglot": mine_aider_polyglot,
    "swebench": mine_swebench,        # all 5 variants in one walk
    "openrouter": mine_openrouter,
    "evalplus": mine_evalplus,        # HumanEval+ / MBPP+
    "arena-hard": mine_arena_hard,    # LMSYS-style head-to-head (older corpus)
    "huggingface": mine_huggingface,  # vendor-published model cards
    "lmarena": mine_lmarena,           # stub (auth-gated)
    "livecodebench": mine_livecodebench,  # stub (dynamic site)
    "terminal-bench": mine_terminal_bench,  # stub (no public results dump)
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
