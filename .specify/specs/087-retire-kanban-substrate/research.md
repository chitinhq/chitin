# Phase 0 Research: Retire the Kanban Substrate

**Feature**: 087-retire-kanban-substrate
**Date**: 2026-05-22
**Status**: Surface map complete; risk flags documented

This document records the file-level surface of the kanban substrate (what the retirement
deletes) and the design decisions that resolve the spec's ambiguities. It exists so the
implementation phase reads from a single source of truth instead of re-discovering the
surface in each task context.

## Decision summary (resolves all spec ambiguities)

| Decision | Choice | Why | Alternatives considered |
|---|---|---|---|
| D1 — Orchestrator references | Comment-only mentions of "kanban" in `go/orchestrator/workflows/{sequence.go,scheduler.go}` STAY as historical context | They explain what the orchestrator REPLACED. They are not imports. FR-006 reads "swarm-side scripts MUST be removed", which the orchestrator is not. | (a) Strip the comments — rejected: loses useful "this replaced X" breadcrumb; (b) Mass-rename "kanban" to "legacy-board" — rejected: gratuitous churn. |
| D2 — swarm-controller fate | RETAINED for now, with a follow-up ticket to evaluate post-merge | swarm-controller is the post-#908 dispatcher. Its 26 kanban hits include both `KANBAN_BOARDS_DIR` config (live read?) and historical comments. Whether it actively depends on kanban-as-runtime needs per-line audit in implementation. Retiring it in 087 risks stranding any operator who has it deployed. | (a) Retire alongside kanban — rejected: scope creep; spec 087 says "retire kanban", not "retire the post-kanban dispatcher"; (b) Force a swarm-controller spec — defer; surface as follow-up ticket if its kanban dependency is real. |
| D3 — hermes-clawta-bridge fate | RETAINED with kanban references stripped (partial edit) | The bridge is the entry point for hermes→clawta dispatch. Spec 081 retired the *board read-model*, but the bridge itself is dispatch-glue. Kanban references in it are vestigial config-resolution. | (a) Retire the whole bridge — rejected: dispatch glue, not kanban substrate; (b) Leave references — rejected: violates FR-011 grep gate. |
| D4 — Console-UI pages | `queue.page.ts`, `tickets.page.ts`, `reports.page.*` are whole-file deletes; `sdlc-diagram.page.ts`, `overview.page.html`, `api.service.ts`, `index.html`, `README.md` are partial edits | Pages dedicated to kanban die wholesale; shared assets (api.service, overview, nav, README) get their kanban-specific sections stripped. | Whole-app delete — rejected: chitin-console serves non-kanban surfaces (orchestrator, sessions, sentinel). |
| D5 — Operator docs (`docs/runbooks/*`) | Each runbook gets a per-file decision in implementation: partial-edit if it has non-kanban content, whole-delete if it is kanban-only | The 13 runbooks with kanban hits range from "incidental mention" to "kanban-only workflow". Pre-deciding here would force premature classification. | Bulk delete all — rejected: too aggressive; loses non-kanban runbook content. |
| D6 — Partition by category, not by language | Phase 2 tasks partition by surface (MCP / kernel / console-API / console-UI / swarm / docs), each a separate worktree | Each surface has a different test loop. A polyglot-mega-PR would be impossible to verify incrementally. | Single mega-PR — rejected: defeats constitution §2's worktree discipline. |
| D7 — Test deletion together with code | `*_test.go` / `*.test.ts` / `test_*.py` files for retired code retire in the *same* worktree partition as the code they exercise | FR-007 requires no orphaned tests; pairing the test delete with the code delete keeps each partition's build green at every commit. | Tests in a final cleanup partition — rejected: leaves intermediate states with broken tests. |
| D8 — FR-011 grep gate enforcement | A `scripts/check-no-kanban.sh` (added in the final partition) runs `grep -rli 'kanban\|hermes.*board' apps/ go/ libs/ services/ swarm/` and exits non-zero on any hit. Wired into CI as a required check. | Without a CI gate, future drift re-introduces kanban references silently. SC-001 needs an enforcement, not just a one-time check. | Manual check on merge — rejected: doesn't survive a single feature PR. |

