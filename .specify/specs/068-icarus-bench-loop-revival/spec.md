# 068 — Icarus bench loop revival (run terminal-bench non-stop)

> Operator directive 2026-05-20: "get the Icarus agent running the bench
> non-stop — have it run, then have Ares and Clawta decide what's next."
> PR #794 retired the v1 icarus-bench loop as "never meaningfully used";
> the v2 Harbor agent (specs 036/038) is spec'd but **not implemented**.
> This spec reverses #794's retirement to get bench runs flowing **now**,
> with the v2 IcarusHarborAgent as the eventual replacement.

## Ticket refs

- chitin — implementation ticket derived from `tasks.md` (created at handoff)

## File-system scope

- `swarm/bin/icarus-bench-runner` — LRU task picker + `harbor run` dispatcher
- `swarm/icarus_harness/{__init__,agent}.py` — the host-side Harbor agent
- `swarm/bin/icarus-bench-ticket-emitter` — bench results → kanban tickets
- `swarm/bin/icarus-bench-loop` — continuous back-to-back runner wrapper
- `swarm/systemd/icarus-bench.service` — systemd unit for the non-stop loop
- `swarm/bin/install-icarus-bench-cron.sh` — periodic-mode (2h) hermes cron
- `swarm/tests/test_icarus_harness.py` — harness regression tests
- `.specify/specs/068-icarus-bench-loop-revival/` (this spec)

## Goal

The Icarus terminal-bench loop runs continuously and unattended: every
cron tick picks the least-recently-tried cached Harbor task, runs the
Icarus agent against it, writes a result, and turns failures into kanban
tickets — so Ares and Clawta have a live stream of bench signal to act on.

## Background

PR #787 added the v1 Icarus harness; PR #794 deleted it (runner, agent,
emitter, cron installer, tests — 1584 lines) because the cron jobs were
paused and "never meaningfully used." Net effect today: exactly one
historical run (2026-05-19, scored 0.0 — verifier failed, but the agent
ran 20 steps to `TASK_COMPLETE`), and nothing scheduled. `harbor` is
installed, docker works, 20+ tasks are cached at `~/.cache/harbor/tasks/`,
and `ollama/qwen3.6:27b` is loaded — the substrate is intact; only
the loop machinery was removed. The v2 Harbor adapter (036/038) is the
intended long-term agent but is unimplemented, so reviving v1 is the
fastest path to live signal.

## Acceptance criteria

AC1. **Machinery restored.** The five files above are present and
`swarm.icarus_harness.agent:IcarusAgent` imports cleanly.

AC2. **A single run completes.** `icarus-bench-runner --n-tasks 1` picks
one cached task, dispatches `harbor run`, and produces a job directory
under `jobs/icarus/<job>/` with a `results.json`.

AC3. **Non-stop loop installed.** A systemd user service
(`icarus-bench.service`, `Restart=always`) runs `icarus-bench-runner`
back-to-back continuously; the runner's `flock` guards against any
overlapping manual run. (A 2h hermes cron is the periodic-mode fallback,
not the non-stop mechanism.)

AC4. **Failures become tickets.** The ticket emitter reads bench results
and creates kanban tickets for failed tasks (idempotently — no dupes).

AC5. **Handoff.** Once the loop is live, Ares and Clawta are handed the
"decide what's next" call via a kanban ticket on the chitin board — the
reliable channel; the agent-bus is unreliable (operator, 2026-05-20).

## Invariants

- **Single-flight.** At most one `icarus-bench-runner` process runs at a
  time; a re-entrant cron tick exits silently on the `flock`.
- **Deterministic task selection.** `pick_tasks` orders by
  `(last_tried_ts ASC, task_name ASC)` — a named final tie-breaker, so
  two ticks over the same cache pick the same next task.

## Out of scope

- Implementing the v2 `IcarusHarborAgent` (specs 036/038) — separate work.
- Changing the agent's reasoning loop or step budget. (Model is
  `ollama/qwen3.6:27b` — local, operator-chosen 2026-05-20: no cloud.)
- Improving task pass rates — this spec gets the loop *running*, not
  *winning*.

## Open questions

- Q1: Loop pause. `icarus-bench-loop` sleeps `ICARUS_LOOP_PAUSE` (60s
  default) between ticks so the model lease can yield to the lint-fix
  watcher; tune if ollama contention shows up.
- Q2: v1→v2 cutover. When 036/038 lands, swap `AGENT_IMPORT_PATH` — out
  of scope here, tracked by 038.
