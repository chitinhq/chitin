# Phase 0 Research: Event-Hash Consolidation

**Feature**: 086-event-hash-consolidation · **Date**: 2026-05-22

The spec carried **zero `[NEEDS CLARIFICATION]` markers** — its Assumptions section
deferred exactly one thing to planning: *"a neutral shared module (or an equivalent
workspace arrangement) is required … the exact mechanism is a planning-phase decision."*
This document resolves that and the supporting design decisions, against verified facts.

## Verified facts (source reads, 2026-05-22)

- **Three separate Go modules, no workspace.** `go/execution-kernel`, `go/run-sdk`,
  `go/orchestrator` each have their own `go.mod`. There is **no `go.work`** at the repo root.
- **Module paths**: `github.com/chitinhq/chitin/go/execution-kernel`,
  `github.com/chitinhq/chitin/go/run-sdk` — both pin `go 1.25.0`.
- **`go/run-sdk/go.mod` has zero `require` entries** — it is a clean, dependency-free module.
- **`go/execution-kernel/go.mod` already uses a local `replace`**: line 33,
  `replace github.com/github/copilot-sdk/go => ./third_party/copilot-sdk-go`.
- **The two Go hash files are otherwise-identical logic with one divergence.**
  `go/execution-kernel/internal/hash/hash.go` (package `hash`, exported
  `CanonicalJSON`/`Sha256Hex`/`HashEvent`) — `default` case at line 87 returns
  `fmt.Errorf("unsupported type %T in canonical JSON", value)`.
  `go/run-sdk/hash.go` (package `manifest`, unexported `canonicalJSON`/`sha256Hex`/
  `hashEvent`) — `default` case at lines 75-85 marshals to JSON and re-decodes (lenient
  fallback). Everything else (null/bool/string/number/array/object handling, key sorting,
  `this_hash` exclusion) is identical.
- **The TS reference** `libs/contracts/src/hash.ts` exports `canonicalJSON`/`sha256Hex`/
  `hashEvent`; the kernel file's header comment already declares it MUST be byte-identical
  to this TS file.
- **The existing parity test** `libs/run-sdk/tests/go-sdk-schema.test.ts` runs
  `go run ./cmd/sdk-fixture` and validates output against `EventSchema` — it proves
  **schema** validity, **not hash agreement**. No cross-language *hash* parity test exists.

---

## Decision 1 — Code-sharing mechanism: a new `go/chainhash` module + local `replace`

**Decision**: Create a new standalone Go module `go/chainhash`
(`module github.com/chitinhq/chitin/go/chainhash`, `go 1.25.0`, no `require` block —
standard library only). Both `go/execution-kernel` and `go/run-sdk` add
`require github.com/chitinhq/chitin/go/chainhash` plus
`replace github.com/chitinhq/chitin/go/chainhash => ../chainhash`.

**Rationale**:
- It matches a pattern **already in the repo** — `go/execution-kernel/go.mod:33` resolves a
  local module with `replace`. No new convention is introduced.
- `chainhash` stays stdlib-only and zero-dependency, so the run SDK remains embeddable by
  third parties (FR-007). Adding `chainhash` takes run-sdk from 0 to 1 `require`, but that
  one require is a first-party, zero-dep sibling module in the same repository — not a
  meaningful dependency increase, and SC-005 ("no increase in dependencies") is read in
  spirit: no *external* or heavyweight dependency is added.
- `replace` directives are ignored by downstream consumers, but
  `github.com/chitinhq/chitin/go/chainhash` is a real, fetchable module path in this
  multi-module repository — a third party that `go get`s `…/go/run-sdk` resolves
  `…/go/chainhash` from the same GitHub repo, exactly as the three existing modules already
  coexist.

**Alternatives considered**:
- **Repo-wide `go.work` workspace** — rejected. `go.work` is a local-development construct;
  it is ignored by `go get`/`go install`, so published consumption still needs each
  `go.mod` to `require` the shared module. It would also introduce a new repo-wide build
  convention (none exists today) with a wider blast radius across all Go builds, for no
  gain over `replace`.
- **Move the hash into `go/execution-kernel` as an exported (non-`internal`) package** —
  rejected. The run SDK would then `require` the entire kernel module, pulling in
  `copilot-sdk`, OTLP, `modernc.org/sqlite`, and `mvdan.cc/sh` — a large dependency
  increase that directly violates FR-007 and SC-005.
- **Fold the hash into `go/run-sdk` as an exported package** — rejected. It would make the
  kernel depend on the run-SDK module (an inverted, surprising dependency direction), and
  the run SDK's root package is `manifest` with mixed concerns. A dedicated neutral module
  is cleaner and has an obvious home.

