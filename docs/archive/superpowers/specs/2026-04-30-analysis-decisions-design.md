---
status: draft
owner: claude-code
kanban: null
implementation_pr: null
superseded_by: null
effective_from: '2026-04-30'
effective_to: null
---

# Analysis Stream: Decisions — Design

**Status:** spec draft — first stream of the analysis layer (Milestone J3 framing). Foundation built here is reused by debt and soul-routing streams (deferred to post-talk).

**Author:** in-session sketch, 2026-04-30.

**Trigger:** the user's question "anything we can do with our data we have aggregated?" 1,225 gov-decisions across 6 days are sitting in `$HOME/.chitin/gov-decisions-*.jsonl` and nothing reads them. Chitin's strategic arc is *aggregate → policy → ecosystem → cloud*; aggregate is shipped, policy is the next loop to close. Analysis is the bridge: turn observed decisions into candidate rules.

**Forcing function:** 2026-05-07 talk. The analysis stream's headline demo is the closed-loop story: "we ran chitin on ourselves, observed N decisions, drafted a candidate rule, here's the PR."

---

## Positioning

The chitin loop today: **driver → gate → decision → audit log**. The decision is recorded but never reread. Analysis closes the loop:

```
                    ┌─────────────────────────────────────┐
                    │  driver (claude/copilot/openclaw)   │
                    └─────────────┬───────────────────────┘
                                  │ action proposal
                                  ▼
                    ┌─────────────────────────────────────┐
                    │  gov.Gate.Evaluate                  │
                    └─────────────┬───────────────────────┘
                                  │ allow/deny + reason
                                  ▼
                    ┌─────────────────────────────────────┐
                    │  gov-decisions-<date>.jsonl         │
                    └─────────────┬───────────────────────┘
                                  │ ← analysis reads here
                                  ▼
                    ┌─────────────────────────────────────┐
                    │  analysis (this spec)               │
                    │   - detect repeat patterns          │
                    │   - draft candidate rules           │
                    │   - emit JSON + markdown            │
                    └─────────────┬───────────────────────┘
                                  │ human-reviewed
                                  ▼
                    ┌─────────────────────────────────────┐
                    │  PR to chitin.yaml / rules          │
                    └─────────────────────────────────────┘
```

Analysis is **read-only** — it never writes to the chain, never proposes actions to drivers, never auto-PRs. Outputs are reports a human reviews. This matches the chitin-v2 architectural rule: Go kernel owns side effects; analysis is on the read side.

---

## Architecture: chain-canonical / projection (mirrors F4)

The analysis layer follows the same shape as F4's OTEL emit:

| Layer | Role | Format |
|---|---|---|
| Source of truth | gov-decisions JSONL (chain audit log) | append-only |
| Canonical analysis output | JSON (`out/<stream>-<date>.json`) | structured, regenerable |
| Projection | Markdown (`out/<stream>-<date>.md`) | human read |

Just as OTEL spans are non-authoritative projections of the chain, **markdown reports are non-authoritative projections of the analysis JSON**. JSON is the contract; markdown is for reading.

---

## Goals (in scope, v1)

1. Python package `python/analysis/` with a clean foundation: loaders, types, writers, common detection primitives.
2. Decisions stream — full implementation: load JSONL → detect patterns → draft candidate rules → emit JSON + markdown.
3. Layer 1 (deterministic) is the demo path. Zero network dependencies. Output is a pure function of input JSONL.
4. Layer 2 (LLM-drafted rules via `--llm-draft`) is opt-in, falls back to heuristic on failure.
5. Talk-day artifact: ranked top-N pattern catalog + candidate rule drafts for the headline patterns.
6. Stub modules for debt and soul-routing streams that import the foundation but return empty results — proves the foundation is shape-correct.

## Non-goals (separate work)

- Auto-PR generation. Humans apply drafts.
- Dashboard / web UI. JSON + markdown only in v1.
- Cross-stream correlation (e.g., "this decision pattern co-occurs with this debt finding"). v2.
- Live streaming / file-watcher mode. CLI-on-demand only.
- Schema migration tooling for the JSONL audit log.
- Inclusion of `flow_events.jsonl` (older swarm format, 105k events). Defer; the gov-decisions log is the canonical chitin source.

---

## Invariants (state the claims, then prove with code)

- **I1.** Given N decisions in window W, output is a ranked list of `(rule_id, action_type, agent_id)` tuples, sorted by count descending with **stable tie-breaker** alphabetic on `rule_id` → `action_type` → `agent_id`. Two runs over the same input produce byte-identical JSON.
- **I2.** Every run produces both `out/<stream>-<date>.json` (canonical) and `out/<stream>-<date>.md` (projection). The markdown is regenerable from the JSON alone — no input dependency.
- **I3.** Layer 1 (deterministic detection + heuristic templates) has zero network dependencies. Pure function of JSONL input.
- **I4.** With `--llm-draft` off (default), output is byte-identical given identical input *and identical `generated_at` clock*. The CLI accepts `--now <iso8601>` to fix the clock for tests; production runs stamp wall-clock.
- **I5.** A bad JSONL line never aborts a run. Parse errors are logged (count surfaced in `input_summary.parse_errors`) and the run continues. Audit-log integrity is the chain's job, not analysis's.