## Surface map (file-level)

The numbers in parentheses are kanban-mention counts from `grep -c`; they help size the
partial-edit difficulty but are not load-bearing.

### A. MCP server (FR-002) — `services/swarm-kanban-mcp/`

**Whole-directory delete** (5 source files):

| File | Action | Notes |
|---|---|---|
| `services/swarm-kanban-mcp/server.py` | delete-whole | The MCP server itself. |
| `services/swarm-kanban-mcp/project.json` | delete-whole | Nx project graph entry. |
| `services/swarm-kanban-mcp/README.md` | delete-whole | — |
| `services/swarm-kanban-mcp/tests/test_server.py` | delete-whole | FR-007 — paired test. |
| `services/swarm-kanban-mcp/tests/__init__.py` | delete-whole | — |

**Project graph cleanup**: removing `services/swarm-kanban-mcp/project.json` automatically
drops the project from `nx.json`'s resolved graph. Verify with `pnpm nx graph` post-delete.

### B. Kernel packages (FR-003) — `go/execution-kernel/`

**Whole-directory delete**:

| File | Action | Notes |
|---|---|---|
| `go/execution-kernel/internal/kanban/schema.go` | delete-whole | Kanban DB schema. |
| `go/execution-kernel/internal/kanban/schema_test.go` | delete-whole | — |
| `go/execution-kernel/internal/kanban/migrate.go` | delete-whole | Migration runner. |
| `go/execution-kernel/internal/kanban/migrate_test.go` | delete-whole | — |
| `go/execution-kernel/internal/boardconfig/boardconfig.go` | delete-whole | Board-config resolver. |
| `go/execution-kernel/internal/boardconfig/boardconfig_test.go` | delete-whole | — |

### B-CLI. Kernel CLI subcommands (FR-003) — `go/execution-kernel/cmd/chitin-kernel/`

**Mixed**: whole-file deletes for the subcommand handlers, partial edit on `main.go`.

| File | Action | Notes |
|---|---|---|
| `go/execution-kernel/cmd/chitin-kernel/kanban_cmd.go` | delete-whole | The `kanban` subcommand handler. |
| `go/execution-kernel/cmd/chitin-kernel/kanban_cmd_test.go` | delete-whole | — |
| `go/execution-kernel/cmd/chitin-kernel/board_config.go` | delete-whole | The `board-config` subcommand handler. |
| `go/execution-kernel/cmd/chitin-kernel/board_config_test.go` | delete-whole | — |
| `go/execution-kernel/cmd/chitin-kernel/main.go` | **partial edit, lines 61-63** | Strip `case "board-config":` and `case "kanban":` switch cases. |
| `go/execution-kernel/cmd/chitin-kernel/gate_hook.go:322` | comment-only edit | Update or strip the comment referencing "out-of-process kanban-dispatch workflow". |
| `go/execution-kernel/cmd/chitin-kernel/router_hook.go:198` | comment-only edit | Update or strip the comment referencing "kanban-dispatched profile". |
| `go/execution-kernel/cmd/chitin-kernel/report.go:77-78` | comment-only edit | Update the §5-citing comment; `--board` flag context retires with kanban. |

### C. Console-API (FR-004) — `apps/chitin-console-api/`

| File | Action | Notes |
|---|---|---|
| `apps/chitin-console-api/src/server.mjs` | **partial edit** | Strip kanban-related routes; keep argus, ELO, gov-decisions, sessions, orchestrator routes. Implementation phase identifies the exact route blocks. |

The original spec assumed a richer console-API kanban surface; the actual surface is one
file with a few route handlers. This is a substantially smaller scope than implied.

### D. Console UI (FR-005) — `apps/chitin-console/`

