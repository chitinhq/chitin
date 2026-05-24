# Spec 061: Unified spec model + framework adapters (L1)

**Status**: RATIFIED 2026-05-23 — Slices 1 + 2 shipped (PR #809, #836).
Implements layer L1 of charter spec 060. Open questions Q1, Q2, Q4
resolved by implementation; Q3 (OpenSpec format) remains open.

**Author lens (Knuth)**: name the normalized shape exactly. Every field
that two adapters would populate differently is a future bug. The model
is the contract L2–L7 depend on — vagueness here propagates upward
through the whole stack.

## Summary

Charter 060 R2 makes multi-framework spec support non-negotiable: spec-kit,
OpenSpec, and Superpowers are all first-class spec *inputs*. This spec
defines the **unified spec model** — one normalized shape — and the
**adapter interface** that maps each framework into it. Layers L2–L7
consume only the unified model and never see a source format.

## Motivation

- **Three formats, one runtime.** spec-kit ships `.specify/specs/NNN/spec.md`;
  OpenSpec and Superpowers each have their own layout. Without
  normalization, every layer above L1 would branch on format — an
  un-maintainable mess.
- **The model is the spine.** charter R1: L2–L7 build on L1's contract.
  A spec must resolve to a stable `spec_id`, an ordered requirement
  list, and acceptance criteria *regardless* of who authored it.
- **chitin already has 53 house-format specs.** They are spec-kit-shaped
  enough to adapt; the model must not orphan them.

## The unified spec model

A spec normalizes to:

```
UnifiedSpec {
  spec_id:          string   # stable id — e.g. "060", "ic-001"
  title:            string
  status:           enum(draft|ratified|superseded)
  source_framework: enum(spec-kit|openspec|superpowers|house)
  source_path:      string   # provenance — where it was adapted from
  requirements:     [ Requirement{ id, text } ]        # R1, R2, ...
  acceptance:       [ AcceptanceCriterion{ id, text } ] # AC1, AC2, ...
  boundaries:       [ string ]                          # boundary cases
  slices:           [ Slice{ id, scope, requirement_ids[] } ]
  open_questions:   [ Question{ id, text, proposed } ]
}
```

`spec_id` is the stable key (constitution §1 — the `NNN` prefix). It is
the join key for L2/L3 attribution (spec 062), replay (063), and the
`/goal` corpus (065).

## Requirements

### R1 — the normalized model is the only upward contract

L2–L7 consume `UnifiedSpec` exclusively. No layer above L1 reads a
source file or branches on `source_framework`. `source_framework` and
`source_path` exist for provenance/debugging only.

### R2 — the adapter interface

An adapter is a pure function `parse(source) -> UnifiedSpec` plus a
`detect(path) -> bool`. Adapters are stateless, deterministic (same
source ⇒ same `UnifiedSpec`), and side-effect-free. Registration is a
single list so a new framework is one entry.

### R3 — spec-kit adapter (reference implementation)

The spec-kit / house-format adapter parses `.specify/specs/NNN-slug/spec.md`:
`# Spec NNN: Title` → `spec_id` + `title`; `### RN —` / `**RN —**`
headings → requirements; `**ACN**` / `### ACN` → acceptance; the
`## Boundary cases` and `## Open questions` sections → their lists;
`## Slice plan` → slices. It MUST adapt all 53 existing house-format
specs without loss.

### R4 — OpenSpec adapter

An adapter for OpenSpec's format. (OpenSpec layout/format to be
confirmed — see Q3.) Same `parse`/`detect` contract.

### R5 — Superpowers adapter

An adapter for the Superpowers plan/skill format under `docs/superpowers/`.
Superpowers "plans" are closer to implementation plans than specs — the
adapter MUST map a plan to `UnifiedSpec` honestly, marking fields it
cannot populate rather than fabricating them.

### R6 — round-trip integrity

For the native house format, `parse` then a `render` back to markdown
MUST be lossless enough that a ratified spec survives a round trip
without semantic change. (Full bidirectional round-trip for OpenSpec /
Superpowers is a non-goal — see Non-goals.)

## Boundary cases

1. **Spec with no requirements / no ACs** (e.g. a charter spec like
   060) → `requirements`/`acceptance` may be empty; `status` and a
   non-empty `slices` or narrative still make it valid.
2. **Ambiguous `spec_id`** (two dirs share `036`) → the adapter
   surfaces the collision as an error, never silently picks one
   (mirrors spec 053 / 050 resolution discipline).
3. **Malformed source** → `parse` raises a typed error naming the file
   and the failed section; it never returns a half-populated model.
4. **A framework chitin doesn't have an adapter for** → `detect`
   returns false everywhere; the spec is simply not ingested, logged.

## Open questions

- **Q1 — model owner** (charter Q1). ~~Does `UnifiedSpec` live as a Go
  type in the kernel, a Python service, or a language-neutral schema
  (JSON Schema) both consume?~~ **Resolved**: language-neutral JSON Schema
  as the contract, with Go and Python bindings. Implemented in
  `libs/contracts/schemas/unified-spec.schema.json`,
  `go/execution-kernel/internal/spec/spec.go`, and
  `python/analysis/unified_spec.py`.
- **Q2 — adapter location.** ~~One `specs/adapters/` package, or each
  adapter near its framework?~~ **Resolved**: one package, one registry.
  Implemented in `go/execution-kernel/internal/spec/adapter/` and
  `python/analysis/spec_adapter/`.
- **Q3 — OpenSpec format.** OpenSpec's on-disk format must be confirmed
  before R4 is implementable. Operator/design-review input. **Still open**.
- **Q4 — adapter priority** (charter Q5). ~~spec-kit ships first (R3).
  Which is second — OpenSpec or Superpowers?~~ **Resolved**: Superpowers
  second (shipped Slice 2, PR #836), OpenSpec third.

> **Note**: Slices 1 and 2 are complete and merged. Slice 3 (OpenSpec
> adapter, T024–T028) is blocked on Q3. This ticket covers the completed
> work; Slice 3 will be a separate follow-up once Q3 resolves.

## Non-goals

- No bidirectional round-trip for OpenSpec/Superpowers — adapters are
  one-way (source → `UnifiedSpec`) except the native house format (R6).
- No spec *authoring* UI — `/speckit-specify` and hand-authoring stay
  as-is. 061 is ingestion/normalization only.
- No migration of the 53 house specs to another format — they are
  adapted in place.

## Acceptance criteria

- **AC1** — `UnifiedSpec` schema is defined and documented.
- **AC2** — the spec-kit/house adapter parses all 53 existing
  `.specify/specs/*/spec.md` into valid `UnifiedSpec` objects, zero
  losses, verified by a test over the whole specs tree.
- **AC3** — `detect` correctly routes a path to exactly one adapter.
- **AC4** — a malformed spec raises a typed error (boundary 3), proven
  by test.
- **AC5** — round-trip of a house-format spec is lossless (R6).
- **AC6** — adding a new adapter is a one-entry registry change (R2).

## Slice plan

- **Slice 1** ✅ — `UnifiedSpec` schema + adapter interface + spec-kit/house
  adapter. R1, R2, R3, R6. AC1–AC6 for the house format. Merged PR #809.
- **Slice 2** ✅ — Superpowers adapter (R5). Merged PR #836.
- **Slice 3** — OpenSpec adapter (R4) — blocked on Q3 (OpenSpec format
  confirmation). Will be a separate follow-up ticket once Q3 resolves.