## Boundaries (Knuth: name them before the code)

- Empty input (0 decisions in window) → empty `patterns: []`, valid JSON, exit 0.
- Single decision → one pattern, count=1, draft attempted if template exists.
- All decisions same `(rule_id, action_type, agent_id)` → one pattern, count=N, ranked alone.
- Tied counts → tie-breaker invoked; never relies on dict iteration order.
- Decision with `rule_id: null` (allow-by-default, no rule fired) → bucket separately under `<none>`, do not mix with denies.
- Decision with `agent: null` or missing → use literal `"<unknown>"`, count it.
- Decision with malformed `ts` → skipped, counted in `input_summary.parse_errors`.
- Window boundary: `ts >= since AND ts < until`. Half-open. The boundary case (a decision at exactly `until`) belongs to the next window.

---

## Layout

```
python/analysis/
├── __init__.py
├── pyproject.toml         # uv-managed; depends only on stdlib + pyyaml
├── types.py               # Decision, Pattern, RuleDraft, Finding (dataclasses)
├── loaders.py             # load_gov_decisions(dir, window) → list[Decision]
├── detect.py              # detect_patterns(decisions) → list[Pattern]
├── draft.py               # draft_rule(pattern) → RuleDraft (heuristic dispatch)
├── llm_draft.py           # [Layer 2] enrich_with_llm(drafts) → list[RuleDraft]
├── writers.py             # write_json(...), write_markdown(...)
├── decisions.py           # main entry: python -m analysis.decisions
├── debt.py                # [stub] decisions-foundation reuser
├── souls.py               # [stub] decisions-foundation reuser
├── templates/             # one heuristic per rule_id family
│   ├── __init__.py        # registry + lookup
│   ├── no_destructive_rm.py
│   ├── bounds_max_files_changed.py
│   ├── no_curl_pipe_bash.py
│   └── no_force_push.py
└── tests/
    ├── test_loaders.py
    ├── test_detect.py
    ├── test_draft.py
    ├── test_writers.py
    └── fixtures/
        └── gov-decisions-fixture.jsonl
```

---

## CLI

```bash
python -m analysis.decisions \
  --window 7d \
  --top-n 10 \
  --out-dir out/ \
  [--llm-draft] \
  [--decisions-dir $HOME/.chitin]
```

**Defaults:**
- `--window 7d`
- `--top-n 10`
- `--out-dir python/analysis/out/` (gitignored)
- `--decisions-dir $HOME/.chitin`
- `--llm-draft` off

**Exit codes:**
- 0 — success (including empty-window)
- 2 — input dir doesn't exist
- 3 — write failure on output

---

## JSON schema (canonical)

```json
{
  "schema_version": "1",
  "stream": "decisions",
  "generated_at": "2026-04-30T12:34:56Z",
  "window": {
    "days": 7,
    "since": "2026-04-23T00:00:00Z",
    "until": "2026-04-30T12:34:56Z"
  },
  "input_summary": {
    "files_read": 6,
    "total_decisions": 1225,
    "allows": 1163,
    "denies": 62,
    "parse_errors": 0,
    "distinct_rule_ids": 14
  },
  "patterns": [
    {
      "rank": 1,
      "rule_id": "no-destructive-rm",
      "action_type": "shell.exec",
      "agent_id": "copilot-cli",
      "count": 19,
      "first_seen": "2026-04-23T...",
      "last_seen": "2026-04-30T...",
      "decision_class": "deny",
      "sample_envelope_ids": ["...", "...", "..."],   // first 3 envelope_ids for human spot-check; not the full set
      "draft": {
        "kind": "heuristic",
        "template": "no_destructive_rm",
        "confidence": "medium",
        "rule_yaml": "rules:\n  - id: no-destructive-rm-test-dirs\n    ...",
        "predicted_impact": {
          "samples_evaluated": 19,                     // ALL decisions in this pattern (= count above), not the 3 in sample_envelope_ids
          "would_allow": 12,
          "would_still_deny": 7,
          "method": "regex-match-on-action_target"
        },
        "notes": "..."
      }
    }
  ],
  "no_template_patterns": [
    {
      "rule_id": "envelope-exhausted",
      "action_type": "...",
      "agent_id": "...",
      "count": 2,
      "reason_no_template": "envelope-exhausted is a structural finding, not a rule-shape pattern"
    }
  ]
}
```

