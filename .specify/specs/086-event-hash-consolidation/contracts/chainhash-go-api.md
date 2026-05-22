# Contract: `go/chainhash` Public Go API

**Feature**: 086-event-hash-consolidation · **Date**: 2026-05-22

The new module `github.com/chitinhq/chitin/go/chainhash` exposes exactly three exported
functions — the single Go implementation of the event hash. The kernel and the run SDK
consume only these. Naming is preserved from the kernel's current `internal/hash` package
so repointing importers is a mechanical `hash.` → `chainhash.` change.

## `CanonicalJSON`

```go
func CanonicalJSON(value any) (string, error)
```

Serializes `value` to canonical JSON: object keys sorted lexicographically at every level,
no whitespace, UTF-8.

- **Accepts** `nil`, `bool`, `string`, `float64`, `int`/`int32`/`int64`, `[]any`,
  `map[string]any`, recursively.
- **Returns an error** for any other type (the strict `default` — Decision 2). It MUST NOT
  fall back to a lenient re-encode.
- Pure and deterministic: the same input always yields the same output string.

## `Sha256Hex`

```go
func Sha256Hex(input string) string
```

Returns the lowercase hex-encoded SHA-256 digest of `input` (interpreted as UTF-8 bytes).
Total function — never errors.

## `HashEvent`

```go
func HashEvent(event map[string]any) (string, error)
```

The event-chain hash. Copies `event` excluding the `this_hash` key, canonicalizes the copy
via `CanonicalJSON`, and returns `Sha256Hex` of the result.

- **Excludes `this_hash`** from the hash input — an event never hashes its own digest field.
- **Returns the error from `CanonicalJSON`** unchanged if the event contains an
  unsupported value type.
- Does not mutate the caller's `event` map.

## Behavioral contract (binding on all implementations)

| Requirement | Statement |
|---|---|
| Cross-language parity | For every event in `go/chainhash/testdata/parity-corpus.json`, `HashEvent` MUST return a value byte-identical to the TypeScript `hashEvent` in `libs/contracts/src/hash.ts` (FR-002). |
| Output stability | For any valid event, the returned digest MUST equal the digest produced by the pre-consolidation implementations (FR-004). The algorithm does not change. |
| Strict types | An unsupported value type MUST yield an error from every implementation — never two different hashes, never a silent success (FR-003). |
| Zero dependencies | The module MUST compile with the Go standard library only — no `require` entries beyond first-party (FR-007). |

## Module wiring contract

`go/chainhash/go.mod`:

```
module github.com/chitinhq/chitin/go/chainhash

go 1.25.0
```

Consumers (`go/execution-kernel/go.mod`, `go/run-sdk/go.mod`) each add:

```
require github.com/chitinhq/chitin/go/chainhash v0.0.0
replace github.com/chitinhq/chitin/go/chainhash => ../chainhash
```

The `replace` form mirrors the kernel's existing
`replace github.com/github/copilot-sdk/go => ./third_party/copilot-sdk-go`.
