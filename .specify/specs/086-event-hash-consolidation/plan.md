# Implementation Plan: Event-Hash Consolidation

**Branch**: `feat/086-event-hash-consolidation` | **Date**: 2026-05-22 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/086-event-hash-consolidation/spec.md`

## Summary

The canonical-JSON + SHA-256 event hash that links the tamper-evident audit chain is
implemented twice in Go — `go/execution-kernel/internal/hash/hash.go` (strict) and
`go/run-sdk/hash.go` (lenient `default` fallback) — plus once in TypeScript
(`libs/contracts/src/hash.ts`, the cross-language reference). The two Go copies disagree on
one boundary case, so the same event can hash differently depending on which component
emits it.

**Technical approach**: introduce one new standalone Go module, `go/chainhash`
(`module github.com/chitinhq/chitin/go/chainhash`, stdlib-only, zero external deps).
Both the kernel and the run SDK consume it via a `require` + local `replace` directive —
the pattern the kernel already uses for `copilot-sdk-go`. The unified implementation adopts
the kernel's **strict** boundary behavior; the run SDK's lenient fallback is dropped. The
two Go copies are deleted and their call sites repointed. A cross-language parity test
(Go `chainhash` ↔ TypeScript `hashEvent`) over a shared fixture corpus runs in CI and
fails on any divergence. The canonical-JSON algorithm and hash output do not change, so
every existing audit chain still verifies.

## Technical Context

**Language/Version**: Go 1.25.0 (`go/execution-kernel`, `go/run-sdk`, and the new
`go/chainhash` all pin `go 1.25.0`). TypeScript (`libs/contracts`) — reference
implementation, modified only by an added test.

**Primary Dependencies**: `go/chainhash` — Go standard library only (`bytes`,
`crypto/sha256`, `encoding/hex`, `encoding/json`, `sort`, `fmt`). No external modules.
Cross-language parity test — `vitest` (already the repo's TS test runner).

**Storage**: N/A — the feature is pure functions over in-memory event maps.

**Testing**: `go test` for the `chainhash` unit + corpus tests and the kernel/run-SDK
suites; `vitest` for the cross-language hash-parity test.

**Target Platform**: platform-agnostic Go; runs wherever the chitin kernel and run SDK run
(Linux operator boxes, third-party embedders).

**Project Type**: shared Go library module within a polyglot monorepo (three Go modules
under `go/`, plus TypeScript libraries under `libs/`).

**Performance Goals**: event hashing is on the per-event emit path; the consolidation MUST
NOT introduce a measurable throughput regression — the algorithm is unchanged, so this is
a "no regression" bar, not a new target.

**Constraints**: `go/chainhash` MUST stay zero-external-dependency (FR-007); hash output
MUST remain byte-identical to the current implementation for all valid events (FR-004);
the kernel and run SDK are separate Go modules, so cross-module sharing uses local
`replace` directives (no repo-wide `go.work`); the new module must be registered in the
Nx project graph like its sibling Go modules so CI builds and tests it (`tasks.md` T002).

**Scale/Scope**: ~110 lines of hash logic consolidated into one module; 2 existing Go
modules updated (`go.mod` + call sites), 1 new module created; ~6 kernel import sites and
the run-SDK `manifest` package call sites repointed; 1 new parity corpus + 1 new
cross-language test.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design — still passes.*

Evaluated against `.specify/memory/constitution.md` (Chitin Repo Constitution Overlay):

| § | Gate | Verdict |
|---|---|---|
| §1 Side-effect boundary | The kernel remains the only component that gates tool calls / writes the chain. This feature relocates a **pure hash function**; it adds no component that writes chain events or gates calls. | ✅ PASS — no impact |
| §2 Branch & worktree | The feature design has no branch/worktree implications. The *implementation work* must run in a dedicated worktree per §2 — flagged for `/speckit-implement`. | ✅ PASS (execution note) |
| §3 Spec-kit promotion gate | Spec 086 has `.specify/specs/086-event-hash-consolidation/spec.md`. | ✅ PASS |
| §4 Tracked installers | No operator-box scripts added — a Go library has no installer. | ✅ PASS — N/A |
| §5 Board-aware scripts | No kanban-touching scripts. | ✅ PASS — N/A |
| §6 Swarm tooling is the exception | Kernel-local Go code belongs under the Go module tree, **not** `swarm/`. The new `go/chainhash` module is placed under `go/`, alongside `go/execution-kernel` / `go/run-sdk` / `go/orchestrator`. | ✅ PASS — placed under `go/`, not `swarm/` |

No violations. **Complexity Tracking is empty.**

## Project Structure

### Documentation (this feature)

```text
specs/086-event-hash-consolidation/
├── plan.md              # This file (/speckit-plan output)
├── spec.md              # Feature specification (/speckit-specify output)
├── research.md          # Phase 0 output — design decisions
├── data-model.md        # Phase 1 output — Event / Canonical form / Parity corpus
├── quickstart.md        # Phase 1 output — how to build and verify
├── contracts/           # Phase 1 output — chainhash Go API + corpus format
│   ├── chainhash-go-api.md
│   └── parity-corpus-format.md
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
go/chainhash/                       # NEW — the single Go hash implementation
├── go.mod                          # module github.com/chitinhq/chitin/go/chainhash, go 1.25.0, no requires
├── hash.go                         # CanonicalJSON, Sha256Hex, HashEvent (strict default)
├── hash_test.go                    # unit tests + parity-corpus test (Go side)
└── testdata/
    └── parity-corpus.json          # shared cross-language fixture corpus

go/execution-kernel/
├── go.mod                          # + require + replace => ../chainhash
└── internal/hash/                  # DELETED — ~6 importers repointed to chainhash

go/run-sdk/
├── go.mod                          # + require + replace => ../chainhash
└── hash.go                         # DELETED — manifest-package callers repointed to chainhash

libs/contracts/src/hash.ts          # UNCHANGED — cross-language reference of record
libs/run-sdk/tests/
└── hash-parity.test.ts             # NEW — vitest: TS hashEvent vs Go chainhash over the corpus
```

**Structure Decision**: A new standalone Go module `go/chainhash` is the single source of
truth for the Go event hash. It is a sibling of the existing Go modules under `go/` and is
consumed by `go/execution-kernel` and `go/run-sdk` through `require` + local `replace`
directives. The TypeScript `libs/contracts/src/hash.ts` stays put as the cross-language
reference; only a new parity test is added on the TS side. See `research.md` for why a new
module (rather than a `go.work` workspace or folding the code into an existing module) was
chosen.

## Complexity Tracking

No constitution violations — this section is intentionally empty.