**Notes on schema:**
- `decision_class` is `"deny"` or `"allow"`. v1 ranks denies; allows are summarized in `input_summary` only.
- `predicted_impact.method` makes the heuristic self-describing — markdown can show it.
- `no_template_patterns` exists so the report doesn't lie by omission. If a pattern is high-count but we have no draft template, it still appears in the output, with `reason_no_template`.

---

## Markdown projection (sketch)

```markdown
# Decisions Analysis — 2026-04-30

**Window:** 7d (2026-04-23 → 2026-04-30)
**Input:** 1225 decisions (1163 allowed, 62 denied), 14 distinct rule_ids

## Top patterns

### #1 — no-destructive-rm × shell.exec × copilot-cli (19 denies)

First seen: 2026-04-23. Last seen: 2026-04-30.

**Candidate rule (heuristic, medium confidence):**

```yaml
rules:
  - id: no-destructive-rm-test-dirs
    when:
      action_type: shell.exec
      action_target: /^(rm|find).*-(rf|delete).*\/(tmp|test|out|graphify-out)/
    decide: allow
    reason: "cleanup of known-temp dirs"
```

**Predicted impact:** 12 of 19 sampled denials would become allows (regex-match-on-action_target). 7 still deny.

[Sample envelopes: env_abc123, env_def456, env_ghi789]

---

### #2 — ...
```

---

## Detection algorithm (Layer 1)

Trivially simple, deliberately so:

```python
def detect_patterns(decisions: list[Decision]) -> list[Pattern]:
    # Group by (rule_id, action_type, agent_id)
    buckets: dict[tuple, list[Decision]] = {}
    for d in decisions:
        if d.allowed:  # v1 ranks denies only
            continue
        key = (d.rule_id or "<none>", d.action_type or "<none>", d.agent or "<unknown>")
        buckets.setdefault(key, []).append(d)

    # Sort: count desc, then stable tie-breaker
    keys_sorted = sorted(
        buckets.keys(),
        key=lambda k: (-len(buckets[k]), k[0], k[1], k[2]),
    )
    return [Pattern.from_bucket(k, buckets[k], rank=i+1)
            for i, k in enumerate(keys_sorted)]
```

That's the whole detection engine. The honest name `detect_patterns` matches: it is exactly group-and-rank, no clustering, no anomaly detection, no ML. The complexity lives in the templates, not the engine.

---

## Templates (Layer 1: heuristic rule drafting)

Each template is a function `(pattern) -> RuleDraft | None`. Returns `None` if it doesn't know how to draft for this pattern. The `templates/__init__.py` registry maps `rule_id` → template function.

**v1 templates (prioritized by observed deny counts):**

| rule_id | template | strategy |
|---|---|---|
| `no-destructive-rm` (30 denies) | `no_destructive_rm.py` | regex-match `action_target` for known-safe dirs (test/, tmp/, out/) → propose `allow`-on-match exception |
| `bounds:max_files_changed` (3 denies) | `bounds_max_files_changed.py` | doc-batch detection: if all-changed paths share a common prefix in `docs/`, propose context-aware ceiling |
| `no-curl-pipe-bash` (3 denies) | `no_curl_pipe_bash.py` | extract URL host; if host in known-trusted list, propose host-allow exception |
| `no-force-push` (2 denies) | `no_force_push.py` | branch detection: propose allow on personal/feature branches, keep deny on main |

Patterns without a template appear in `no_template_patterns` — honest about analyzer limits, not silent.

**Predicted impact** is computed by running the proposed rule against the sample decisions. The `method` field describes how (`regex-match-on-action_target`, `path-prefix-match`, etc.). If the method is non-deterministic, the template returns `None` and the pattern goes to `no_template_patterns`.

---

## Layer 2 — LLM-drafted rules (opt-in)

When `--llm-draft` is set:

1. For each pattern in the top-N, build a prompt: the pattern, its sample decisions, the current `chitin.yaml` rules section.
2. Call local LLM via ollama (qwen3-coder).
3. LLM returns YAML rule + impact prediction in a structured format.
4. On any failure (LLM down, parse error, timeout) → fall back to heuristic template, mark `kind: "heuristic-fallback"`.

**Demo strategy:** Layer 2 is **off** during the talk. The talk demonstrates Layer 1, which is fully deterministic. Layer 2 ships in v1 but is positioned as upside, not load-bearing.

---

## Failure handling

- Bad JSONL line → log to stderr, increment `input_summary.parse_errors`, continue.
- Output dir doesn't exist → create it.
- Output write fails → exit 3 with stderr explanation. JSON is written before markdown so partial state is "JSON exists, markdown missing" — never reverse.
- LLM call fails (Layer 2) → heuristic fallback per pattern, log to stderr, continue.
- Decision with malformed `ts` → skip, count in parse errors. Don't fail the run.
- Decision with missing `rule_id` for a deny → `<none>` bucket. We surface "the gate denied without a rule_id" as its own finding — that's a chitin debt signal in itself.

