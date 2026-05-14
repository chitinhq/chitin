# chitin/python/analysis — Library Spec

**Status:** living spec for the `analysis` Python package. Extends the
decisions-stream design at
`docs/superpowers/specs/2026-04-30-analysis-decisions-design.md` to cover every
sibling module that now lives in this directory.

**Reading order:** start with §Positioning, then §Module map. The §Invariants
section is the contract every module is graded against — if a module's code or
docstring diverges from these, the docstring is wrong, not the spec.

---

## Positioning

`analysis/` is the **read side** of the chitin loop. The Go kernel owns side
effects (drivers, gates, chain emission). Python reads what the kernel wrote
and produces JSON + markdown reports a human reviews.

```
                  ┌──────────────────────────────────────┐
                  │  Go kernel (drivers, gov, chain)     │
                  └─────┬─────────────────┬──────────────┘
                        │ writes          │ writes
                        ▼                 ▼
            ~/.chitin/gov-decisions-*.jsonl
            ~/.chitin/events-*.jsonl
            ~/.codex/sessions/**/rollout-*.jsonl
            docs/debt-ledger.md
                        │ reads
                        ▼
                  ┌──────────────────────────────────────┐
                  │  analysis/  (this package)           │
                  │   - loaders, types, writers          │
                  │   - decisions / debt / souls streams │
                  │   - predict (P2 classifier)          │
                  │   - codex_mine / skill_mine          │
                  │   - floundering_calibration          │
                  └─────┬────────────────────────────────┘
                        │ writes (gitignored)
                        ▼
                  python/analysis/out/<stream>-<date>.{json,md}
```

Analysis **never** writes to the chain, never proposes actions to drivers,
never auto-PRs. It is observation-only.

---

## Module map

Each row names the one-sentence contract. Files prefixed `[stub]` are
foundation-generalization proofs that emit valid empty findings.

| Module | Contract |
|---|---|
| `__init__.py` | Public API surface — re-exports the canonical types and functions a consumer needs. |
| `__main__.py` | `python -m analysis` lists submodules and exits 2. |
| `models.py` | Dataclasses + JSONL parsers. `Decision`, `Pattern`, `RuleDraft`, `PredictedImpact`. Tolerant parser (`parse_decision_line`) never raises. |
| `loaders.py` | `load_gov_decisions(dir, Window) -> LoadResult`. Half-open window. Bad lines counted, never fatal. |
| `detect.py` | `detect_patterns(decisions) -> list[Pattern]`. Group-and-rank by (rule_id, action_type, agent_id) — denies only. Deterministic tie-breaker. |
| `draft.py` | `draft_for_pattern(p) -> RuleDraft \| None`. Looks up `REGISTRY[rule_id]` and dispatches. |
| `llm_draft.py` | `enrich_with_llm(pairs)`. Layer 2, opt-in. Any failure → `kind="heuristic-fallback"`. Off by default. |
| `writers.py` | `write_json` (canonical) + `write_markdown_from_json` (projection). JSON is the contract; markdown is regenerable. |
| `decisions.py` | CLI for the decisions stream. `python -m analysis.decisions`. |
| `debt.py` | (a) Stream stub — emits empty findings. (b) `load_ledger(path) -> LedgerLoadResult` — parses `docs/debt-ledger.md`'s yaml-fenced sections. |
| `souls.py` | [stub] Souls stream stub — emits empty findings to prove foundation generalizes. |
| `predict.py` | Stdlib logistic regression. `extract_features → train → predict`. Used by the kernel advisor's predicted-failure gate. |
| `codex_mine.py` | Post-hoc mine of `~/.codex/sessions/**/rollout-*.jsonl`. Two subcommands: `usage` (quota rollup) and `ingest` (project function_calls into chitin events JSONL). |
| `skill_mine.py` | n-gram surface of repeat workflows from `~/.chitin/events-*.jsonl`. Groups by `chain_id` (NOT by file). |
| `floundering_calibration.py` | Per-tier threshold suggestion for the kernel's floundering heuristic, derived from real session distributions. |
| `templates/__init__.py` | Template registry. Maps `rule_id` → function `(Pattern) -> RuleDraft \| None`. |
| `templates/<rule>.py` | One per known rule_id family. Heuristic / diagnostic / research kinds documented below. |
| `templates/all.py` | Side-effect import of every template (registers them). |
| `search_backends/base.py` | `SearchBackend` Protocol + `BackendError`. Empty results ≠ failure. |
| `search_backends/tavily.py` | One implementation. Reads credentials from env. |