| File | Action | Notes |
|---|---|---|
| `apps/chitin-console/src/app/pages/queue.page.ts` | delete-whole | Kanban queue page. |
| `apps/chitin-console/src/app/pages/tickets.page.ts` | delete-whole | Kanban tickets page. |
| `apps/chitin-console/src/app/pages/reports.page.ts` | delete-whole if kanban-only, partial-edit if mixed | Verify in implementation. |
| `apps/chitin-console/src/app/pages/reports.page.html` | matches `reports.page.ts` decision | — |
| `apps/chitin-console/src/app/pages/sdlc-diagram.page.ts` | **partial edit** | Strip kanban-node from the SDLC diagram. |
| `apps/chitin-console/src/app/pages/overview.page.html` | **partial edit** | Strip kanban-overview section. |
| `apps/chitin-console/src/app/api.service.ts` | **partial edit** | Strip `getKanban*` / `getTickets*` methods. |
| `apps/chitin-console/src/index.html` | **partial edit** | Strip kanban nav link if present. |
| `apps/chitin-console/README.md` | **partial edit** | Strip kanban-feature mentions. |

App router (likely `app.routes.ts` or similar — verify in implementation): de-register
the deleted page routes.

### E. swarm/ — scripts, workflows, installers, tests (FR-006 + FR-007)

**E.1 swarm/bin scripts**:

| File | Action | Notes |
|---|---|---|
| `swarm/bin/board_resolver.py` | delete-whole | 11 hits — "shared board resolution for multi-board kanban". Kanban-only. |
| `swarm/bin/board-watchdog-bounded.py` | delete-whole | 10 hits — reads `~/.hermes/kanban/boards/`. Kanban-only. |
| `swarm/bin/clawta-swarm-board-watcher` | delete-whole | 3 hits — "watches the swarm kanban board". Kanban-only. |
| `swarm/bin/swarm-controller` | **RISK FLAG — D2** | 26 hits including `KANBAN_BOARDS_DIR`. Implementation phase decides: strip kanban deps (partial-edit) or escalate to follow-up spec. |
| `swarm/bin/clawta-pr-reviewer` | **partial edit** | 2 hits — calls `hermes kanban` CLI. Replace with a non-kanban ticket-lookup mechanism or strip the call if redundant. |
| `swarm/bin/clawta-blocked-escalator` | **partial edit** | 2 hits — same shape as clawta-pr-reviewer. |
| `swarm/bin/swarm-audit` | **partial edit** | 7 hits — audits kanban DB state. Strip kanban-audit; keep non-kanban audit logic. |
| `swarm/bin/chitin-bench-loop` | **comment-only edit** | 1 hit — comment about hermes env. |

**E.2 swarm/workflows**:

| File | Action | Notes |
|---|---|---|
| `swarm/workflows/kanban-dispatch.lobster` | delete-whole | 74 hits — the kanban dispatch workflow. The primary kanban surface in `swarm/`. |
| `swarm/workflows/hermes-clawta-bridge.py` | **partial edit — D3** | 13 hits — dispatch glue; strip kanban-specific resolution but keep the bridge. |
| `swarm/workflows/_pick_driver.py` | **partial edit** | 5 hits — driver picking logic, kanban-aware; strip kanban context, keep driver picking. |
| `swarm/workflows/spawn_worker_subprocess.py` | **partial edit** | 2 hits — minor kanban context. |

**E.3 swarm/bin installers** (constitution §4 — paired with their scripts):

| File | Action | Notes |
|---|---|---|
| `swarm/bin/install-swarm-workflow.sh` | **partial edit** | Strip the `kanban-dispatch.lobster` symlink line (line 27). Other workflow links may stay. |
| `swarm/bin/install-board-watchdog-bounded.sh` | delete-whole | Installs `board-watchdog-bounded.py`, which retires. |
| `swarm/bin/install-board-watchdog-prompt.sh` | delete-whole | Paired with board-watchdog. |
| `swarm/bin/install-clawta-swarm-board-watcher.sh` | delete-whole | Installs `clawta-swarm-board-watcher`. |
| `swarm/bin/install-clawta-chitin-bench-board-watcher.sh` | delete-whole | Board-aware bench watcher. |

