# Stale-seed refresh: web-search-augmented mining

Status: design. Companion to `2026-05-05-conformance-substrate.md`
(seed sourcing layer §107).

Date: 2026-05-06

## Goal, in one sentence

Any operator running chitin can keep their `model_seeds` table fresh
against newly-released models — without depending on Anthropic's
WebSearch tool, without leaking the operator's repo data, using only
the LLM and search credentials the operator already brings.

## The invariant

A model is **stale** iff:

```
operator_matrix.json contains the model
AND max(model_seeds.pulled_at WHERE model LIKE %model%) < (now - max_age)
```

`refresh-stale` visits exactly the stale set, never the fresh set.
That is the contract; if the implementation visits a fresh row, it's
a bug.

## The pluggable backend interface

```python
class SearchBackend(Protocol):
    """One call. Returns top-N results with title + url + snippet.
    Raises BackendError for credential / quota / network issues — never
    returns []  for those (we want to distinguish 'no results' from
    'backend broken')."""
    def query(self, q: str, n: int = 5) -> list[SearchResult]: ...

class SearchResult(TypedDict):
    title: str
    url: str
    snippet: str          # backend-provided summary; may be empty
```

Implementations land in `python/analysis/search_backends/`:
  - `tavily.py`    — needs `TAVILY_API_KEY`
  - `serper.py`    — needs `SERPER_API_KEY`
  - `brave.py`     — needs `BRAVE_SEARCH_API_KEY`
  - `searxng.py`   — needs `SEARXNG_URL` (self-hosted, no API key)

Selected via `chitin.yaml`:

```yaml
seeds:
  web_search:
    backend: tavily            # | serper | brave | searxng
    max_results_per_model: 5
    extraction_role: web-extract   # which chitin role does the LLM extraction
    confidence: low             # all web-search seeds tagged low-confidence
                                # (sorted below leaderboard seeds in matrix)
  refresh:
    max_age_days: 7
    rate_limit_per_min: 30      # cap search backend calls
```

No backend pre-selected: chitin will refuse to run the refresh
command without `seeds.web_search.backend` set, and print the env
var the chosen backend needs.

## The extraction step

The operator's own LLM does the extraction. Routed through
`chitin-execute-request --role web-extract` so it inherits all the
same governance + cost-tracking the operator already has wired.

Prompt template (locked, no model-specific tweaks):

```
You are extracting one benchmark score from search results.

Model under test: {model_id}

Top {n} results:
{numbered_snippets}

Return JSON ONLY:
{ "benchmark": "<lowercase short-name>", "score": <number>,
  "score_unit": "percent" | "score" | "elo",
  "source_url": "<the result URL the score came from>",
  "scored_at": "<YYYY-MM-DD if stated, else null>" }

If no result has a verifiable score for this exact model, return:
{ "benchmark": null }
```

Boundary cases the prompt forbids ambiguity on:
  - "exact model": `claude-opus-4-7` ≠ `claude-opus-4-5`. Family
    matches are NOT acceptable here — the family fallback already
    exists in the matrix display layer.
  - "verifiable score": numeric, attached to a benchmark name, in the
    snippet text. Inferred or projected scores are rejected.
  - "score": a number, never a range. If the source gives a range,
    extraction returns null.

Extraction failure ≠ crash. The model is left stale; the operator
sees `(no benchmark data — refresh attempted YYYY-MM-DD)` instead of
`(no benchmark data — too new)` so they know it's been tried.

## The new CLI command

```
$ chitin seeds refresh-stale [--max-age 7d] [--dry-run] [--only <model>]
```

- `--dry-run` lists the stale set + estimated cost (search calls × cost
  + LLM calls × cost) without spending anything.
- `--only <model>` refreshes one model — useful when a new model drops
  and you don't want to wait for the cron.

Output (terse):

```
refresh-stale: 12 models stale (max-age=7d)
  searching tavily (5 results × 12 = 60 calls, ~$0.06)
  extracting via T0=hermes (12 calls)
  ─────
  ✓ claude-opus-4-7   aider-polyglot 78.2  (https://...)
  ✓ gpt-5.5           swebench-verified 71.0 (https://...)
  ⚠ gpt-5.4-mini      no score found in top 5 results
  ...
  10 refreshed, 2 left stale
  cost: $0.08 search + $0.003 LLM
```

## Recommended weekly run

Phase-2 extension to `chitin doctor install`:

```
$ chitin doctor install --enable-seed-refresh
  installing systemd timer chitin-seed-refresh.timer (weekly Mon 02:00)
  ...
```

Before this lands, the operator can do it themselves with a one-liner
in their crontab. Document in `docs/operator-guide.md`.

## Open questions (operator decides before we code)

1. **Confidence weighting in routing.** Web-search seeds get
   `confidence: low` by default. Should the routing query down-weight
   them (e.g., 0.5×) or just sort them visually but treat the score
   as authoritative? Lean: down-weight in routing, present at full
   value in the matrix display. Operators who want different can
   override via `seeds.web_search.confidence`.

2. **Quality vs cost trade.** 1 search × top-5 results vs 3 searches
   × top-3 results (different query phrasings → cross-reference).
   The latter catches more, costs ~3×. Lean: ship 1×5 first; expose
   a `--thorough` flag that does 3×3 for operators who care.

3. **Failure cache.** When extraction returns null, do we record
   `pulled_at` anyway so we don't re-try on every run? Lean: yes,
   write a stub row with `value=null` + `raw_payload="extraction
   returned null"` so refresh-stale skips it for `max_age_days`. Set
   `seeds.refresh.retry_failures_after` separately if operator wants
   to try again sooner.

## Defer list (NOT in this commit)

- **Harness/driver discovery.** Separate command, much lower
  cadence (monthly?), surfaces candidates for manual triage rather
  than auto-mining. Lives in `compatibility_seed.py harness-discover`.
- **PDF/preprint extraction.** Many fresh model results land in arxiv
  PDFs before they hit blog posts. The HTML-only extraction will miss
  these. Defer; surface as an alarm when ≥3 stale models in a row
  return null on extraction.
- **Score corroboration.** Two web-search seeds for the same (model,
  benchmark) from different URLs that disagree by >10pp → flag as
  conflicting, don't auto-resolve. Defer.

## Why this commit is small

Scope is the interface, the tavily backend, the extraction prompt,
and the `refresh-stale` CLI command. Adding the other 3 backends is
copy-paste-against-Protocol. Adding the systemd timer is copy from
the existing `chitin-mine-daily` pattern. We get an end-to-end seed
refresh working in one commit, and the rest is incremental.
