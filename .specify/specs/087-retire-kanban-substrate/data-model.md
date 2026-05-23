# Data Model: The Retiring Kanban Substrate

**Feature**: 087-retire-kanban-substrate
**Date**: 2026-05-22

This is not a new-feature data model — it is the decomposition of the substrate being
retired, so the implementation phase has a single accounting of "what is the kanban
substrate, formally, and what state does each piece carry?"

## The substrate, decomposed

The kanban substrate is the union of **three concerns** that historically were entangled
under one name. Naming them apart makes the retirement tractable.

### 1. The persistence — SQLite databases (PRESERVED, not deleted)

The on-disk state lives in two locations:

- `~/.chitin/kanban/<board>/kanban.db`
- `~/.hermes/kanban/boards/<board>/kanban.db`

**Owner**: the operator's machine.
**Schema**: `go/execution-kernel/internal/kanban/schema.go` defined the columns: tickets
(id, title, lane, priority, severity, claimed_by, ts), state lanes (triage, ready,
in_progress, done), board metadata, audit log.
**Retirement effect**: the files stay. No code reads or writes them after the cull.
Migration runner (`migrate.go`) is gone, so the schema is frozen at whatever version the
operator's DB is on. Operators decide when (or whether) to delete the files. **FR-008 /
SC-005 enforce non-deletion**.

### 2. The exposure — code that reads/writes the persistence (DELETED)

This is the code surface that the retirement removes:

#### 2a. Kernel-side persistence layer
- `go/execution-kernel/internal/kanban/schema.go` — schema bootstrap.
- `go/execution-kernel/internal/kanban/migrate.go` — schema migration runner.
- `go/execution-kernel/internal/boardconfig/boardconfig.go` — board-name → DB-path resolver.

#### 2b. Kernel CLI subcommands
- `chitin-kernel kanban` — wrapped the persistence (init / migrate / show / inspect operations).
- `chitin-kernel board-config <slug>` — wrapped boardconfig.

After the cull, both invocations return "unknown subcommand". No autonomous fallback.

#### 2c. MCP exposure to agents
- `services/swarm-kanban-mcp/server.py` — exposed kanban DB ops as MCP tools (`mcp__swarm_kanban__claim_ticket` etc.) for agents (clawta, ares) running with MCP access. After the cull, attempting to register the server fails (binary not found); agents that listed it as a tool config have a stale tool descriptor (operator-side cleanup).

#### 2d. swarm/-side polling and dispatch
- `swarm/workflows/kanban-dispatch.lobster` — the lobster workflow that polled the kanban DB for ready tickets and spawned workers. Deleted in full.
- `swarm/bin/board_resolver.py` — multi-board resolution helper. Deleted.
- `swarm/bin/board-watchdog-bounded.py` — DB-health watchdog. Deleted.
- `swarm/bin/clawta-swarm-board-watcher` — board-watcher (the script the cron line invoked). Deleted.
- Plus partial edits to: swarm-controller, clawta-pr-reviewer, clawta-blocked-escalator, swarm-audit, hermes-clawta-bridge, _pick_driver, spawn_worker_subprocess.

#### 2e. Console-API exposure
- HTTP routes in `apps/chitin-console-api/src/server.mjs` that opened the kanban DB and returned ticket / queue / report state to the UI. Stripped from the route table.

#### 2f. Console-UI rendering
- `apps/chitin-console/src/app/pages/{queue,tickets,reports}.page.{ts,html}` — pages dedicated to kanban state. Deleted.
- `api.service.ts`, `overview.page.html`, `sdlc-diagram.page.ts`, `index.html`, `README.md` — partial-edited.
- Nav-bar / app-router de-registration — partial edit.

### 3. The semantics — what "kanban" *meant* in the platform

This is the *concept* the substrate represented: a board-shaped, lane-based, ticket-as-row
view of work-in-progress that agents and operators read to decide what to dispatch and
what to display. Retiring the substrate retires the concept from active platform
vocabulary. The Temporal orchestrator (spec 070) carries the dispatch semantics now
(workflows, child workflows, signals). The console-UI carries the visibility semantics
through sessions / orchestrator-diagram pages.

