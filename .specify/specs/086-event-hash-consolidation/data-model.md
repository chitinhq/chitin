# Phase 1 Data Model: Event-Hash Consolidation

**Feature**: 086-event-hash-consolidation · **Date**: 2026-05-22

This feature is a pure-function library consolidation — there is no database and no
persistent storage. The "data model" is the three conceptual entities the hash operates
over and the fixture that pins their behavior. No state transitions.

## Entity: Event

The unit being hashed. In Go it is a `map[string]any`; in TypeScript a
`Record<string, unknown>`. It is the canonical event envelope plus its payload.

| Aspect | Detail |
|---|---|
| Shape | A string-keyed map of envelope fields (e.g. `chain_id`, `seq`, `prev_hash`, `event_type`, `ts`, `payload`) plus its own `this_hash` field. |
| Hash input | All fields **except `this_hash`** — `this_hash` is excluded before canonicalization so an event never hashes itself. |
| Value types permitted | `null`, `bool`, `string`, number (integer or float), array of permitted values, string-keyed map of permitted values. |
| Invalid value types | Anything else (functions, channels, arbitrary structs not reduced to the above). Per Decision 2, these produce a defined error — they are never silently hashed. |

Validation rules (from FR-003): a value type outside the permitted set MUST cause
`HashEvent` to return an error; it MUST NOT produce a hash.

## Entity: Canonical form

The deterministic serialization of an Event that the SHA-256 hash is computed over. Two
events hash to the same value **if and only if** their canonical forms are byte-identical.

| Rule | Detail |
|---|---|
| Key ordering | Object keys sorted lexicographically **at every level of nesting**, not just the top level. |
| Whitespace | None — no spaces, no newlines, no indentation. |
| Encoding | UTF-8. |
| Primitives | `null` → `null`; booleans → `true`/`false`; strings and numbers → their standard JSON encoding. |
| Output | A SHA-256 hex digest of the canonical-form string. |

This algorithm is **unchanged** by this feature (FR-004): the consolidation moves the code,
it does not alter the bytes produced for any valid event. That invariant is what guarantees
existing audit chains keep verifying.

## Entity: Parity corpus

The shared, version-controlled set of fixture events that every implementation is tested
against. It is the conformance contract referenced by FR-002 and FR-005.

| Aspect | Detail |
|---|---|
| Location | `go/chainhash/testdata/parity-corpus.json` — co-located with the canonical Go module. |
| Consumed by | the Go corpus test (`go/chainhash/hash_test.go`) and the cross-language vitest parity test (`libs/run-sdk/tests/hash-parity.test.ts`). |
| Required coverage | every edge case in spec §"Edge Cases": the divergent-`default` boundary value, empty/null content, deep nesting, non-ASCII/Unicode strings, numeric extremes, and a historical-chain-shaped record. |
| Format | See `contracts/parity-corpus-format.md`. |

The corpus is the single fixture both languages share; it has no schema beyond the contract
file and changes only when a new edge case is identified.
