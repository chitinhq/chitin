# Implementation Plan: Spec 061 — Unified spec model + framework adapters

**Branch**: `main` (already merged as #809) | **Date**: 2026-05-23 | **Spec**: [.specify/specs/061-unified-spec-model/spec.md](./spec.md)

**Input**: Feature specification from `.specify/specs/061-unified-spec-model/spec.md`

## Summary

Define the `UnifiedSpec` schema as the single normalized contract for all spec frameworks (spec-kit, OpenSpec, Superpowers). Build the adapter interface (`detect` + `parse`) and a reference spec-kit/house adapter that parses all existing house-format specs into `UnifiedSpec` objects. Ship JSON Schema as the canonical contract with Go, TypeScript, and Python bindings.

## Technical Context

**Language/Version**: Go 1.23 (primary kernel), TypeScript (contracts lib), Python 3.13 (analysis)

**Primary Dependencies**: Zod (TS bindings), Go standard lib, Python dataclasses

**Storage**: Filesystem `.specify/specs/NNN-slug/spec.md`

**Testing**: Go `testing` package, Python `pytest`, Vitest (TS)

**Target Platform**: Linux server (chitin execution kernel + analysis tooling)

**Project Type**: Library (multi-language bindings)

**Performance Goals**: Parse 88 specs in < 1 second

**Constraints**: Adapters must be pure, deterministic, side-effect-free

**Scale/Scope**: 88 existing house specs; 3 source frameworks planned

## Constitution Check

✅ Spec-driven (this IS the spec), TDD (tests shipped alongside), no scope creep.

## Project Structure

```text
# Canonical schema (language-neutral)
libs/contracts/schemas/unified-spec.schema.json     # JSON Schema draft-07

# TypeScript bindings (libs/contracts)
libs/contracts/src/unified-spec.schema.ts            # Zod schema + types

# Go bindings (execution kernel)
go/execution-kernel/internal/spec/spec.go             # UnifiedSpec types
go/execution-kernel/internal/spec/adapter/adapter.go  # SpecAdapter interface + registry
go/execution-kernel/internal/spec/adapter/speckit/    # spec-kit adapter (R3)
go/execution-kernel/internal/spec/adapter/superpowers/ # Superpowers adapter (R5)

# Python bindings (analysis)
python/analysis/unified_spec.py                       # UnifiedSpec dataclass
python/analysis/speckit_adapter.py                    # spec-kit adapter (Python)

# Spec directory
.specify/specs/061-unified-spec-model/
├── spec.md      # This feature specification
├── plan.md      # This file
└── tasks.md     # Task list (derived from this plan)
```

## Complexity Tracking

No constitution violations.