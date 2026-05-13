---
status: open
owner: claude-code
kanban: t_76fd3758
implementation_pr: null
superseded_by: null
effective_from: '2026-05-12'
effective_to: null
---

# Spec: swarm observability via chitin CLI

Date: 2026-05-12
Status: spec — open
Kanban: `t_76fd3758` (priority 70)
Author: claude-code (operator-controlled, spec writer)

## Problem

Today's swarm observability is an operator-side hack:

- `swarm/bin/swarm-audit` greps raw JSONL at `~/.chitin/gov-decisions-*.jsonl`.
- The 5-bullet summary it produces is written to `~/.openclaw/logs/swarm-audit.log` (operator-local file, not in chitin).
- Hermes' daily standup has no way to ingest the audit other than reading that log file.

Three downstream consequences:

1. **Past audit summaries aren't queryable.** They live in a log file, not the chain ledger. No temporal correlation with the decisions they summarize.
2. **Hermes (the natural daily-scan home, per memory `project_otel_emit_direction.md`) would have to read log files** to consume audit output — an off-channel pattern that breaks the "chitin is the canonical observability plane" principle.
3. **New downstream consumers (claude-mem, semantic indexers, dashboard) can't discover audit data** through the same CLI they use for raw decisions.

## Architectural principle

`chitin` is the canonical observability plane. **All analysis-relevant signals flow through chitin's chain ledger and are queried via `chitin-kernel` subcommands.** Operator-side log files are debug-only — not architectural.

This spec aligns swarm-audit + Hermes' standup with that principle. It is the substrate for the future semantic-indexer + dashboard work; once everything queries through one CLI, those layers compose cleanly.

## Current CLI surface (verified 2026-05-12)

```
chitin-kernel decisions recent [--window-hours N] [--limit N] [--dir <path>]
  → JSON array of decision rows from the chain ledger

chitin-kernel chain stats
  → markdown table grouped by tool_name: total / allows / denies / success%
  → currently all-time, no windowing

chitin-kernel chain summarize --session=<id>
  → markdown summary of ONE session (per-session, not time-range)

chitin-kernel chain replay --session=<id> [--policy-cwd=<d>] [--json]
  → re-evaluate a session against current policy; diff verdict

chitin-kernel chain related --entry-id=<id> [--file=<path> ...]
  → related sessions, most-recent + best-match first

chitin-kernel chain snapshot --session=<id>
  → session snapshot

chitin-kernel emit --event-file <json>
  → write a v2 Event into the chitin ingestion path
```

## What this spec changes

### Change 1 — swarm-audit reads via the CLI

Replace the raw-JSONL grep in `swarm/bin/swarm-audit` with `chitin-kernel decisions recent --window-hours 24 --limit 5000`. The aggregations (denials by rule × agent, unknown action_targets, protected-branch writes, etc.) continue to live in Python because today's audit-side bucketing is well-tested; `chitin-kernel` provides the row source, the script provides the lens.

The Python aggregation logic is intentionally retained rather than pushed into the kernel — most of it is heuristic ("agent-filtered detector for `git commit` outside checkout -b" etc.) and changes faster than the Go binary's release cycle. The kernel exposes structured data; the audit-side composes lenses on top.

### Change 2 — swarm-audit EMITS its summary into chitin

After producing the 5-bullet operator summary, the audit script writes a chain event:

```json
{
  "kind": "swarm.audit.summary",
  "v": 1,
  "ts": "2026-05-13T08:00:00Z",
  "agent": "claude-code",
  "driver": "swarm-audit",
  "payload": {
    "window_hours": 24,
    "facts": { … structured facts blob from gather_*() … },
    "bullets": [
      {"tag": "fix-now", "text": "..."},
      {"tag": "verify",  "text": "..."},
      {"tag": "watch",   "text": "..."},
      {"tag": "ok",      "text": "..."}
    ],
    "model": "openai-codex/gpt-5.5",
    "duration_ms": 21000
  }
}
```

Emitted via:

```
chitin-kernel emit --event-file /tmp/swarm-audit-<ts>.json
```

The event becomes a first-class chain artifact — queryable, replayable, indexable.

### Change 3 — Hermes' standup uses the CLI

The Hermes standup cron prompt (configured via `hermes cron edit <id> --message ...`) gains a chain-mining preamble that uses **only `chitin-kernel`**:

> Before composing the standup, run these queries and weave the output into your summary:
> - `chitin-kernel decisions recent --window-hours 6 --limit 500` — recent agent activity; flag any spike in denials by rule_id or agent
> - `chitin-kernel chain stats` — current aggregate by tool_name; identify outliers vs the last standup
> - `chitin-kernel chain related --entry-id <recent-swarm.audit.summary>` (if last audit was within 12h) — quote the top 2 bullets from the audit
>
> Do NOT read files in `~/.openclaw/logs/`. If you need chain data, ask chitin-kernel for it.

