"""Refresh stale model_seeds rows by web-searching for benchmark scores
and extracting them via the operator's own LLM.

Per docs/design/2026-05-06-stale-seed-refresh.md.

Invariant: a model is STALE iff
    M ∈ operator_matrix.json (in any cli's models[])
  AND NOT EXISTS seed where (seed.model contains a long-enough token of
      M's normalized form) AND seed.pulled_at > now - max_age.

`refresh-stale` visits exactly the stale set, never the fresh set.
That is the contract; if this implementation visits a fresh row, it's
a bug.

Usage:
    cd python/analysis && uv run python -m analysis.refresh_stale \\
        --max-age 7d [--dry-run] [--only <model>] [--backend tavily]

Requires:
  - operator_matrix.json (run `operator_matrix detect` first)
  - $TAVILY_API_KEY (or other configured backend's env var)
  - `claude` CLI authed (used for extraction; first commit only — once
    chitin role 'web-extract' lands, we route through chitin-execute-request)
"""
from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import sqlite3
import subprocess
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

from analysis.compatibility_seed import open_db, upsert_seed
from analysis.operator_matrix import (
    MIN_TOKEN_CHARS,
    _expand_search_tokens,
    _normalize_model_id,
    _resolve_lookup_token,
)
from analysis.search_backends import BackendError, get_backend

CHITIN_HOME = Path(os.environ.get("CHITIN_HOME") or os.path.expanduser("~/.chitin"))
MATRIX_JSON = CHITIN_HOME / "operator_matrix.json"

# Locked extraction prompt — see design doc §"The extraction step".
# Boundary cases the prompt must forbid ambiguity on are encoded here;
# do not soften without re-reading the design doc's invariants.
EXTRACTION_PROMPT = """You are extracting one benchmark score from search results.

Model under test: {model_id}

Top results:
{numbered_snippets}

Return JSON ONLY (no prose, no code fences):
{{
  "benchmark": "<lowercase short-name like 'aider-polyglot' or 'swebench-verified' or null>",
  "score": <number or null>,
  "score_unit": "percent" | "score" | "elo",
  "source_url": "<the result URL the score came from>",
  "scored_at": "<YYYY-MM-DD if stated, else null>"
}}

Rules:
  - "exact model": {model_id} is NOT the same as a sibling family
    member. Family-level matches are rejected.
  - "verifiable score": numeric, attached to a benchmark name, in the
    snippet text. Inferred or projected scores are rejected.
  - "score": a single number, never a range. If the source gives a
    range, return benchmark: null.

If no result has a verifiable score for THIS exact model, return:
{{"benchmark": null, "score": null, "score_unit": null, "source_url": null, "scored_at": null}}
"""

# Search query template per stale model. Kept simple — the design doc
# defers cross-referenced multi-query mode (--thorough flag) to a
# later commit.
SEARCH_QUERY_TEMPLATE = (
    "{model} benchmark score (SWE-bench OR aider-polyglot OR HumanEval "
    "OR LiveCodeBench OR MMLU) site:github.com OR site:arxiv.org OR "
    "site:openai.com OR site:anthropic.com OR site:deepmind.google"
)

# Where the failure-cache stub rows land — same model_seeds table,
# distinguished by source='web-search' + value=NULL semantics. Per
# design doc open question §3 (lean: cache failures, expose
# retry_failures_after for operators who disagree).
FAILURE_SOURCE = "web-search"
FAILURE_METRIC = "extraction_attempted"


# ─── Operator matrix → unique model set ────────────────────────────────

def load_reachable_models() -> list[str]:
    """Return the union of model ids across every authed CLI in
    operator_matrix.json. De-dups by normalized form."""
    if not MATRIX_JSON.exists():
        raise SystemExit(
            f"no operator matrix at {MATRIX_JSON} — "
            "run `python -m analysis.operator_matrix detect` first"
        )
    matrix = json.loads(MATRIX_JSON.read_text())
    seen: dict[str, str] = {}  # normalized → display
    for cli in matrix.get("clis", []):
        for m in cli.get("models", []):
            norm = _resolve_lookup_token(m)
            if norm and norm not in seen:
                seen[norm] = m
    return sorted(seen.values())


# ─── Staleness check ───────────────────────────────────────────────────

