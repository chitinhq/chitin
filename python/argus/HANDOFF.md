# Argus-Max — M1+M2 hand-off

What landed in this PR, what's wired up, what to do next.

## What this PR ships

### M1 — Foundation cleanup
- **`<think>` leak fixed.** `argus.llm.strip_thinking()` handles three formats:
  qwen3 `Thinking...\n...done thinking.\n\n<answer>`, `<think>` XML tags,
  and the `Thinking:` prefix. Reporter narration now lands clean prose.
- **Threshold config.** `~/.argus/config.yaml` (optional) overrides
  detector + kernel + Discord + policy settings. Sane defaults baked in.

### M2 — Continuous research kernel
- **`argus kernel`** runs a tick loop: heartbeat → cheap detectors →
  rate-limited Discord critical push → one LLM-bound task (narrate /
  keep-warm / engagement-meta) → journal entry → sleep. Default tick:
  60s. Pauses LLM work whenever the operator's opencode is active.
- **`argus.llm`** consolidates all qwen calls through ollama's HTTP API
  (`/api/chat` at `127.0.0.1:11434`). Model stays warm via 5min keepalive.
  `think=false` suppresses chain-of-thought at the protocol level.
  Includes a daily call cap (default 800), circuit breaker on consecutive
  failures, and a secret-redaction pass before logging prompts.
- **`argus.judge`** verifies every LLM output before it lands in a report
  or memory: structural check → citation-provenance bind → optional LLM
  judge with bias hardening. Citations must come from a precomputed
  expected set; hallucinated refs are rejected without an LLM call.
- **`argus.prompts.wrap_untrusted()`** is the single chokepoint for
  embedding indexed text (kanban comments, log lines, PR titles, etc.)
  into LLM prompts. Fence-break attempts are escaped; the system preamble
  tells the model to treat fenced content as data not instructions.

### Schema additions (all additive, idempotent)
- `events.source` and `events.payload_json` for cross-source ingest later.
- `findings` table — every detector emit persists here (deduped per hour
  by entity identity).
- `llm_calls` table — every qwen call logged with retention timestamp
  (30-day rotation) and redacted prompt.
- `hypotheses` + `evidence_links` tables — schema only; advancement
  logic lands in M3.
- `memory` table with `pinned` + `archived_ts` — schema only; dream-pass
  distillation lands in M4.
- `kernel_state` for kernel cross-tick state (e.g., last critical push).
- `schema_migrations` tracks applied versions.

### Agent contract — moved up from M6
- **`argus findings --since <ts> [--severity LEVEL] [--limit N] [--pretty]`**
  emits stable structured JSON. Schema versioned (currently v1). Hermes
  and Clawta can consume this directly.
- **`argus finding {ack,snooze,flag,apply} <id>`** lets the operator
  (or another agent) close the engagement loop on findings.
- **`argus action-rate`** prints the 7-day engagement metric.

### Safety hardening (peer-review revisions R-1 through R-13)
- Prompt-injection wrapper required at every untrusted-data site.
- SQLite read-only enforced via URI mode for non-writer paths.
- GPU politeness: defers when opencode sentinel was touched in last 5min,
  utilization > 85%, or VRAM free < 256 MiB.
- Hourly HTML snapshots dropped (graveyard, per operator-value review).
- `~/.argus/argus-state.html` is a free-running live page, local-only.
- `argus_meta:engagement` finding auto-fires when 7-day action rate < 10%.

## How to enable the kernel as a service

1. Install the unit:
   ```bash
   cp python/argus/systemd/argus-kernel.service ~/.config/systemd/user/
   systemctl --user daemon-reload
   systemctl --user enable --now argus-kernel.service
   ```
2. Optional: opencode-active sentinel wrapper so the kernel respects
   your live use:
   ```bash
   # add to your shell rc or wrap your opencode invocation:
   touch ~/.argus/operator-active
   opencode "$@"
   ```
3. Optional: edit `~/.argus/config.yaml` to tune cadence + thresholds.
   See `python/argus/config.py` for the full schema.

## Smoke-tested against real data

- 85/85 unit tests pass.
- Kernel ran 4 ticks against `~/.argus/index.db` (37k events, 369
  unique findings) — narration cited real rule_ids and agent names,
  zero `Thinking...` artifacts, GPU politeness pause confirmed via
  sentinel.
- `argus findings --since` round-tripped 246 findings as JSON.
- `argus finding ack 739` updated DB row.

## What's NOT yet implemented (follow-on PRs)

- **M3** — hypothesis advancement, policy proposal engine, EIG scoring.
  Schema is in place; the kernel just needs to grow the task classes.
- **M4** — dream pass distillation, memory weight decay, MEMORY.md
  renderer, Parquet archival.
- **M5** — cross-source ingest (kanban, git, logs, beliefs). Each is a
  small independent module.
- **M6** — Hermes-side fold (Argus producer-side is already shipped via
  the agent contract).
- **M7** — CodeAD-style detector synthesis. Deferred until the AST
  validator has a written threat-test suite.

## Reading the design

- `docs/superpowers/specs/2026-05-13-argus-max.md` — full design with
  embedded peer review revisions (search for "Revisions after peer review").

## Day-1 operational tips

- The live state page is at `~/.argus/argus-state.html`. Curl it or
  open in a browser — auto-refreshes every 30s.
- Tail the kernel journal: `tail -f ~/.argus/journal.ndjson | jq .`
- Watch GPU contention: `watch -n2 'jq .gpu_util_pct ~/.argus/heartbeat.json'`
- Force a narration right now: `argus kernel --tick-interval 1 --max-ticks 1`
- See findings since today started: `argus findings --since $(date -d "today 0:00" +%s) --pretty`
