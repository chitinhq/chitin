# Hermes Staged Tick v1 — Design

**Date:** 2026-04-22
**Status:** Design — ready for user review, then handoff to writing-plans.
**Supersedes:** `docs/superpowers/specs/2026-04-21-hermes-autonomy-v1-design.md` (abandoned — see `docs/observations/2026-04-22-autonomy-v1-post-mortem.md`).
**Relates to:**
- `docs/observations/2026-04-22-autonomy-v1-post-mortem.md` (root-cause analysis that motivates this design)
- Chitin governance v1 (PR #45, merged `5cc74bbe8a`) — provides the tool-boundary enforcement layer this design relies on
- Octi Pulpo (`~/workspace/octi`, archived) — source of the stage→tier mapping pattern ported here

## Preamble

The hermes autonomy v1 canary (PR #43) failed in the most instructive
way possible: a single-session tick with a prompt-asked delegate
pattern let the primary model ignore the delegate and execute its own
plan — a destructive one. Governance v1 (PR #45) patched the
enforcement layer so destructive actions are blocked at the tool
boundary regardless of which model decides to attempt them. But
enforcement only prevents harm; it does not allocate work correctly.
Two problems remain:

1. **qwen3-coder:30b (local, on the user's 3090) is idle.** As primary
   it runs whole-tick judgment — picking issues, scoping actions,
   deciding whether to delegate — work it's not well suited for, and
   it defaults to over-conservative behavior (zero grooming label
   proposals in 14h of ticks on 2026-04-22).
2. **glm-5.1:cloud (delegate) is underused.** As `delegate_task`
   target it only runs when primary chooses to delegate, which — per
   the empirical 2026-04-22 observation — rarely matches "every
   judgment call."

The architectural answer comes from octi-pulpo: **stage → tier
mapping**. Each pipeline stage is locked to a model tier; the model
doesn't get to delegate its way around the mapping. This spec ports
the minimum viable version of that pattern to the hermes autonomous
worker as a three-stage shell-orchestrated tick: **PLAN (glm) → CODE
(qwen, iff code is needed) → ACT (glm)**.

## One-sentence invariant

Every 10-minute cron tick runs at most three isolated hermes
invocations — a glm-5.1 planner, an optional qwen3-coder implementer
(local only), and a glm-5.1 executor — each with its model locked by
the orchestrator script, and each artifact (plan, diff, act-log)
persisted to `~/chitin-sink/ticks/<date>/<ts>/` for observability and
debug.

## Scope

### In scope

- **Three-stage shell orchestrator** `scripts/hermes/tick.sh`,
  version-controlled in the chitin repo under `scripts/hermes/`.
- **Three markdown prompt files** (`prompt-plan.md`, `prompt-code.md`,
  `prompt-act.md`) co-located, version-controlled.
- **`plan-schema.json`** defining the contract between Stage 1 and
  Stages 2/3.
- **Model locks per stage:** Stage 1 and Stage 3 invoke
  `hermes chat --model glm-5.1:cloud`; Stage 2 invokes
  `hermes chat --model qwen3-coder:30b`. Hardcoded in tick.sh; not
  configurable via env at v1.
- **Local-ollama health probe** before Stage 2. Unreachable → skip
  Stage 2 and Stage 3; after 3 consecutive unreachable ticks,
  WhatsApp-surface via the existing daily-summary bridge.
- **Tick artifact directory** at
  `~/chitin-sink/ticks/<YYYY-MM-DD>/<UTC-ISO>/` capturing
  `queue.json`, `plan.json`, `diff.patch` (if any), `act-log.txt`,
  `tick.log`, stderr files, and `ollama-probe.txt`.
- **Cron replacement:** delete the existing `autonomous-worker` cron
  and `~/.hermes/scripts/autonomous-worker-orders.txt`; register a
  new cron pointing at `~/workspace/chitin/scripts/hermes/tick.sh`.
- **Four testing layers:** JSON Schema validation in CI, bats
  orchestration tests, manual `--dry-run` end-to-end on this box,
  live supervised canary on issue #10 (`.js extension for ESM
  import`).

### Out of scope

- **Review stage (QA).** Octi has one; we omit for v1 (YAGNI).
  Governance v1 handles destructive-action blocking; diff-vs-plan
  drift surfacing is a v2 concern if we observe it.
- **Triage as a separate stage.** Merged into PLAN. At ≤30 open
  issues we don't have scale to justify a dedicated triage cost.
- **Go-native dispatcher.** Octi's adapter pattern is implemented
  here as separate shell processes, not a Go interface type. Port the
  code if v2 needs richer routing (multi-repo, multi-agent).
- **In-tick retry / backoff.** Each tick is independent. If Stage 3
  fails transiently, next tick re-plans from the canonical queue.
- **PR merging.** Unchanged from v1: hermes opens PRs, user merges.
- **Multi-agent / distributed.** One machine, one cron. Same as v1.
- **Ollama fallback to glm for code.** Explicitly rejected: violates
  the "qwen for code only" architectural rule (see
  `feedback_hermes_model_split_semantics.md`).
- **Webhook / event-driven triggers.** Cron-only, same as v1.

## Architecture

```
           cron tick — every 10 minutes
                         │
                         ▼
         ~/workspace/chitin/scripts/hermes/tick.sh
                         │
                         ▼
        ┌────────────────────────────────────┐
        │  queue fetch (gh issue list +      │
        │    gh pr list)                     │
        │  write ~/chitin-sink/ticks/…/      │
        │    queue.json                      │
        └──────────────────┬─────────────────┘
                           ▼
        ┌────────────────────────────────────┐
        │  STAGE 1 — PLAN                    │
        │  hermes chat --model glm-5.1:cloud │
        │    --system prompt-plan.md         │
        │    --context queue.json            │
        │  → plan.json                       │
        │    action ∈ {skip, code, external} │
        └──────────────────┬─────────────────┘
                           │
              ┌────────────┴────────────┐
              │                         │
        action=code                action≠code
              │                         │
              ▼                         │
   ┌──────────────────────┐             │
   │ probe 127.0.0.1:11434│             │
   │   2s timeout         │             │
   └──────┬───────────────┘             │
          │ unreachable → exit 0        │
          │ ok                          │
          ▼                             │
   ┌──────────────────────────┐         │
   │ STAGE 2 — CODE           │         │
   │ hermes chat              │         │
   │   --model qwen3-coder:30b│         │
   │   --system prompt-code.md│         │
   │   --context plan + files │         │
   │ → diff.patch             │         │
   └──────┬───────────────────┘         │
          │                             │
          └──────────┬──────────────────┘
                     ▼
        ┌────────────────────────────────────┐
        │  STAGE 3 — ACT                     │
        │  hermes chat --model glm-5.1:cloud │
        │    --system prompt-act.md          │
        │    --context plan + diff           │
        │  tool calls: git apply / commit /  │
        │    push / gh pr create / gh issue  │
        │    comment / gh issue edit         │
        │  → governance v1 pre_tool_call     │
        │    hook fires on every call        │
        │  → act-log.txt                     │
        └──────────────────┬─────────────────┘
                           ▼
             tick.sh exits 0, cron updates Last run
```

### Invariants

1. **Stage 2 runs iff `plan.json.action == "code"`.**
2. **Stage 2 never calls glm.** Configured by hardcoded `--model
   qwen3-coder:30b`. Local ollama at `127.0.0.1:11434` is the only
   valid backend for this stage.
3. **Stage 1 and Stage 3 never call qwen.** No `delegate_task` is
   used anywhere in the tick — delegation is architectural (process
   separation), not in-session.
4. **Three hermes invocations are independent processes.** No shared
   memory, no session reuse. Artifacts on disk are the only channel.
5. **Governance v1's `pre_tool_call` hook fires on every tool call in
   Stage 3.** No code changes to governance. Block dicts are logged
   to `act-log.txt` and the existing
   `~/chitin-sink/gate-log-<date>.jsonl` and are expected outcomes,
   not errors.
6. **tick.sh always exits 0 except on shell crash.** Stage failures
   are data, not script errors.

## Components

Everything lives under `scripts/hermes/` in the chitin repo:

```
scripts/hermes/
├── tick.sh                  # cron entrypoint; orchestrates 3 stages
├── prompt-plan.md           # Stage 1 system prompt (glm-5.1)
├── prompt-code.md           # Stage 2 system prompt (qwen3-coder:30b)
├── prompt-act.md            # Stage 3 system prompt (glm-5.1)
├── plan-schema.json         # JSON schema for plan.json
└── README.md                # operator-facing docs
```

| File | Runs as | Purpose |
|------|---------|---------|
| `tick.sh` | bash under cron | Orchestrator. Fetches queue, runs stages, validates plan.json, handles ollama probe, writes artifacts. Hardcodes model per stage. |
| `prompt-plan.md` | glm-5.1:cloud | Judgment. Reads queue as context, emits a single `plan.json` object to stdout. No tool use. |
| `prompt-code.md` | qwen3-coder:30b local | Code generation. Reads plan + file contents, emits a single unified diff to stdout. No tool use — diff is inert text until Stage 3 applies it. |
| `prompt-act.md` | glm-5.1:cloud | Execution. Reads plan + diff, calls shell tools (`git apply`, `git commit`, `git push`, `gh pr create`, `gh issue comment`, `gh issue edit`). Governance fires on every call. |
| `plan-schema.json` | JSON Schema | Contract between Stage 1 and Stages 2/3. Validated by `jq` or `ajv` in tick.sh before dispatching to Stage 2/3. |
| `README.md` | docs | How to trigger a tick manually (`--dry-run`), where artifacts land, how to pause via cron. |

### plan.json schema (sketch — authoritative version in `plan-schema.json`)

```json
{
  "action":        "skip" | "code" | "external",
  "issue_number":  42,
  "reason":        "one-sentence why this action",
  "diff_request":  {
    "files":  ["path/to/file.go"],
    "intent": "free-text description for Stage 2"
  },
  "external_action": {
    "kind":          "comment" | "label" | "pr_open",
    "body_or_label": "string",
    "linked_issue":  42
  }
}
```

- `diff_request` required iff `action == "code"`.
- `external_action` required iff `action == "external"`.
- `action == "skip"` requires neither.

Rationale for three actions, not four: an earlier sketch included
`read` (investigate-then-re-plan-next-tick), but v1 is stateless
across ticks — there is no mechanism for tick N+1 to read tick N's
notes. `read` without that mechanism is just a `skip` with extra
words. If we later add cross-tick memory, `read` returns as a v2
action type.

### What is explicitly NOT a component

- No Go code. Shell + markdown + JSON schema. Reuses hermes CLI, `gh`,
  `git`, `jq`, `curl`.
- No new adapter interface. Octi's `Adapter` pattern is conceptually
  here (each stage IS an adapter) but implemented as separate
  processes, not an interface type.
- No daemon. Cron-triggered only, one-shot per tick.
- No SQLite / Redis state between ticks. The GitHub queue is
  canonical; each tick re-derives state.

## Data flow

### Tick artifact layout

```
~/chitin-sink/ticks/2026-04-22/20260422T103020Z/
├── env.txt              # captured cron env (CHITIN_KERNEL_PATH, HERMES_HOME, …)
├── queue.json           # gh issue list (labeled + unlabeled) + gh pr list
├── plan.json            # Stage 1 output (required)
├── plan-stderr.txt      # Stage 1 stderr
├── ollama-probe.txt     # "ok" | "unreachable" (one line)
├── diff.patch           # Stage 2 output — present iff action=="code"
├── code-stderr.txt      # Stage 2 stderr
├── act-log.txt          # Stage 3 stdout (tool calls + results)
├── act-stderr.txt       # Stage 3 stderr
└── tick.log             # tick.sh combined orchestration log
```

### Happy-path — implement tick

```
T+00:00  tick.sh fires; mkdir tick dir; write env.txt
T+00:00  gh issue list + gh pr list → queue.json
T+00:00  hermes chat glm-5.1 prompt-plan → plan.json {action: code, issue: 10, …}
T+00:05  jq validates plan.json against plan-schema.json
T+00:05  action=="code" → probe http://127.0.0.1:11434/api/tags → ok
T+00:05  git worktree add ~/workspace/chitin-10; symlink node_modules
T+00:06  hermes chat qwen3-coder prompt-code → diff.patch
T+00:28  hermes chat glm-5.1 prompt-act (tool-calling) → applies diff, commits, pushes, gh pr create → PR #47
T+00:32  tick.sh exit 0
```

### Grooming / label-apply tick

```
T+00:00  tick.sh fires
T+00:00  queue.json has unlabeled issues
T+00:00  glm-5.1 prompt-plan → plan.json {action: external, external_action: {kind: label, body_or_label: hermes-autonomous, linked_issue: 10}}
T+00:04  (skip Stage 2 — action ≠ code)
T+00:04  glm-5.1 prompt-act → gh issue edit #10 --add-label hermes-autonomous (governance allows — label is not destructive)
T+00:05  tick.sh exit 0
```

Grooming is now a first-class `action=external` output from Stage 1.
The grooming-stall we observed on 2026-04-22 came from qwen being
over-conservative as primary; glm as the planner is expected to
propose grooming when queue is empty.

### Skip tick

```
T+00:00  tick.sh fires
T+00:00  queue.json: no labeled issues, all unlabeled are too risky per plan-prompt criteria
T+00:00  glm-5.1 prompt-plan → plan.json {action: skip, reason: "no viable targets"}
T+00:03  (skip Stage 2 and Stage 3)
T+00:03  tick.sh exit 0
```

## Error handling

### Exit code philosophy

tick.sh **always exits 0** unless the shell itself crashes (unset
variable, failed pipeline under `set -euo pipefail`). Stage failures
are **data** — captured in the tick artifact directory — not script
errors. This preserves cron's health metric (only real crashes show
red) and matches the existing `autonomous-worker` behavior.

### Error categories

| Category | Detection | Captured in | Surfaced to user |
|----------|-----------|-------------|------------------|
| `plan_parse_error` | `jq` rejects Stage 1 stdout | `tick.log`, no `plan.json` | Daily summary counts parse errors |
| `plan_schema_violation` | Required field missing for action type | `tick.log`, `plan.json` flagged | Daily summary |
| `ollama_unreachable` | `curl` probe fails (2s timeout) | `ollama-probe.txt: unreachable` | **WhatsApp after 3 consecutive ticks** |
| `code_empty_output` | Stage 2 stdout empty or `git apply --check` rejects | `tick.log`, partial `diff.patch` | Daily summary |
| `governance_blocked` | Stage 3 tool call returns block dict | `act-log.txt` + `gate-log-<date>.jsonl` | Daily summary denial count |
| `stage3_tool_failure` | gh/git command non-zero exit, not governance | `act-stderr.txt` | Daily summary |
| `tick.sh crash` | bash pipefail trips | nothing (by definition) | Cron `Last run: failed` |

### 3-consecutive-unreachable surface

`~/chitin-sink/ollama-unreachable-streak.txt` holds a single integer.
tick.sh increments on unreachable, resets to 0 on any successful
probe. The existing daily-summary cron reads this file; if ≥3,
WhatsApp message: `"⚠ ollama unreachable for N ticks — 3090 likely
off; autonomous worker idle"`. No immediate per-tick WhatsApp to
avoid spam.

### Worktree hygiene

- tick.sh creates `~/workspace/chitin-<N>` only at Stage 3 apply
  time — not at plan time. A skip/read/external tick never creates a
  worktree.
- If Stage 3 fails partway, the worktree is left in place. Existing
  orphan-worktree sweep cron (7-day mtime) cleans it up. A
  half-applied worktree may contain useful debug state.
- Node_modules symlinked idempotently, as today.

### Idempotency across ticks

Stage 1's queue fetch always includes `gh pr list --search "is:open
linked:issue"`; Stage 1's prompt instructs: "if a PR already
references the chosen issue, pick a different issue or return
`action=skip`." No SQLite/Redis state — GitHub is canonical.

### Governance denials are expected, not errors

When Stage 3 hits a `pre_tool_call` block, the block dict is returned
as a hermes message. tick.sh treats this as a normal completion:
write to `act-log.txt`, exit 0. The existing
`gate-log-YYYY-MM-DD.jsonl` is also appended via the hermes plugin's
existing path — two independent records.

### Retry semantics

None. Each tick is independent. No per-issue retry counter, no
backoff. If Stage 3 failed transiently (network glitch), next tick
re-plans and re-tries naturally. If it fails systematically
(malformed diff), Stage 1 will likely re-propose the same thing — a
signal to surface to the user via daily summary, not to retry around.

## Testing

Four layers, scaled to what each actually catches.

### Layer 1 — Schema validation (automated, in CI)

- `plan-schema.json` validated by a small Go or Node test in CI.
- Fixture dir `scripts/hermes/test-fixtures/plans/` with ~10 samples
  (5 valid, 5 invalid); CI runs `jq` or `ajv` and asserts expected
  pass/fail.
- Catches: drift between `prompt-plan.md`'s output expectations and
  the schema.

### Layer 2 — tick.sh orchestration (bats, local-only)

- Stub `hermes`, `curl`, `gh`, `git`, `jq` with shell functions that
  write deterministic output.
- Assertions: stage sequencing (Stage 2 iff action=code, Stage 3 iff
  action∈{code,external}), artifact dir creation, exit codes, stderr
  capture, streak counter increments/resets.
- Run: `bats tests/hermes-tick.bats`.
- Catches: branching logic regressions, streak-counter bugs,
  premature worktree creation.

### Layer 3 — Dry-run end-to-end (manual, on this box)

- `./tick.sh --dry-run` flag: runs all three stages with real
  hermes/ollama/glm, but Stage 3's tool-calling prompt is replaced
  with "print the tool calls you would make, do not execute." No
  git/gh side effects.
- Produces a full tick artifact directory with a simulated
  `act-log.txt`.
- Run against a known issue (e.g. #10) before flipping the cron.
- Catches: prompt issues, schema-vs-prompt mismatches that only show
  up with real LLM output, ollama probe behavior on this machine.

### Layer 4 — Live canary (one tick, supervised)

- After Layers 1–3 pass, register the new cron and let one tick fire.
- Canary target: issue #10 (`.js extension for ESM import`) — small,
  clear scope, one-file change, the most boring-safe candidate.
- Confirm one full cycle produces sane `plan.json`, sane
  `diff.patch`, and either a correct PR or a clean governance denial.

### Explicitly NOT tested in this spec

- Hermes's plugin subsystem, governance v1's `pre_tool_call` hook,
  the chitin-sink event capture — all already covered by PR #37 + PR
  #45 tests.
- Load / concurrency. `cron.max_parallel_jobs: 1` means there's never
  more than one tick running.
- WhatsApp bridge delivery. Already live; the new message content is
  a one-line string.

### Success criteria for shipping

- All Layer 1 CI checks pass.
- All Layer 2 bats tests pass.
- Layer 3 dry-run produces a plan + diff a human would approve for
  issue #10.
- Layer 4 canary tick opens a real PR (or clean governance denial)
  that matches the dry-run output modulo LLM nondeterminism.

## Migration

1. Merge this spec + implementation plan.
2. Remove the existing `autonomous-worker` cron (look up id via
   `hermes cron list`, then `hermes cron rm <id>`).
3. Tombstone `~/.hermes/scripts/autonomous-worker-orders.txt` and
   `~/.hermes/scripts/autonomous-worker-context.py` to a dated backup
   dir (e.g. `~/.hermes/scripts/retired-<date>/`); do not delete
   outright.
4. Register new cron: `hermes cron create` with schedule `every 10m`
   pointing at `~/workspace/chitin/scripts/hermes/tick.sh`.
5. Canary per Layer 4.

## Open questions

None blocking implementation. Post-ship observations may inform v2
(review stage, fallback policy, multi-repo).

## References

- Post-mortem: `docs/observations/2026-04-22-autonomy-v1-post-mortem.md`
- Governance v1: PR #45 (merged `5cc74bbe8a`)
- Octi Pulpo stage→tier reference:
  `~/workspace/octi/internal/routing/modeltier.go`,
  `~/workspace/octi/internal/dispatch/cascade.go`,
  `~/workspace/octi/internal/dispatch/clawta_adapter.go`
- Durable feedback:
  `~/.claude/projects/-home-red-workspace-chitin/memory/feedback_hermes_model_split_semantics.md`
