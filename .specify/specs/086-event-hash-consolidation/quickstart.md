# Quickstart: Event-Hash Consolidation

**Feature**: 086-event-hash-consolidation · **Date**: 2026-05-22

How to build and verify this feature once implemented. All commands run from the repo root
(`/home/red/workspace/chitin`). Per constitution §2, implementation work runs in a
dedicated worktree, not the shared checkout.

## Build

```bash
# The new shared module compiles standalone with the standard library only.
cd go/chainhash && go build ./... && cd -

# Both consumers build with the new dependency wired via the local replace directive.
cd go/execution-kernel && go build ./... && cd -
cd go/run-sdk && go build ./... && cd -
```

A clean build of all three modules confirms the `require` + `replace` wiring (Decision 1)
resolves correctly.

## Verify

### 1. One Go implementation remains (FR-001, FR-008 — User Story 3)

```bash
# Exactly one Go file defines the hash; the two old copies are gone.
rtk grep -rln "func HashEvent" go/          # → only go/chainhash/hash.go
test ! -e go/execution-kernel/internal/hash/hash.go && echo "kernel copy deleted"
test ! -e go/run-sdk/hash.go && echo "run-sdk copy deleted"
```

### 2. Go unit + corpus tests pass (FR-002, FR-003 — User Story 1)

```bash
cd go/chainhash && go test ./... && cd -          # unit tests + parity-corpus test
cd go/execution-kernel && go test ./... && cd -   # kernel suite, importers repointed
cd go/run-sdk && go test ./... && cd -            # run-SDK suite, callers repointed
```

### 3. Cross-language parity is enforced (FR-002, FR-005, FR-006 — User Story 2)

```bash
# The new vitest parity test hashes the shared corpus with the TS hashEvent and
# asserts byte-identical agreement with the Go chainhash output. Runs in CI.
rtk vitest run libs/run-sdk/tests/hash-parity.test.ts
```

To prove the guard works (User Story 2, acceptance scenario 2): introduce a deliberate
one-character change in `go/chainhash/hash.go` (e.g. alter key sorting), re-run the parity
test, and confirm it **fails**; then revert.

### 4. Existing audit chains still verify (FR-004, SC-004 — User Story 1)

```bash
# Re-verify a pre-existing chain: every event must still verify — no hash changed.
chitin-kernel chain-verify --dir ~/.chitin
```

### 5. The run SDK stays embeddable (FR-007, SC-005)

```bash
# go/run-sdk depends only on first-party, zero-dep modules. Inspect its module graph:
cd go/run-sdk && go mod graph && cd -
# Expect: only github.com/chitinhq/chitin/go/chainhash — no external/heavyweight deps.
```

## Done when

- All three Go modules build and `go test ./...` passes in each.
- `libs/run-sdk/tests/hash-parity.test.ts` passes in CI and fails on an injected divergence.
- `chitin-kernel chain-verify` reports zero verification failures on an existing chain.
- `grep` finds exactly one `HashEvent` implementation under `go/`.
