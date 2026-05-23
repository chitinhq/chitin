# Implementation Plan: Retire the Kanban Substrate

**Branch**: `feat/087-retire-kanban-substrate` | **Date**: 2026-05-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/087-retire-kanban-substrate/spec.md`

## Summary

Retire every active in-tree surface that reads, writes, exposes, or maintains the kanban
substrate. The work is **deletion + a small number of partial edits**, not new
implementation. The Temporal orchestrator (spec 070) already absorbs the dispatch
responsibility; spec 081 retired the board read-model; PR #908 retired the
`clawta-poller`. This retirement removes the residual code so the codebase reflects the
operating model.

Technical approach: discover the precise file-level surface (Phase 0), classify each file
as **delete-whole** / **partial-edit** / **unwire-only** (Phase 1), then execute the
deletion under the worktree+worker discipline (Phase 2, owned by `/speckit-tasks`).
Operator-owned `kanban.db` files are never touched; the kernel's protocol-agnostic
`mcp__server__tool` recognition stays intact.

## Technical Context

**Language/Version**: polyglot — Go 1.25 (kernel + orchestrator + chainhash),
TypeScript / Node ≥ 20 (apps/cli, apps/console, apps/console-api, libs/*), Python 3
(swarm/tests, swarm/workflows). No new code; no new versions.

**Primary Dependencies**: no additions. Removals: any `go.mod` / `package.json` entries
listing the retired modules (`services/swarm-kanban-mcp`, kernel `internal/kanban`,
`internal/boardconfig`, console-api kanban-route helpers if any). Phase 0 produces the
exact list.

**Storage**: the platform stops using `~/.chitin/kanban/<board>/kanban.db` and
`~/.hermes/kanban/boards/<board>/kanban.db`. The files themselves are operator-owned and
remain on disk untouched (FR-008). Chain events, gov-decisions, sentinel artifacts,
operator-report logs — all unchanged.

**Testing**: Go (`go test ./...`), TS (`pnpm nx test <project>` / vitest), Python
(`pytest swarm/tests`). FR-007 retires test files for retired code; the remaining test
suites stay green. SC-002 is the gate.

**Target Platform**: Linux operator boxes (the chitin-kernel runs here), GitHub-Actions
runners (CI). No platform changes.

**Project Type**: Nx polyglot monorepo with a Go workspace and a pnpm/Nx TypeScript
workspace plus standalone Python tooling under `swarm/`. The retirement touches all three
language partitions but adds no code to any.

**Performance Goals**: N/A — this is a deletion. Indirect goal: a measurable drop in
`go test` and `pnpm test` runtimes from the absence of the kanban test surface; a
measurable drop in `nx graph` size from the absence of `services/swarm-kanban-mcp`.

**Constraints**:
- The Temporal orchestrator's dispatch flow MUST remain functional after each commit
  (SC-003 — no platform capability lost).
- The kernel's general `mcp__server__tool` recognition MUST stay (FR-009 / Assumption).
- Operator-owned `kanban.db` files MUST stay (FR-008 / SC-005). No autonomous filesystem
  cleanup outside the repo's working tree.
- Per constitution §2: every implementation commit MUST land in a dedicated worktree
  (spec 070 enforcement), not on the shared checkout. The plan/research artifacts produced
  here are markdown and exempt — but the execution phase (`/speckit-tasks` → implement)
  is bound by §2.

**Scale/Scope**: estimated 30–80 files across the 5 categories (services/MCP, kernel
package + CLI, console-API routes, console-UI pages, swarm scripts + tests). Phase 0
returns the exact count.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Each rule in `.specify/memory/constitution.md` evaluated against this retirement:

| § | Rule | Verdict | Why |
|---|------|---------|-----|
| 1 | Side-effect boundary — kernel is the only event-chain writer; non-kernel writes go through hermes/openclaw. | ✅ PASS | Retirement REMOVES side-effect paths (kanban DB writes). No new bypasses introduced. The kernel's chain-write surface is unchanged. |
| 2 | Worker + worktree discipline — every unit of work runs in a fresh dedicated worktree; shared checkout is never a work surface. | ✅ PASS (conditional) | Plan-phase artifacts are markdown produced on the shared checkout — exempt per session precedent (spec 086/087/088 all written here). Phase 2 execution (deletion commits) MUST run in dedicated worktrees per §2 — the plan **explicitly enforces this** in the Phase 2 strategy below. |
| 3 | Spec-kit promotion gate — any `triage → ready` ticket has a matching spec. | ✅ PASS | This spec.md exists at `specs/087-retire-kanban-substrate/spec.md` and was pushed as PR #919. |
| 4 | Tracked installers — every operator-box script ships with `swarm/bin/install-*.sh`. | ✅ PASS | Retiring a script MUST also retire its installer. Phase 0's swarm-scripts category enumerates installer pairs so the deletion includes both. |
| 5 | Board-aware scripts — kanban-touching swarm scripts accept `--board` flag. | ✅ PASS (vacuous) | All board-aware scripts are being deleted; no surviving script depends on the board concept. |
| 6 | Swarm tooling is the exception, not the pattern — kanban-local code belongs under `cmd/`/`internal/`/`libs/`, not `swarm/`. | ✅ PASS | Retirement reduces the `swarm/` surface; aligns with §6's direction (less transitional housing). |

**Initial gate verdict**: 6/6 PASS. Phase 0 research proceeds. No complexity tracking
entries required.

## Project Structure

### Documentation (this feature)

```text
specs/087-retire-kanban-substrate/
├── plan.md              # this file (/speckit-plan output)
├── spec.md              # the user-facing spec
├── research.md          # Phase 0 — surface map + design decisions
├── data-model.md        # Phase 1 — kanban substrate entity map (what exists, what retires)
├── quickstart.md        # Phase 1 — how to verify the retirement post-merge
├── contracts/           # Phase 1 — interface contracts that disappear
│   ├── retired-mcp-server.md
│   ├── retired-cli-subcommands.md
│   ├── retired-console-api-routes.md
│   └── retired-console-ui-pages.md
├── checklists/
│   └── requirements.md  # written at /speckit-specify
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root — surface being retired)