**The semantics are not "lost"** — they migrate to the new substrate. A workflow
execution is a "ticket" in the new vocabulary; a queued child workflow is a "ready
ticket"; the operator's "what's running" is satisfied by the sessions page. The
retirement is a vocabulary migration as much as a code deletion.

## State transitions (what happens at each commit)

The retirement lands as multiple worktree partitions (Phase 2 / `/speckit-tasks`). Each
partition leaves the platform in a known-consistent state:

| After partition | Kanban DBs (operator) | Kanban code (in tree) | Platform dispatch | Console visibility |
|---|---|---|---|---|
| (baseline — pre-merge) | exist, may be active | present, unused by live flow | Temporal orchestrator | sessions + kanban pages |
| 1 — MCP server gone | unchanged | minus services/swarm-kanban-mcp | unchanged (orchestrator dispatch independent) | unchanged (UI still has kanban pages, just nothing exposes the data via MCP) |
| 2 — kernel pkg + CLI gone | unchanged | minus internal/kanban, internal/boardconfig, kernel CLI subcommands | unchanged | `kanban` subcmd unknown; existing CLI consumers fail cleanly |
| 3 — console-API routes gone | unchanged | minus kanban routes in server.mjs | unchanged | UI pages get HTTP errors on kanban-route calls |
| 4 — console-UI pages gone | unchanged | minus chitin-console kanban pages | unchanged | sessions + orchestrator pages only |
| 5 — swarm scripts + installers + tests gone | unchanged | minus board_resolver, board-watchdog, clawta-swarm-board-watcher, kanban-dispatch.lobster, paired installers, paired tests | unchanged | unchanged |
| 6 — docs updated | unchanged | docs reflect post-retirement surface | unchanged | unchanged |
| 7 — CI gate added | unchanged | scripts/check-no-kanban.sh enforces no future drift | unchanged | unchanged |

**Each row** is a green-build state. The platform is in a deliverable state after each
partition. This satisfies constitution §2 (each partition is a deliverable worker dispatch).

## Entity-relationship notes (for the implementer)

- The kernel persistence layer (2a) is **internally** consumed only by 2b (CLI) and 2c
  (MCP). No other in-tree code imports `internal/kanban` or `internal/boardconfig`.
  Verifying this with `go list -deps` is a Phase 0.5 check the implementation can re-run
  before partition 2 commits.

- The swarm-side polling (2d) reads the persistence (1) **through** the boardconfig
  resolver, not through the kernel CLI. So 2d can retire BEFORE 2a/2b without leaving
  consumers of the kernel CLI orphaned (there are no remaining consumers; clawta-poller
  was the last, retired by #908).

- The console-API (2e) is the ONLY in-tree HTTP-layer consumer of the persistence.
  Retiring its routes before 2a is fine; retiring 2a before 2e leaves the routes
  dangling (their handlers reference the retired schema). **Order matters: 2e BEFORE 2a**,
  OR the same partition.

- The console-UI (2f) consumes the console-API. Retiring 2f BEFORE 2e leaves the API
  routes orphaned (no callers); retiring 2e BEFORE 2f leaves UI calls hitting 404s.
  **Order matters: 2f BEFORE 2e** if minimizing transient state, OR the same partition.

**Recommended partition order** (resolves the orderings above):

1. MCP server (2c) — independent, retire first.
2. Console-UI pages (2f) — orphan no API yet.
3. Console-API routes (2e) — no callers remain.
4. Kernel pkg + CLI (2a + 2b) — no consumers remain.
5. swarm scripts (2d) — independent of 1–4; can also go earlier in parallel.
6. Docs (3, vocabulary migration) — last, references the post-retirement surface.
7. CI gate — last, locks in the SC-001 invariant.

`/speckit-tasks` lays out the task graph per this ordering.
