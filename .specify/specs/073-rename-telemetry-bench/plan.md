# Implementation Plan: Collapse to Chitin Telemetry + Chitin Bench

**Branch**: `073-rename-telemetry-bench` | **Date**: 2026-05-20 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/073-rename-telemetry-bench/spec.md`

## Summary

A rename + collapse refactor — no redesign. Collapse Sentinel + Argus into
one **Chitin Telemetry** subsystem; rename **Icarus → Chitin Bench**. Done in
a safe order: repo file/module renames first (CI-gated), then the package
collapse, then the operational cutover (the running service + the live board)
last and carefully.

## Technical Context

**Languages**: Python (`python/argus`, `python/analysis`), bash (the
`icarus-bench-*` scripts), systemd (`icarus-bench.service`), the kanban board
(SQLite under `~/.hermes/kanban/boards/`).

**Primary Dependencies**: none new — this is `git mv` + reference rewrites.

**Testing**: `pytest` for the touched Python; `go vet`/`go build` where Go
references a renamed path; CI green at every step; a final grep gate.

**Constraints**: the `icarus-bench.service` is **running** (a bench loop may
be in-flight); the `icarus` kanban board is **live** with open tickets.
Neither may lose state.

**Project Type**: refactor across an existing repo + one systemd unit + one
kanban board.

**Scale/Scope**: ~7 scripts, 1 harness dir, 2 Python packages → 1, 1 service,
1 board, plus reference updates in specs/INDEX/skills/docs.

## Constitution Check

| Principle | Assessment |
|-----------|------------|
| §1 Side-effect boundary | PASS — a rename changes no side-effect routing. |
| §2 Workers + worktrees | PASS — executed in a worktree; the rename is committed via PR. |
| §3 Spec-kit gate | PASS — 073 has `spec.md` + this `plan.md`; `tasks.md` next. |
| §4 Tracked installers | PASS — the renamed `install-*.sh` stay tracked + idempotent. |
| §5 Board-aware scripts | PASS — the renamed bench scripts keep their `--board` flag. |
| §6 Swarm tooling | PASS — no new tooling; renames stay where they live. |

No violations → Complexity Tracking empty.

## Rename Map

| Old | New |
|-----|-----|
| `python/argus` + the Sentinel parts of `python/analysis` | `python/chitin_telemetry` (one package) |
| `/sentinel` skill | `/telemetry` skill |
| `swarm/icarus_harness/` | `swarm/chitin_bench/` |
| `swarm/bin/icarus-bench-runner` | `swarm/bin/chitin-bench-runner` |
| `swarm/bin/icarus-bench-loop` | `swarm/bin/chitin-bench-loop` |
| `swarm/bin/icarus-bench-ticket-emitter` | `swarm/bin/chitin-bench-ticket-emitter` |
| `swarm/bin/icarus-watcher` | `swarm/bin/chitin-bench-watcher` |
| `swarm/bin/install-icarus-bench-cron.sh` | `swarm/bin/install-chitin-bench-cron.sh` |
| `swarm/bin/install-icarus-cron.sh` | `swarm/bin/install-chitin-bench-watcher-cron.sh` |
| `swarm/bin/install-clawta-icarus-board-watcher.sh` | `swarm/bin/install-clawta-chitin-bench-board-watcher.sh` |
| `swarm/systemd/icarus-bench.service` | `swarm/systemd/chitin-bench.service` |
| `jobs/icarus/` | `jobs/chitin-bench/` |
| `icarus` kanban board | `chitin-bench` kanban board |
| `swarm.icarus_harness.agent:IcarusAgent` | `swarm.chitin_bench.agent:BenchAgent` |

## Migration Phases (safe order)

- **Phase 1 — Repo renames (CI-gated).** `git mv` the directories and
  scripts; rewrite every code import, `AGENT_IMPORT_PATH`, and reference;
  `go build` / `pytest` green at each step.
- **Phase 2 — Telemetry collapse.** Merge `python/argus` and the Sentinel
  detection/analysis from `python/analysis` into `python/chitin_telemetry`;
  one package, one `/telemetry` skill (was `/sentinel`).
- **Phase 3 — Operational cutover (last, careful).** Install
  `chitin-bench.service` beside `icarus-bench.service`, start it, then
  stop + disable the old (an in-flight bench run is cheap — the LRU picker
  re-runs it). Migrate the `icarus` board → `chitin-bench`, preserving every
  ticket, comment, and status.
- **Phase 4 — References.** `INDEX.md`, the `/sentinel`+`/evolve` skills,
  docs. **Grep gate:** no active `icarus`/`argus`/`sentinel` subsystem
  reference remains (only historical/superseded spec files).

## Project Structure

The Rename Map above is the structure change. New homes:
`python/chitin_telemetry/`, `swarm/chitin_bench/`, `swarm/bin/chitin-bench-*`.

## Complexity Tracking

None — no constitution violations.
