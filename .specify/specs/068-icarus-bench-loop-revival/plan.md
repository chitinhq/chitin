# 068 — Implementation plan

## Approach

Revival, not redesign. PR #794's deletion is reverted verbatim from git
(`git checkout 389b486^ -- <paths>`); the restored runner is already
written for this `harbor` version (verified: `--path`, `--agent-import-path`,
`--model`, `--job-name`, `--jobs-dir`, `-k` all still valid flags). The
only new decisions are operational: confirm a run works end-to-end, then
schedule it.

## Run mechanism

`icarus-bench-runner` (host-side Python):

1. `discover_tasks()` — scan `~/.cache/harbor/tasks/<ulid>/<task>/` for
   dirs containing `task.toml`.
2. `pick_tasks()` — LRU pick, `(last_tried_ts, task_name)` ordering,
   state in `~/.icarus/bench-runner-state.json`.
3. `run_one_task()` — `harbor run --path <task> --agent-import-path
   swarm.icarus_harness.agent:IcarusAgent --model ollama/qwen3-coder:30b-32k
   --job-name icarus-<ts>-<task> --jobs-dir jobs/icarus -k 1`, with
   `PYTHONPATH` = repo root and a 1800s wall-clock cap.
4. After all picks, invoke `icarus-bench-ticket-emitter` (unless
   `--skip-emitter`).

The agent runs on the host and reaches the docker task only through
Harbor's `environment.exec`, so ollama stays uncontended.

## Non-stop loop

True non-stop is a daemon, not a cron. `swarm/bin/icarus-bench-loop`
runs `icarus-bench-runner` back-to-back forever (60s pause per tick);
`swarm/systemd/icarus-bench.service` (`Restart=always`) supervises it,
so a crash or clean exit is auto-revived. Single-flight stays `flock` on
`~/.icarus/bench-runner.lock` — a stray manual run can't stack with the
loop. The `~/.icarus/model-lease.lock` keeps the bench runner and the
lint-fix watcher from stacking ollama calls. The restored
`install-icarus-bench-cron.sh` (2h hermes cron) is retained as a
periodic-mode alternative, superseded for non-stop by the service.

## Dependencies (verified present)

- `harbor` CLI on PATH — yes (`/home/red/.local/bin/harbor`).
- docker daemon — yes (2 containers running).
- `ollama/qwen3-coder:30b-32k` — yes (`ollama list`).
- Cached tasks — yes (20+ under `~/.cache/harbor/tasks/`).

## Risks

- R1: `IcarusAgent` import fails (stale deps after the harness sat
  deleted). Mitigation: smoke run T002 surfaces it; fix under T003.
- R2: A harbor task with no cached docker image stalls the run.
  Mitigation: 1800s wall-clock cap returns `timeout`; the LRU picker
  rotates to the next task next tick.
- R3: ollama contention with the lint-fix watcher. Mitigation: the
  model-lease lock (existing) serializes them.

## Validation

- T002 smoke run produces `jobs/icarus/<job>/results.json`.
- `swarm/tests/test_icarus_harness.py` passes.
- After T004, `~/.icarus/bench-runner.log` shows ticks on the interval.
