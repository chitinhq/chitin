# Implementation Plan: Polyglot Monorepo Layout

**Branch**: `074-polyglot-monorepo-layout` | **Date**: 2026-05-20 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/074-polyglot-monorepo-layout/spec.md`
**Companion analysis**: `docs/strategy/chitin-monorepo-audit.md`

## Summary

Close the Nx **registration gap** and converge the repo on one polyglot
layout. Nx already orchestrates Go and Python here (`execution-kernel`,
`chitin-analysis`, `chitin-router-plugin-api-python` are live Nx projects) —
the failure is that ~half the code (`go/run-sdk`, `python/argus`, surviving
`services/*` and `swarm/*`, `bench/`) has no `project.json`, so `nx affected`,
the graph, caching, and CI affected-detection are blind to it.

Done in **four independently shippable phases**: Phase 0 culls dead drift so
the project inventory is honest; Phase 1 registers every survivor (zero file
moves — the real integration win); Phase 2 standardizes registration, targets,
and tags and routes CI through Nx; Phase 3 relocates `go/*` and `python/*`
under `apps/`/`libs/` (the cosmetic fix, staged last, one PR per project).

## Technical Context

**Languages**: TypeScript/Angular, Go (`go/execution-kernel`, `go/run-sdk`),
Python (`python/argus`, `python/analysis`, `services/*`, `swarm/*`, `bench/`).

**Primary tooling**: Nx 22.6.5; `nx:run-commands` for non-TS targets; the
`type:* / scope:* / layer:* / lang:*` tag taxonomy; `tools/lint/layer-tag-coverage.ts`;
`eslint.config.mjs` boundary rules; `.github/workflows/ci.yml`.

**Primary dependencies**: none new — `nx:run-commands` shelling to `go`,
`pytest`/`uv`, `tsc` is sufficient and already proven (audit §2, Out-of-Scope).

**Testing**: a workspace assertion that `nx show projects` covers every
survivor; the extended `layer-tag-coverage` gate as an executable check; per
Go relocation `go build ./... && go test ./...`; per Python/TS relocation
`nx test <project>`. e2e-default: SC-002 (affected detection) verified against
a real git diff.

**Constraints**: specs 069 (decommission agent-bus + Octi) and 070 (Temporal
orchestrator) define what code is deletion-bound — this spec MUST skip it
(FR-012, INV-006). Go module-path rewrites on relocation are the only
non-mechanical risk and are isolated to Phase 3, one project per PR.

**Project type**: monorepo tooling/structure refactor — registration metadata
+ file moves, no build-logic change (INV-005).

**Scale/scope**: ~10 orphan directories to register (Phase 1); ~5 projects to
relocate (Phase 3); 2 drift directories to cull (Phase 0).

## Constitution Check

| Principle | Assessment |
|-----------|------------|
| §1 Side-effect boundary | PASS — registration + relocation change no side-effect routing; behavior-preserving (INV-005). |
| §2 Workers + worktrees | PASS — each phase ships via PR from a worktree; no change to worker/worktree law. |
| §3 Spec-kit gate | PASS — 074 has `spec.md` + this `plan.md`; `tasks.md` accompanies. |
| §4 Tracked installers | PASS — no installer changes; CI workflow edits stay tracked. |
| §5 Board-aware scripts | PASS — board-aware swarm scripts keep their `--board` flag through registration. |
| §6 Swarm tooling | PASS — swarm tooling is registered in place (Phase 1) before any move (Phase 3). |

No violations → Complexity Tracking empty.

## Decision (from the audit)

Adopt **Option B — type-first, language as a tag** (audit §3): every project
under `apps/` or `libs/` by *what it is*; language is the `lang:*` tag, and a
genuine multi-language domain uses per-language subfolders (the
`libs/router-plugin-api` precedent). Reached by staged migration so the
high-value, low-risk registration is never blocked by the mechanical moves.

Two open questions are resolved at their phase, not now:
- **`swarm/` granularity** — split into several Nx projects along the existing
  `swarm/tests/` boundaries rather than one coarse project; finalized at
  Phase 1 planning once 070's surviving surface is known.
- **`go/run-sdk` placement** — fold into `libs/run-sdk/go/` (same SDK contract
  as the TS `run-sdk`); confirmed at Phase 3.

## Migration Phases

- **Phase 0 — Cull drift (FR-016).** `git rm` the abandoned
  `apps/chitin-dashboard` React app (superseded by `apps/chitin-console`;
  also stops committing a `dist/`); remove the untracked `apps/agentguard-vscode`
  leftover. `nx show projects` afterward reflects only real projects.
- **Phase 1 — Close the registration gap (FR-001–003, US1).** Add a
  `project.json` (standard targets + full tag set) to every surviving orphan:
  `go/run-sdk`, `python/argus`, `bench/`, `services/mini-mcp`,
  `services/swarm-kanban-mcp`, and each surviving `swarm/*` component. **Zero
  file moves.** Value: `nx affected`/graph/cache become correct across all
  three languages.
- **Phase 2 — Standardize + route CI through Nx (FR-004–006, US2).** One
  registration mechanism (`project.json`); consistent `build`/`test`/`lint`/
  `validate` target names; full tag set on every project; extend
  `layer-tag-coverage` to Go/Python and fail the build on a gap; replace the
  bespoke `go test` / `go vet` / `python -m unittest` CI steps with
  `nx affected` / `nx run-many`.
- **Phase 3 — Converge the layout (FR-007–011, US3).** Relocate `go/*` and
  `python/*` under `apps/`/`libs/` by type — one isolated, mechanical PR per
  project. Go moves rewrite the `go.mod` module path + every internal import
  atomically (`go build ./... && go test ./...` gate); Python moves update
  relative path deps; `pnpm-workspace.yaml`, root `tsconfig.json` references,
  and `vite.config.ts` globs are corrected to match. Ends with the
  contributor layout/registration guide (FR-014).

Phase ordering is strict (0 → 1 → 2 → 3); each is independently shippable and
delivers standalone value (FR-015).

## Project Structure

Destination (Option B), reached at Phase 3:

```
apps/        all runnable units — Angular, Node, the Go kernel, Python CLIs
libs/        all dependency-only packages; multi-language domains use
             per-language subfolders (router-plugin-api pattern)
tools/       Nx generators + lint
(no top-level go/ or python/ language folder)
```

Phases 0–2 leave files where they are; only Phase 3 moves them.

## Complexity Tracking

None — no constitution violations. The single non-mechanical risk (Go
module-path rewrite) is isolated to Phase 3 and gated per-PR by
`go build ./... && go test ./...`.