Hermes' identity as the "daily surface scan + operator-facing summary" layer (per the just-amended Hermes+Clawta spec, PR #545) is preserved. claude-code remains the on-demand deep-dive layer.

### Change 4 — retire `~/.openclaw/logs/swarm-audit.log` as a substantive surface

The audit script keeps writing to that log for crash-debug / operator inspection, but **no downstream consumer reads it**. Hermes ingests via `chitin-kernel chain related --kind swarm.audit.summary`; future consumers do the same.

## CLI extensions needed (small, scoped)

The current CLI is mostly sufficient; two small additions land alongside this spec:

1. **`chitin-kernel chain related --kind <event-kind>` flag** — currently `related` requires `--entry-id`. Add an event-kind filter so consumers can query the last N audit summaries without needing an entry hint: `chitin-kernel chain related --kind swarm.audit.summary --limit 5`. Backwards-compatible (additive flag).

2. **`chitin-kernel chain stats --window-hours <N>` flag** — currently `chain stats` is all-time. Add windowing so Hermes can ask for "last 6h aggregates" instead of accumulating-since-time-zero numbers. Backwards-compatible (default behavior preserved when flag is absent).

These are the only kernel-side changes in scope. Everything else is configuration / scripting.

## Acceptance

1. **`swarm-audit` no longer greps raw JSONL.** Code review: zero `gov-decisions-*.jsonl` open() / glob() calls remain in the script's data path. Data comes from `subprocess.run(['chitin-kernel', 'decisions', 'recent', ...])`.
2. **`swarm-audit` emits a `swarm.audit.summary` chain event** on every successful run. Verifiable: after a fresh `swarm-audit --dry-run` (or real run), `chitin-kernel chain related --kind swarm.audit.summary --limit 1` returns the just-emitted entry.
3. **Hermes' standup quotes a fresh chitin-kernel-derived stat** in its Discord summary. Verifiable: the trajectory for one standup-cron run shows tool calls to `chitin-kernel` and zero reads of `~/.openclaw/logs/swarm-audit.log`.
4. **Operator can answer "why were claude-code denials spiking yesterday?" using `chitin-kernel` alone**, no log-file reads. Smoke command:

   ```bash
   chitin-kernel decisions recent --window-hours 24 --limit 5000 \
     | jq '[.[] | select(.agent == "claude-code" and .allowed == false)] | group_by(.rule_id) | map({rule: .[0].rule_id, count: length}) | sort_by(-.count)'
   ```

5. **`chitin-kernel chain related --kind swarm.audit.summary --limit 5`** returns the last five audit summaries. (Requires the new `--kind` flag.)
6. **`chitin-kernel chain stats --window-hours 24`** returns aggregations limited to the last 24h. (Requires the new `--window-hours` flag.)

## Out of scope (separate followup tickets)

