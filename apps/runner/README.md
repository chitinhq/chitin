# `apps/runner`

Chitin's autonomous swarm runtime — Temporal worker + dispatcher +
the suite of cron-fired scripts that close the §3 station-taxonomy
loop from `docs/design/2026-05-02-swarm-as-software-factory.md`.

## What's here

### Long-running

| File | Role |
|------|------|
| `src/worker.ts` | Temporal worker. Polls `chitin-worker-q`, runs activities (`runAgentTurn`, `runGatekeeperNotify`). Loaded by `chitin-worker.service`. |
| `src/workflow.ts` | Workflow registry — re-exports `executeRequestWorkflow` + `reviewGraphWorkflow` so the worker's webpack bundle picks them up. |
| `src/activity.ts` | The activity that spawns an agent in a worktree, runs it, captures the result envelope. |
| `src/review-graph-workflow.ts` | The §5 review-tier escalation chain. Dispatches reviewers R1→R2→R3, calls the gatekeeper notify activity at the end. |

### Cron-fired scripts (one per timer; pattern: deterministic, idempotent, telemetry-emitting, no Temporal)

| Script | Timer | Job |
|--------|-------|-----|
| `src/dispatcher.ts` | `chitin-dispatcher.timer` (every 5 min) | Read backlog, pick next ready entry, submit one programmer workflow, run apply step, enqueue review-graph. |
| `src/researcher.ts` | `chitin-researcher.timer` (every 4h) | Pull external signals (arxiv / Reddit / HN / openclaw / ollama), dedup against `docs/roadmap.md`, append candidates. |
| `src/lessons.ts` | `chitin-lessons.timer` (daily) | Scan merged swarm/* PRs, distill one-sentence lessons, append to `docs/swarm-lessons.md`. The dispatcher prepends recent lessons to programmer prompts. Heuristic v1 + LLM-backed v2 (off by default; flip via `CHITIN_LESSONS_USE_LLM=1`). |
| `src/debt-curator.ts` | `chitin-debt-curator.timer` (daily) | Scan repo for TODO/FIXME/HACK/XXX markers, dedup, file `docs/debt-ledger.md` entries at severity:'low'. |
| `src/groomer.ts` | `chitin-groomer.timer` (daily) | Read roadmap candidates, draft up to N (default 1) `in_design` backlog entries from arxiv-source candidates. The existing `groom-pass.ts` (Copilot-driven) takes them to `ready`. |
| `src/alarm-feeder.ts` | `chitin-alarm-feeder.timer` (daily) | Read latest rollup `alarms[]`, dedup, draft `in_design` entries with `role: analyst`. Closes §7 telemetry → backlog flywheel. |
| `src/stale-doc-detector.ts` | `chitin-stale-doc-detector.timer` (daily) | Scan `docs/**/*.md` for project-relative path refs that no longer exist; file ledger entries. Tech-writer's debt-detection half. |
| `src/groom-pass.ts` | (manual / on-demand) | Copilot-driven grooming pass — promotes `in_design` → `ready` by classifying tier / file scope / estimated_loc. |

### Role registry + prompts

| File | Purpose |
|------|---------|
| `src/role-prompts.ts` | Role → prompt-builder registry. Routes `BacklogEntry.role` (programmer / researcher / reviewer / analyst / …) to the right template. |
| `src/researcher-prompts.ts` | Researcher prompt + `<<<CANDIDATES>>>`-marked structured emit + parser. |
| `src/reviewer-prompts.ts` | Adversarial reviewer prompt template (R1/R2/R3 tones) + `<<<REVIEW>>>`-marked emit + parser. |
| `src/gatekeeper.ts` | Slack digest + §6 auto-merge gates (CI green, bucket-B rate, 🔴 findings, T5-path, scope-intersect, driver-success-rate). Opt-in via `CHITIN_GATEKEEPER_AUTO_MERGE=1`. |

### Dispatcher integration

| File | Purpose |
|------|---------|
| `src/review-graph-dispatch.ts` | After PR opens, the dispatcher calls `enqueueReviewGraph` to fire `reviewGraphWorkflow` as a separate top-level workflow. Fire-and-forget — the next dispatcher tick is free to pick a new entry. |
| `src/grooming/parse-backlog.ts` | Parser for `docs/swarm-backlog.md`. |
| `src/grooming/apply-workflow-result.ts` | Apply step: push branch, open PR, revert known artifacts (e.g., `.claude/settings.json` overwrite). |

## Pipeline at a glance

```
chitin-researcher.timer (4h) → docs/roadmap.md candidates
                                ↓
chitin-groomer.timer (24h) → docs/swarm-backlog.md in_design entries (arxiv source-allowlist)
                                ↓
groom-pass.ts (Copilot-driven, on-demand) → ready entries
                                ↓
chitin-dispatcher.timer (5m) → programmer workflow → activity → PR
                                ↓
                                + enqueue reviewGraphWorkflow
                                ↓
review-graph (R1→R2→R3 escalation + parse-failure handling) → ReviewGraphResult
                                ↓
gatekeeper notify activity:
  ├─ all 6 §6 gates pass + CHITIN_GATEKEEPER_AUTO_MERGE=1 → gh pr merge
  └─ otherwise → Slack digest, operator decides

chitin-swarm-rollup.timer (24h) → ~/.cache/chitin/swarm-rollups/<date>.json
                                ↓
chitin-alarm-feeder.timer (24h) → role:analyst investigate-* in_design entries
                                ↓
analyst workflow → python -m analysis.investigate (deterministic recipe) → markdown report

chitin-lessons.timer (24h)        → docs/swarm-lessons.md → prepended to programmer prompts
chitin-debt-curator.timer (24h)   → docs/debt-ledger.md  (TODO/FIXME scan)
chitin-stale-doc-detector.timer (24h) → docs/debt-ledger.md (broken doc refs)
```

## Flags + env

| Variable | Effect |
|----------|--------|
| `CHITIN_REPO_ROOT` | Repo root (defaults to cwd). Used by every script. |
| `CHITIN_SLACK_WEBHOOK_URL` | Slack webhook for dispatcher events + gatekeeper digests. Empty = no-op. |
| `CHITIN_LESSONS_USE_LLM` | `1` = lessons distilled via `claude -p haiku-4-5` (reflective). Off = heuristic title-strip. |
| `CHITIN_GATEKEEPER_AUTO_MERGE` | `1` = gatekeeper actually `gh pr merge`s on green. Off = notify-only. |
| `CHITIN_RESEARCHER_CAP` | Max candidates per researcher tick (default 5). |
| `CHITIN_GROOMER_CAP` / `CHITIN_GROOMER_SOURCES` | Groomer cap (default 1) / source allowlist (default `arxiv`). |
| `CHITIN_DEBT_CURATOR_CAP` | Default 20. |
| `CHITIN_LESSONS_SCAN_LIMIT` | Default 30 PRs back. |
| `CHITIN_ALARM_FEEDER_CAP` | Default 1. |
| `CHITIN_STALE_DOC_CAP` | Default 10. |

## Test suite

```bash
pnpm exec vitest run apps/runner
```

500+ tests. Each cron script has its own `*.test.ts` covering pure
logic + the runner's happy / dedup / cap / no-op / error-containment
paths.

## Systemd units

See `infra/systemd/README.md` for unit installation, env-file setup,
and operate / pause / hard-stop runbooks.
