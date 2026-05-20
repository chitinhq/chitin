# 067 — Decomposer derives kanban tickets from tasks.md

> Operator request 2026-05-20, after the spec-061 duplication. The gateway's
> LLM-freelance auto-decomposer was disabled (`kanban.auto_decompose: false`)
> because it produced non-canonical, un-deduped subtask tickets. Decomposition
> now belongs to the Spec-Kit `tasks` step — but nothing yet turns a spec's
> `tasks.md` into kanban tickets. This spec closes that gap.

## Ticket refs

- chitin `t_7017fcab` — implementation ticket for this spec

## File-system scope

- `hermes_cli/kanban_decompose.py` (hermes-agent repo) — add a deterministic
  tasks.md-derivation path, **or** a new `hermes_cli/kanban_taskstoissues.py`
- `hermes_cli/` CLI wiring — a `hermes kanban taskstoissues <spec-id>`
  subcommand (or an extension of `hermes kanban decompose`)
- `tests/hermes_cli/` — unit tests for the parser and idempotency
- `.specify/specs/067-tasks-to-tickets/spec.md` (this file)

## Goal

Turn a spec's canonical `tasks.md` into kanban tickets — deterministically and
idempotently — one ticket per task, linked to the spec and ordered by
dependency, so the swarm never hand-creates or LLM-freelances spec subtasks
again.

## Background

The Spec-Kit SDD workflow (`~/.hermes/ROLE.md` → "Spec Workflow") produces
`.specify/specs/NNN-<slug>/tasks.md` as the canonical task list. The gateway
LLM auto-decomposer is now off (`kanban.auto_decompose: false`). The missing
link: nothing converts `tasks.md` → kanban tickets. Today an agent must
hand-create them, which is error-prone and re-introduces the duplication risk
(`t_6756b648` vs PR #809).

## Acceptance criteria

AC1. **tasks.md parser.** Given `.specify/specs/NNN-<slug>/tasks.md` in the
spec-kit tasks format (checkbox list, `T0NN`-style task ids, dependency
markers), the tool parses every task into `{task_id, title, body, depends_on}`.

AC2. **Idempotent ticket creation.** One kanban ticket per task. Re-running
the tool creates **zero** duplicates — tasks already turned into tickets are
detected via a stable `task_id ↔ ticket` link and skipped.

AC3. **Linkage.** Each created ticket is linked (kanban parent/child) to the
spec's root ticket, and its body carries the spec id + `task_id` so dedup
checks (and a future poller dedup pre-check) can find it.

AC4. **Dependency order.** A task whose dependencies are unmet yields a
`blocked` ticket (or one carrying a dependency ref the poller honors); a task
that is ready yields a `ready` ticket.

AC5. **CLI surface.** `hermes kanban taskstoissues <spec-id>` (or an extension
of `hermes kanban decompose`) runs it. `--dry-run` prints the plan and mutates
nothing.

AC6. **No LLM freelancing.** The path is fully deterministic — it reads
`tasks.md`; it never calls an LLM to invent tasks. The LLM auto-decomposer
stays disabled.

## Invariants

- Every spec subtask ticket traces to exactly one task in exactly one
  `tasks.md`. No subtask ticket exists without a backing task; re-running the
  tool never duplicates a ticket.

## Out of scope

- Re-enabling the gateway LLM auto-decomposer.
- Decomposition of non-spec / general tickets — agents groom those via the
  pull loop, not this tool.

## Open questions

- Q1: Extend `hermes kanban decompose` with a tasks.md branch, or ship a
  separate `taskstoissues` command? (Separate command keeps `decompose`'s
  LLM path cleanly retired.)
- Q2: How is the spec ↔ root-ticket mapping established — the `tasks` step
  writes the root ticket id into `tasks.md`, or a `NNN-<slug>` ↔ ticket
  naming/linking convention?