---

## Invariants (the contract)

These are claims every relevant module must preserve. The mention in a
docstring is evidence; the test suite is the proof.

### I1 — Determinism by tie-breaker (decisions stream)

Given N decisions in window W, `detect_patterns` returns a ranked list whose
ordering is:

1. count descending
2. then alphabetic on `rule_id`
3. then `action_type`
4. then `agent_id`

Two runs over identical input produce byte-identical JSON. See
`detect.py::detect_patterns`. Tested in `tests/test_detect.py` and
`tests/test_writers_json.py`.

### I2 — JSON canonical, markdown projection

Every stream emits both `out/<stream>-<date>.json` and
`out/<stream>-<date>.md`. The markdown is regenerable from the JSON alone —
no input dependency. `write_markdown_from_json` reads only the JSON path.

### I3 — Layer 1 has zero network dependencies

The default code path is a pure function of the JSONL input. Networked code
lives behind opt-in flags: `--llm-draft` (decisions stream → `llm_draft.py`)
and the search-backends subpackage (separate caller).

### I4 — Determinism modulo wall clock

With `--llm-draft` off, output is byte-identical given identical input *and
identical `generated_at`*. CLIs accept `--now <iso8601>` to fix the clock for
tests; production stamps wall-clock time.

### I5 — Bad input never aborts a run

A malformed JSONL line, missing field, or coercion failure is counted in
`input_summary.parse_errors` and the run continues. Audit-log integrity is
the chain's job, not analysis's. The contract holds for:

- `models.parse_decision_line` — returns `None`, never raises.
- `loaders.load_gov_decisions` — counts parse errors, never raises.
- `debt.load_ledger` — increments `parse_errors`, single stderr warning at end.
- `codex_mine.iter_session_events` — skips bad lines silently (post-hoc).
- `skill_mine.iter_decision_events` — skips bad lines silently.

### I6 — Side-effect-free reads

No analysis module writes to the chain, the kernel state, or any
gov-decisions JSONL. Outputs only go to `out/` (gitignored), to user-named
paths for predict-model persistence, or to a usage-feed file under
`~/.cache/chitin/usage/`.

### I7 — Template return shape (`draft.py`)

A template function returns either:

- `RuleDraft` with `kind ∈ {"heuristic", "diagnostic", "research-prompt"}` —
  the template knows what to say for this pattern.
- `None` — template declines. Caller surfaces in `no_template_patterns`.

`RuleDraft.predicted_impact` is `Optional[PredictedImpact]`:
  - `kind == "heuristic"` (or `"heuristic-fallback"` / `"llm"`) MUST populate
    it — these drafts propose rule changes whose impact must be predictable.
  - `kind == "diagnostic"` / `"research-prompt"` MAY leave it `None` — these
    surface a finding for human review, not a rule change. The writer
    renders "_No predicted impact_" in markdown and `null` in JSON.

A template never raises. A template never makes a network call (Layer 1).

### I8 — Window semantics (`loaders.Window`)

Half-open: a decision is in the window iff `since <= ts < until`. A
boundary case (`ts == until`) belongs to the next window. Same convention
applies wherever a window is computed (`predict.py`, `floundering_calibration.py`).

---

## Boundaries (named, then code)

Per Knuth: every function names its empty / single / max / null cases before
the implementation runs.

### Decisions stream

- Empty input (0 decisions) → `patterns: []`, valid JSON, exit 0.
- Single decision → one pattern, count=1, draft attempted.
- All-same-bucket → one pattern, count=N.
- Tied counts → tie-breaker invoked (I1).
- `rule_id is None` → bucket as `"<none>"`.
- `agent is None` → bucket as `"<unknown>"`.
- Malformed `ts` → skipped, counted in parse_errors.
- Window edge: `ts == until` belongs to next window (I8).

### Predict

- Empty training set → degenerate model, `predict()` returns `base_rate` (0.0).
- Unknown `action_type` / `agent` at predict time → `"<unk>"` column.
- Class balance near 0.5 OR n_samples < 50 → `insufficient_signal: true`.
- Hour default uses UTC, not local — match training timestamps.

### Codex mine