Concrete tree of surfaces this retirement touches. The exact file list lands in
`research.md` after Phase 0 discovery.

```text
# RETIRED (whole-directory delete)
services/swarm-kanban-mcp/                  # FR-002 — MCP server
go/execution-kernel/internal/kanban/        # FR-003 — kernel kanban package
go/execution-kernel/internal/boardconfig/   # FR-003 — board-config package

# RETIRED (partial edits — strip kanban registrations from multi-purpose files)
go/execution-kernel/cmd/chitin-kernel/      # FR-003 — unwire `kanban` + `board-config` subcommands
apps/console-api/src/                       # FR-004 — strip kanban routes; keep argus/ELO/gov-decisions routes
apps/console/src/                           # FR-005 — delete kanban pages; keep sessions/orchestrator pages
nx.json, pnpm-workspace.yaml, *.project.json # FR-002/004/005 — strip retired project graph entries

# RETIRED (whole-directory or per-file delete in swarm)
swarm/bin/                                  # FR-006 — residual kanban-polling scripts + their installers
swarm/workflows/                            # FR-006 — kanban-dispatch.lobster + deps
swarm/tests/                                # FR-007 — Python tests for retired dispatch

# PRESERVED (explicitly unaffected)
go/orchestrator/                            # spec 070 — dispatch substrate, untouched
go/execution-kernel/internal/gov/           # FR-009 — `mcp__server__tool` recognition, untouched
go/chainhash/                               # spec 086 — hash module, untouched
apps/cli/                                   # CLI commands unrelated to kanban
libs/telemetry/                             # post-HP2 single-copy telemetry
~/.chitin/kanban/, ~/.hermes/kanban/        # FR-008 — operator-owned data, NEVER touched
```

**Structure Decision**: The retirement is a polyglot multi-surface cull. Phase 0 maps the
exact file surface; Phase 1 specifies the contracts that disappear; Phase 2
(`/speckit-tasks`) breaks the cull into task units, each runnable in its own worktree per
§2.

## Phase 2 Execution Strategy (preview — owned by `/speckit-tasks`)

Each task in the eventual tasks.md MUST be runnable as a discrete worktree-isolated
worker dispatch per constitution §2. Suggested partition (one PR per partition, or one PR
with one logical commit per partition):

1. **services/swarm-kanban-mcp/** (FR-002) — whole-directory delete + project graph clean.
2. **go/execution-kernel/internal/kanban + boardconfig + CLI unwire** (FR-003) — kernel
   binary loses two packages and two subcommands.
3. **apps/console-api/ kanban routes** (FR-004) — partial edits to the route table.
4. **apps/console/ kanban pages + nav** (FR-005) — page deletes + nav-bar unwire.
5. **swarm/ residual scripts + installers + tests** (FR-006 + FR-007) — paired
   installer/script deletes per constitution §4.
6. **Active operator docs** (FR-010) — README + runbooks update.
7. **Cross-cutting grep gate** — the final SC-001 / FR-011 verification commit (which
   passes if the gate is dirty-free, fails the PR otherwise).

Each partition lands as its own worktree-isolated commit. The PR may bundle them or split
them; the worktree discipline is the invariant per §2.

## Complexity Tracking

No constitution violations. This section is intentionally empty.