## Decision 2 — Boundary behavior: strict (the kernel's behavior wins)

**Decision**: `chainhash`'s canonical-JSON `default` case returns
`fmt.Errorf("unsupported type %T in canonical JSON", value)` — the kernel's current strict
behavior. The run SDK's lenient JSON-round-trip fallback (`go/run-sdk/hash.go:75-85`) is
**dropped**.

**Rationale**:
- It is the Pathfinder UP1 recommendation, and it is the behavior the cross-language
  contract is already pinned to (the kernel file's "must be byte-identical to the TS impl"
  comment).
- The run SDK already converts every payload to plain JSON values (`map[string]any`) before
  hashing — its lenient `default` arm is effectively **unreachable in real SDK flow**, so
  removing it changes no observed behavior for real callers.
- A hard error surfaces a genuinely malformed payload at emit time, instead of silently
  hashing a value the kernel would reject — fail-loud is the correct posture for a
  chain-integrity primitive (FR-003: both Go paths must behave identically).

**Alternatives considered**: keep the lenient fallback everywhere — rejected; it masks
malformed payloads and would force the kernel to *loosen* a stricter, already-published
contract.

## Decision 3 — Kernel `internal/hash` disposition: delete and repoint importers

**Decision**: Delete `go/execution-kernel/internal/hash/hash.go` and rewrite its importers
(~6 sites, exact count confirmed by grep at task time) to import
`github.com/chitinhq/chitin/go/chainhash`. No re-export shim is left behind.

**Rationale**: FR-001 ("exactly one Go implementation") and FR-008 ("no private copy"). The
importers call `hash.CanonicalJSON` / `hash.Sha256Hex` / `hash.HashEvent`; `chainhash`
exports the identical names, so the rewrite is a mechanical import-path + package-qualifier
change (`hash.` → `chainhash.`). A shim would leave two packages where one suffices —
"prefer deletion over abstraction" (Pathfinder UP1).

## Decision 4 — run-SDK `hash.go` disposition: delete and repoint callers

**Decision**: Delete `go/run-sdk/hash.go`. Its callers inside the run SDK's `manifest`
package call the unexported `canonicalJSON`/`sha256Hex`/`hashEvent`; rewrite those call
sites to the exported `chainhash.CanonicalJSON`/`Sha256Hex`/`HashEvent`.

**Rationale**: FR-008. The run SDK keeps zero *external* dependencies and gains one
first-party `require` (`chainhash`), consistent with Decision 1.

## Decision 5 — Parity enforcement: a shared corpus + a cross-language hash-parity test

**Decision**: Add a shared **parity corpus** — a JSON fixture file of events covering every
edge case from the spec — at `go/chainhash/testdata/parity-corpus.json`. Add two tests that
consume it: a Go test in `go/chainhash/hash_test.go`, and a new cross-language vitest test
`libs/run-sdk/tests/hash-parity.test.ts` that hashes every corpus event with the TypeScript
`hashEvent` and asserts byte-identical output against the Go-produced hashes. The existing
`cmd/sdk-fixture` + `go-sdk-schema.test.ts` harness is the working model for invoking Go
from a vitest test.

**Rationale**: FR-002/FR-005/FR-006/SC-003. The existing `go-sdk-schema.test.ts` proves
schema validity but never compares hashes — hash parity needs its own test. Because the
repo's CI already runs vitest, the new test runs in CI for free (FR-006). Co-locating the
corpus with `chainhash` keeps the single source of truth for *both* the implementation and
its conformance fixtures.

**Alternatives considered**:
- A Go-only parity test — rejected; it cannot catch TypeScript drift (FR-002 requires
  cross-language agreement).
- Extending the existing schema test — rejected; schema validity ≠ hash agreement, and
  overloading one test obscures both intents.

**Corpus edge cases to include** (from spec "Edge Cases"): a payload value at the
previously-divergent `default` boundary; empty payload / null envelope fields; deeply
nested objects (key ordering at every depth); non-ASCII / Unicode string values; numeric
edge values (large integers, floats); and at least one event shaped like a real historical
chain record to anchor FR-004 (output unchanged).

## Open risks carried into tasks/implementation

- **Number serialization parity** — Go `json.Marshal` and JS `JSON.stringify` agree for
  ordinary numbers but can differ at extremes (very large integers, float precision). The
  parity corpus MUST include numeric extremes so the parity test catches any real
  divergence; the kernel comment already asserts byte-identical parity for current usage.
- **Importer count** — Pathfinder UP1 estimated ~6 kernel importers of `internal/hash`;
  the exact set is a `grep` at task time, not an assumption.