- Missing sessions dir → empty list, no crash.
- `chain_id` containing path separators → sanitized via
  `_safe_chain_id_basename` to keep ingest writes inside `out-dir`.
- Function_call args not parseable as JSON → empty target string.

### Skill mine

- Events with no `chain_id` → bucket under `"<unknown>"` (not dropped).
- N-grams composed of all-same verbs or all-`read-*` → flagged trivial.

### Floundering calibration

- Session with zero writes → `max_stall_s` = full session duration.
- Session with one write → `max_stall_s` measured from first decision to that write.
- Driver not in `DRIVER_TIER` → tier `"unknown"`.

### Debt ledger

- Missing file → empty result, no raise.
- Yaml block missing a required field → skip + count parse_errors.
- `discovered_at` may be a `datetime`, `date`, or string — normalized to ISO-8601 string.

---

## CLI surface

```
python -m analysis                        # list submodules + exit 2
python -m analysis.decisions [--llm-draft]
python -m analysis.debt
python -m analysis.souls
python -m analysis.predict {train|predict}
python -m analysis.codex_mine {usage|ingest}
python -m analysis.skill_mine
python -m analysis.floundering_calibration
```

All CLIs accept `--out-dir` (default `python/analysis/out`) and a `--now`
override where time matters (I4).

Exit codes:

- `0` — success (including empty windows).
- `2` — input dir missing OR insufficient input.
- `3` — output-write failure (decisions stream).

---

## Output schema (canonical)

The JSON envelope every stream emits via `writers.write_json`:

```json
{
  "schema_version": "1",
  "stream": "decisions" | "debt" | "souls",
  "generated_at": "<iso8601>",
  "window": {
    "size": "7d",
    "since": "<iso8601>",
    "until": "<iso8601>",
    "total_seconds": <int>
  },
  "input_summary": { ... },
  "patterns": [ ... ],
  "no_template_patterns": [ ... ]
}
```

`schema_version="1"` is the contract handle. Consumers should branch on
`stream` for stream-specific fields and on `schema_version` for shape.

---

## Foundation extraction ("C lean")

The decisions stream is the deepest implementation. `debt` and `souls` stubs
exist to prove the foundation generalizes:

- `loaders.py` — generic JSONL window loader.
- `models.Finding` (currently `Pattern` plus `writers.Finding`) — the shape
  `write_json` operates on.
- `writers.py` — operates on `Finding` lists, not Pattern-specific.

When `souls` and `debt` ship real detection, no foundation change is needed.

---

## Test surface

`tests/` directory contents enforce the spec:

- `test_loaders.py` — half-open window, bad-line tolerance.
- `test_detect.py` — tie-breaker stability, boundary cases.
- `test_writers_json.py` — determinism (run twice, byte-equal).
- `test_writers_markdown.py` — markdown regenerable from JSON alone.
- `test_predict.py` — empty input → degenerate model; insufficient_signal flag.
- `test_template_<rule>.py` — per-template heuristic behavior.
- `test_templates_auto_register.py` — every template module imports without error.
- `test_codex_mine.py` — sanitization of chain_id; safe write paths.
- `test_skill_mine.py` — chain_id grouping across files; trivial-ngram filter.
- `test_debt_ledger.py` — yaml-fence parsing; parse_errors counting.
- `test_cli.py` — exit codes, `--now` determinism, end-to-end shape.
- `test_draft_registry.py` — template return-shape contract (I7).
- `test_types.py` — model coercion invariants (I5).
- `test_llm_draft.py` — fallback to heuristic on any failure.
- `test_stubs.py` — debt + souls emit valid empty findings.

A failing test invalidates the corresponding invariant claim. Fix the code,
not the test.

---

## Self-review

- ✓ Every module has a one-sentence contract.
- ✓ Invariants I1–I8 each cite at least one module that enforces them.
- ✓ Boundaries named before code — empty / single / all-same / tied / null /
  window-edge for every stream.
- ✓ JSON is canonical; markdown regenerable; demo path deterministic.
- ✓ Side-effect-free reads stated as I6.
- ✓ Network use behind opt-in flags (I3); default path is pure.

Open question for review:

- **Q.** Should `souls.py` be deleted now that the stream hasn't shipped real
  detection in three months, or kept as a foundation-generalization proof
  until v2? **Suggested answer:** keep until v2. The cost of the stub is one
  ~50-line file; the cost of foundation drift if we delete it and reintroduce
  it later is higher.