# Metrics that aren't benchmark scores. A model with ONLY these is
# still considered stale — pricing rows from openrouter, attempt-stubs
# from prior failures, context-length metadata. Operators run
# refresh-stale because they want SCORES; pricing alone doesn't satisfy.
NON_SCORE_METRIC_PREFIXES = ("cost_", "completion_token_", "prompt_token_")
NON_SCORE_METRICS = {"context_length", FAILURE_METRIC, "avg_tokens"}


def _is_score_metric(metric: str) -> bool:
    if metric in NON_SCORE_METRICS:
        return False
    return not any(metric.startswith(p) for p in NON_SCORE_METRIC_PREFIXES)


def is_stale(conn: sqlite3.Connection, model_id: str, max_age_days: int) -> bool:
    """A model is stale iff no benchmark-SCORE seed row exists for any
    expanded token of its normalized form, with pulled_at within
    max_age_days. Pricing-only and stub failure rows do NOT make a
    model fresh — operators run refresh-stale to get scores.

    Uses the SAME token expansion as operator_matrix.py's lookup so
    staleness and lookup agree (otherwise we'd refresh models that
    already have data, or skip models whose data isn't actually
    surfacing in the matrix)."""
    cutoff = (datetime.now(timezone.utc) - timedelta(days=max_age_days)).isoformat()
    tokens = _expand_search_tokens(_normalize_model_id(model_id))
    if not tokens:
        # Below MIN_TOKEN_CHARS — nothing matchable. Treat as stale so
        # the operator at least sees the attempt-failure record.
        return True
    placeholders = " OR ".join("LOWER(model) LIKE ?" for _ in tokens)
    cur = conn.execute(
        f"SELECT metric FROM model_seeds WHERE pulled_at > ? "
        f"AND ({placeholders})",
        [cutoff] + [f"%{t.lower()}%" for t in tokens],
    )
    return not any(_is_score_metric(row[0]) for row in cur.fetchall())


# ─── LLM extraction (claude --print for first commit) ─────────────────

class ExtractorError(Exception):
    pass


def _which_extractor() -> tuple[str, list[str]] | None:
    """First-commit extractor choice: prefer claude haiku (cheap +
    fast). Once chitin role 'web-extract' lands, this gets replaced
    with `chitin-execute-request --role web-extract`."""
    if shutil.which("claude"):
        return "claude", ["claude", "-p", "--model", "haiku", "--output-format", "text"]
    return None


def extract_via_llm(prompt: str, timeout: int = 60) -> dict | None:
    """Run prompt through the operator's LLM CLI; parse JSON response.

    Returns the parsed dict on success, None on extraction failure
    (LLM responded but didn't return parseable JSON, or returned the
    explicit null sentinel). Raises ExtractorError on infrastructure
    failure (CLI not found, timeout, non-zero exit)."""
    chosen = _which_extractor()
    if chosen is None:
        raise ExtractorError("no extractor CLI available — install `claude`")
    name, cmd = chosen
    try:
        proc = subprocess.run(
            cmd, input=prompt, capture_output=True, text=True, timeout=timeout
        )
    except subprocess.TimeoutExpired as e:
        raise ExtractorError(f"{name} timed out after {timeout}s") from e
    except FileNotFoundError as e:
        raise ExtractorError(f"{name} not on PATH") from e
    if proc.returncode != 0:
        raise ExtractorError(
            f"{name} exit {proc.returncode}: {proc.stderr[:200]}"
        )
    out = proc.stdout.strip()
    if not out:
        return None
    # Strip optional ``` fences / leading prose. Find first { and parse.
    m = re.search(r"\{.*\}", out, re.DOTALL)
    if not m:
        return None
    try:
        parsed = json.loads(m.group(0))
    except json.JSONDecodeError:
        return None
    if not isinstance(parsed, dict):
        return None
    if parsed.get("benchmark") is None or parsed.get("score") is None:
        return None
    return parsed


# ─── Refresh loop ──────────────────────────────────────────────────────