Other installers (`install-chitin-console.sh`, `install-chitin-orchestrator.sh`,
`install-hermes-clawta-bridge.sh`, `install-clawta-mention-listener.sh` [spec 088],
`install-swarm-audit.sh`, `install-clawta-pr-lifecycle.sh`, etc.) are not paired with
retired scripts and stay.

**E.4 swarm/tests**:

| File | Action | Notes |
|---|---|---|
| `swarm/tests/test_check_swarm_kanban_isolation.py` | delete-whole | Tests kanban-isolation — retires with kanban. |
| `swarm/tests/test_clawta_swarm_board_watcher.py` | delete-whole | Paired with the script. |
| `swarm/tests/test_dispatch_atomicity_invariant.py` | **partial edit or delete** | 10 hits — implementation decides: if invariant is kanban-specific, delete; if reusable for orchestrator dispatch, port. |
| Others in `swarm/tests/test_chitin_bench.py` etc. | **inspect** | The bench test file accumulates classes from many tickets; check for kanban references during implementation. |

### F. libs/ (TS) — `libs/contracts/`

| File | Action | Notes |
|---|---|---|
| `libs/contracts/src/execution-request.schema.ts:106` | **comment-only edit** | Comment says "Hermes Agent (kanban dispatcher)". Update to "Hermes Agent (dispatcher)" or strip. |

### G. (already covered above per-category)

### H. Build / project graph

| File | Action | Notes |
|---|---|---|
| `services/swarm-kanban-mcp/project.json` | delete-whole | Drops swarm-kanban-mcp from Nx graph automatically. |
| `nx.json` | no edit needed | Root Nx config doesn't list services explicitly; project graph derives from project.json files. |
| `pnpm-workspace.yaml` | no edit needed | Doesn't list services individually. |
| `go.work`, `go.mod` | no edit needed | Kernel `internal/kanban` and `internal/boardconfig` are internal-only; no module path exposure. |

### I. Active operator docs (FR-010) — `README.md`, `AGENTS.md`, `docs/runbooks/*`

Per-runbook decisions land in implementation. The runbooks that mention kanban:

- `AGENTS.md`, `README.md` (repo root) — partial-edit.
- `docs/runbooks/dispatch-pipeline.md` — partial-edit (mentions kanban-dispatch).
- `docs/runbooks/spec-lifecycle.md` — partial-edit if kanban mentioned in passing.
- `docs/runbooks/swarm-sdlc-*` — per-doc verdict; some are kanban-era workflow docs.
- `docs/runbooks/regression-gate.md`, `unknown-rate-alarm.md`, `usage-feeds.md`,
  `swarm-runtime-guards.md`, `worktree-conventions.md`, `chitin-router.md`,
  `governance-self-mod-defense.md`, `governance-signed-policy.md`, `health.md`,
  `plugin-sandbox.md`, `safe-temp-work.md`, `scripts-classification.md`,
  `swarm-sdlc-status-machine.md`, `swarm-sdlc-mock-worker-dogfood.md`,
  `swarm-sdlc-hermes-grooming-prompt.md` — partial-edit; per-doc verdict.

Implementation phase decides for each: partial-edit (kanban references stripped) vs
whole-delete (kanban-era doc no longer applicable to current substrate).

### J. Sanity checks (preserves FR-009 and orchestrator-dispatch invariant)

**MCP-action recognition (FR-009)**: the `mcp__server__tool` action-name normalization
lives in driver normalizers, NOT in the kanban package:

- `go/execution-kernel/internal/driver/claudecode/normalize.go`
- `go/execution-kernel/internal/driver/codex/normalize.go`
- `go/execution-kernel/internal/driver/hermes/normalize.go`