---

## Talk-day demo path (2026-05-07)

```bash
$ cd ~/workspace/chitin
$ python -m analysis.decisions --window 7d
✓ Loaded 1225 decisions from $HOME/.chitin/ (6 files, 0 parse errors)
✓ Detected 10 deny patterns across 9 rule_ids
✓ Drafted 4 candidate rules (heuristic), 6 patterns lack templates
✓ Wrote python/analysis/out/decisions-2026-05-07.json
✓ Wrote python/analysis/out/decisions-2026-05-07.md

Top finding:
  rule_id=no-destructive-rm × shell.exec × copilot-cli — 19 denies
  → draft adds normalize() exempting test-dir cleanup
  → predicted: 12 allows, 7 still deny
  → see out/decisions-2026-05-07.md for the rule diff
```

The demo opens the markdown in an editor, shows the rule diff, applies it as a PR. **Closed loop, end to end, in two minutes.**

---

## Foundation extraction (the "C lean")

Although v1 is deep on decisions, the package layout is structured so debt and soul-routing can plug in as ~1-day additions:

- `loaders.py` — generic JSONL window loader; gov-decisions is the first caller, debt is the second (loads from `metrics.jsonl` and event chain), souls is the third.
- `types.py` — `Finding` is the abstract over `Pattern`. Debt findings are `Finding`s with a different shape; markdown writer dispatches on `stream` field.
- `writers.py` — `write_json` / `write_markdown` operate on `Finding` lists, not Pattern-specific.
- `detect.py` — the group-and-rank primitive is pulled into `_groupby_count_with_tiebreak()` so debt and souls reuse it.

The stubs `debt.py` and `souls.py` import the foundation, return empty findings, and write valid empty JSON/markdown. This proves the foundation generalizes before v2 is implemented.

---

## Implementation plan (preview)

The plan doc will be written separately via `/superpowers writing-plans`. Tasks (preview):

1. Survey: confirm top deny rule_ids on latest data (already done — see this spec).
2. Scaffold `python/analysis/` (pyproject, init, types, fixtures).
3. Loaders: parse gov-decisions JSONL with window filtering.
4. Detection engine: group-and-rank with tie-breaker. Tests for boundaries (empty / single / all-same / tied).
5. Templates: 4 heuristics for the observed top denies. Each gets its own test file.
6. Writers: JSON canonical + markdown projection. Determinism test (run twice, byte-equal).
7. CLI entry: `python -m analysis.decisions` + arg parsing + exit codes.
8. Stubs: `debt.py` + `souls.py` writing valid empty findings — proves foundation generalizes.
9. Layer 2: LLM-drafted rules with fallback path. Off by default.
10. End-to-end run on real `$HOME/.chitin/gov-decisions-*.jsonl` — verify talk-day output.
11. Talk-day rehearsal: clean run, screenshot of the markdown, dry-run of the demo.

---

## Out of scope, capture for later

- **Cross-stream findings** (decision pattern correlated with debt finding correlated with soul outcome) — v2 has the full closed loop.
- **Auto-PR generation** — once Layer 1 produces drafts the human trusts, automation can apply them. Not v1.
- **Streaming / live mode** — interesting for the dashboard; not needed for talk demo.
- **Schema migration of gov-decisions JSONL** — orthogonal; if the audit-log schema evolves, loaders adapt.
- **`flow_events.jsonl` ingestion** — older swarm format, 105k events; not chitin-native. Defer.
- **Multi-agent comparison reports** — soul-routing stream's territory.

---

## Spec self-review

- ✓ Invariants stated as one-sentence claims, code shape preserves them.
- ✓ Boundaries named: empty / single / all-same / tied / null fields / window edges.
- ✓ Tie-breaker is fully specified (alphabetic primary → secondary → tertiary).
- ✓ JSON is canonical; markdown is regenerable; demo path is deterministic.
- ✓ Real survey data drives template priority — no guessing.
- ✓ Failure modes are explicit; bad data never aborts a run.
- ✓ "C lean" is enforced by stubs that prove foundation generalizes.
- ✓ Forcing function (talk) shapes scope — Layer 1 is the demo path; Layer 2 is upside.
- ✓ Read-only side: no kernel writes, no auto-PR, no chain emission.

Open question for review:
- **Q.** Should v1 also rank *allows* (not just denies) so we surface "rule X allowed Y times for tool Z by agent W" as candidate-tighten patterns? Tightening is the dual of loosening. **Suggested answer:** v1 ranks denies only (the loosen direction is the immediate talk story). v2 adds allows ranking under the soul-routing stream where "agent A always allowed for tool T" is a soul-fit signal.