def refresh_one(
    conn: sqlite3.Connection,
    model: str,
    backend,
    n_results: int,
    dry_run: bool,
) -> dict:
    """Refresh ONE model. Returns {status: 'refreshed'|'no-data'|'error',
    detail: str}. Never raises."""
    q = SEARCH_QUERY_TEMPLATE.format(model=model)
    try:
        results = backend.query(q, n=n_results)
    except BackendError as e:
        return {"status": "error", "detail": f"backend: {e}"}

    if not results:
        if not dry_run:
            _record_failure(conn, model, "no search results")
        return {"status": "no-data", "detail": "0 search results"}

    snippets = "\n\n".join(
        f"[{i + 1}] {r['title']}\n    {r['url']}\n    {r['snippet'][:500]}"
        for i, r in enumerate(results)
    )
    prompt = EXTRACTION_PROMPT.format(
        model_id=model, numbered_snippets=snippets
    )

    if dry_run:
        return {"status": "would-refresh", "detail": f"{len(results)} results"}

    try:
        parsed = extract_via_llm(prompt)
    except ExtractorError as e:
        return {"status": "error", "detail": f"extractor: {e}"}

    if parsed is None:
        _record_failure(conn, model, "extraction returned null")
        return {"status": "no-data", "detail": "no verifiable score in results"}

    # Write the seed row. source='web-search' (per design §"failure
    # cache" — same source used for both successes and stub failures,
    # discriminated by metric).
    upsert_seed(
        conn=conn,
        model=model,
        source=FAILURE_SOURCE,
        metric=parsed["benchmark"],
        value=float(parsed["score"]),
        scored_at=parsed.get("scored_at"),
        raw_payload={
            "source_url": parsed.get("source_url"),
            "score_unit": parsed.get("score_unit"),
            "extracted_from": [r["url"] for r in results],
        },
    )
    return {
        "status": "refreshed",
        "detail": f"{parsed['benchmark']} {parsed['score']} ({parsed.get('source_url', '?')})",
    }


def _record_failure(conn: sqlite3.Connection, model: str, reason: str) -> None:
    """Failure-cache stub row: lets is_stale() see this model was
    already attempted and skip it for max_age_days."""
    upsert_seed(
        conn=conn,
        model=model,
        source=FAILURE_SOURCE,
        metric=FAILURE_METRIC,
        value=0.0,                # value column is NOT NULL, so 0.0 stub
        scored_at=None,
        raw_payload={"failure_reason": reason},
    )


# ─── CLI ───────────────────────────────────────────────────────────────

def _parse_max_age(s: str) -> int:
    """Accept '7d', '14d', '30d'. Returns days as int."""
    m = re.fullmatch(r"(\d+)d", s)
    if not m:
        raise argparse.ArgumentTypeError(
            f"--max-age must be like '7d', got {s!r}"
        )
    return int(m.group(1))


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--max-age", type=_parse_max_age, default=7,
                   help="age threshold in days (e.g. 7d). default: 7d")
    p.add_argument("--dry-run", action="store_true",
                   help="list stale models + estimated cost; don't search/extract")
    p.add_argument("--only", metavar="MODEL",
                   help="refresh only this one model id")
    p.add_argument("--backend", default="tavily",
                   help="search backend (default: tavily)")
    p.add_argument("--n-results", type=int, default=5,
                   help="search results per query (default: 5)")
    args = p.parse_args()

    conn = open_db()

    if args.only:
        candidates = [args.only]
    else:
        candidates = load_reachable_models()

    stale = [m for m in candidates if is_stale(conn, m, args.max_age)]
    print(
        f"refresh-stale: {len(stale)}/{len(candidates)} models stale "
        f"(max-age={args.max_age}d, backend={args.backend})"
    )
    if not stale:
        return

    if args.dry_run:
        for m in stale:
            print(f"  would-refresh: {m}")
        # Cost estimate — Tavily basic ~$0.005/call, claude haiku ~$0.0015/extract
        n = len(stale)
        print(f"\nestimated cost: ~${n * 0.005 + n * 0.002:.3f}")
        print("(cost varies by backend + extractor; this is a rough Tavily+claude-haiku ballpark)")
        return

    try:
        backend = get_backend(args.backend)
    except BackendError as e:
        print(f"backend init failed: {e}", file=sys.stderr)
        sys.exit(2)

    counts = {"refreshed": 0, "no-data": 0, "error": 0}
    for i, model in enumerate(stale, 1):
        result = refresh_one(conn, model, backend, args.n_results, dry_run=False)
        status = result["status"]
        counts[status] = counts.get(status, 0) + 1
        sigil = {"refreshed": "✓", "no-data": "—", "error": "✗"}.get(status, "?")
        print(f"  [{i}/{len(stale)}] {sigil} {model:40} {result['detail']}")
        conn.commit()

    print(f"\n{counts.get('refreshed', 0)} refreshed, "
          f"{counts.get('no-data', 0)} no-data, "
          f"{counts.get('error', 0)} error")


if __name__ == "__main__":
    main()