These are untouched by this retirement. Kernel governance continues to recognize MCP
calls from any agent to any MCP server (just not the deleted swarm-kanban-mcp, which
nothing inside the repo invokes anymore).

**Orchestrator dispatch (FR-006 assumption)**: `go/orchestrator/` has zero imports of
`kanban` or `boardconfig`. The only matches are two breadcrumb COMMENTS:

- `go/orchestrator/workflows/scheduler.go:104` — "and dispatches each as a child
  WorkUnitWorkflow. It replaces the kanban …"
- `go/orchestrator/workflows/sequence.go:29` — "reproducibly — not pulled from a kanban
  board."

Per D1, these stay as historical context. The orchestrator's dispatch path is fully
independent of the retired substrate.

## Risk flags (handed off to implementation)

These are the deliberate "implementation phase decides" items. Each comes with the
evidence the implementer needs:

1. **R1 — swarm-controller's kanban dependency**: 26 hits across script body; depending
   on whether `KANBAN_BOARDS_DIR` paths are actively read or are dead config-resolution,
   either strip them (partial-edit) or escalate to a follow-up spec (091?). The
   implementer reads swarm-controller line by line in its dedicated worktree.

2. **R2 — runbook per-file verdict**: 15+ runbooks reference kanban. The implementer's
   per-doc decision is partial-edit vs whole-delete; no plan-phase pre-decision.

3. **R3 — test_dispatch_atomicity_invariant.py**: 10 hits, but the dispatch atomicity
   invariant might be reusable for the Temporal orchestrator. Implementer decides:
   delete or port.

4. **R4 — clawta-pr-reviewer / clawta-blocked-escalator**: both invoke `hermes kanban`
   CLI for ticket lookup. The `hermes kanban` CLI retires with the substrate (it lives
   outside chitin but reads chitin-side kanban DBs). Implementer decides: replace the
   ticket-lookup with the orchestrator equivalent or strip the call if it's only used
   on operator-manual paths.

Each risk flag is bounded — none of them are "rethink the spec" items, all are
"per-file decisions deferred to the line-of-code context".

## Counts (for `/speckit-tasks` sizing)

- **Whole-file deletes**: ~35 (5 MCP, 6 kernel pkg, 4 kernel CLI, 2 console-UI pages,
  3 swarm/bin scripts, 1 swarm/workflows, 5 installers, 3 swarm/tests, plus
  per-runbook delete-whole decisions)
- **Partial-edits**: ~20 (1 main.go, 3 comment-only kernel hooks, 1 console-API,
  6 console-UI files, 4 swarm/bin partial, 3 swarm/workflows partial, 1 libs/contracts,
  ~1 installer partial, plus per-runbook partial-edit decisions)
- **Files to inspect during implementation**: ~15 runbooks + swarm-controller +
  test_dispatch_atomicity + 2 misc swarm/tests files
- **Comment-only or trivial edits**: ~5
- **CI gate to add**: 1 (`scripts/check-no-kanban.sh` + a workflow step)

Estimated implementation effort: 7 worktree partitions per the Phase 2 strategy preview
in `plan.md`. Each partition lands as 1–2 commits.

## What's NOT in the cull (preserved surfaces, explicit reminders)

- `go/orchestrator/` — Temporal dispatch substrate, untouched (D1 — comments stay).
- `go/execution-kernel/internal/gov/` — governance gate, untouched.
- `go/execution-kernel/internal/driver/{codex,hermes,claudecode}/normalize.go` — MCP
  action recognition (FR-009).
- `go/chainhash/` — spec 086 hash module, untouched.
- `apps/cli/` — unrelated CLI commands.
- `libs/telemetry/` — post-HP2 single-copy telemetry.
- `~/.chitin/kanban/`, `~/.hermes/kanban/` — operator data (FR-008 / SC-005).
- `.specify/specs/` history — historical, untouched (FR scope OUT).
- `docs/decisions/` history — historical, untouched.