- **Semantic indexer over chain events** — claude-mem-adjacent layer that vectorizes chain payloads for fuzzy retrieval. Built ON TOP of this spec's event-emission contract; not part of it.
- **Web/TUI dashboard** — covered by the chitin-dashboard spec (PR #520, kanban `t_8f4d2ee5`). The dashboard's data source is the same CLI this spec aligns swarm-audit with — natural pairing, but separate scope.
- **Pushing the Python aggregation logic into chitin-kernel** — premature. Heuristics evolve faster than the Go binary; keep them in the script until the lens is stable.
- **Retroactive ingestion of historical `swarm-audit.log` entries** into chain events. Not worth the migration; the log only has ~1 day of history at any given time, and the audit is daily.
- **Multi-machine sync of audit events** — out for now (single-operator-machine deployment). Same shape as the broader chitin chain-ledger sync question.

## Implementation pointers for the worker

- **CLI flag additions** live in `go/execution-kernel/cmd/chitin-kernel/replay_cmd.go` (`cmdChainRelated` and `cmdChainStats`). Mirror the flag-parse pattern in `cmdDecisionsRecent` for `--window-hours`. For `--kind`, add it as a top-level filter applied before the existing entry-id / file-path heuristics.
- **swarm-audit refactor** is in `swarm/bin/swarm-audit`. Three changes:
  - `iter_ledger_rows_since(cutoff_iso)` → replaced with a subprocess call to `chitin-kernel decisions recent --window-hours <N> --limit 5000` returning JSON.
  - `gather_chain_facts(cutoff_iso)` → unchanged (still aggregates Python-side over the new row source).
  - `deliver(summary, dry_run)` → also emit a chain event before / instead of writing to `~/.openclaw/logs/`. Write the event JSON to `/tmp/swarm-audit-<ts>.json`, call `chitin-kernel emit --event-file /tmp/swarm-audit-<ts>.json`, then `rm` the temp file.
- **Hermes standup prompt** is configured via `hermes cron edit <job-id> --message <new-prompt>`. The exact cron job id is operator-local; pull it via `hermes cron list | grep grooming-standup`. The new prompt is the one in this spec's Change 3 section.
- **Tests:**
  - Go: add unit tests for the new CLI flags in `cmd/chitin-kernel/replay_cmd_test.go` (or equivalent).
  - Python: add a dry-run smoke that produces a parseable `swarm.audit.summary` event JSON. No need to actually `emit` it in CI.
  - End-to-end smoke: on a dev box, run `swarm-audit` and verify `chitin-kernel chain related --kind swarm.audit.summary --limit 1` returns the just-emitted entry.

## Event schema (`swarm.audit.summary` v1)

```jsonc
{
  "kind": "swarm.audit.summary",
  "v": 1,
  "ts": "<RFC3339 UTC>",
  "agent": "claude-code",
  "driver": "swarm-audit",
  "payload": {
    "window_hours": 24,
    "facts": {
      "chain": {
        "total_rows": 1234,
        "top_denials": [{"rule": "...", "action": "...", "agent": "...", "count": 42}],
        "unknown_action_targets": [{"target": "...", "count": 7}],
        "protected_branch_writes": [{"agent": "codex", "target": "git commit ...", "ts": "..."}],
        "clawta_spawn_count": 27
      },
      "kanban": {
        "lane_counts": {"triage": 28, "in_progress": 4, ...},
        "stale_in_progress": [{"id": "t_xxx", "stale_hours": 25.0, ...}],
        "recently_done": [{"id": "t_yyy", "title": "..."}]
      },
      "github": {
        "opened": [{"number": 543, "title": "..."}],
        "merged": [{"number": 541, "title": "..."}],
        "closed": []
      },
      "clawta": {
        "recent_ticks": 171,
        "dispatched_count": 9,
        "demoted_count": 12,
        "invariant_repairs": 2,
        "pr_reviews_posted": 0
      }
    },
    "bullets": [
      {"tag": "fix-now", "text": "..."},
      {"tag": "verify",  "text": "..."},
      {"tag": "watch",   "text": "..."},
      {"tag": "ok",      "text": "..."}
    ],
    "model": "openai-codex/gpt-5.5",
    "duration_ms": 21000
  }
}
```

The `payload.facts` block carries the structured aggregations the audit script produces today; `payload.bullets` carries the LLM-composed operator summary. Both can be consumed independently — a future dashboard might render `facts` as charts; Hermes' standup quotes `bullets`.

## Why this is the right scope for one ticket

- Three concrete, contained surfaces: swarm-audit Python, two small Go CLI flags, one Hermes prompt edit.
- Each change is independently testable and reversible.
- The event schema (`swarm.audit.summary` v1) is the only durable contract introduced; everything else is reversible code.
- Larger ambitions (semantic indexer, dashboard) are deliberately deferred. They build on top of this and need the event-emission contract to exist first.

## Followup tickets to file on implementation PR

1. **Build the chain-event semantic indexer** — vectorize `swarm.audit.summary` (and other event kinds) into a queryable store. The claude-mem-adjacent idea from earlier in 2026-05-12; relevant memory: `project_bounded_context_v1_schema.md`.
2. **Extend `chitin-kernel chain stats --window-hours` to support `--agent`, `--rule-id`, `--denied-only` filters.** Audit-script aggregations could then move into the kernel; not urgent.
3. **Migration plan for `~/.openclaw/logs/swarm-audit.log`** — after one week of dual-writing, drop the log file entirely (or downgrade it to debug-only with no operator-facing surface).

## Related

- Memory `project_otel_emit_direction.md` — chitin emits OTEL, doesn't ingest. This spec preserves that direction (audit emits INTO chitin, doesn't ingest FROM other systems).
- Memory `feedback_telemetry_first_pillar.md` — pull data before answering; ground improvements in observed signals. This spec is the substrate that makes that easy.
- Companion spec from today: `2026-05-12-no-gov-self-mod-bypass.md` (PR #548) — same canon-extension pattern that allows clean separation of "data plane" (chain ledger) from "policy plane" (rules).
- Existing chitin-dashboard spec: `2026-05-12-chitin-dashboard.md` (PR #520) — dashboard's data source aligns with the CLI surface this spec aligns swarm-audit with.
