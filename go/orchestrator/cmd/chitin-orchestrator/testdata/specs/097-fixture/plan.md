# Plan: 097-fixture (round-trip test fixture)

**Branch**: `feat/097-fixture` | **Date**: 2026-05-23 | **Spec**: [spec.md](spec.md)

## Summary

Three tasks, all mapping to `code.implement`. The spec-077 adapter's `MapCapability` keyword extractor matches "implement" in every description, yielding a uniform DAG that the SchedulerWorkflow can dispatch against any driver in the default registry that declares `code.implement` (claudecode, codex, openclaw).

## Project Structure

```text
go/orchestrator/cmd/chitin-orchestrator/testdata/097-fixture/
├── spec.md
├── plan.md
├── tasks.md
└── checklists/
    └── requirements.md
```

This fixture is consumed only by tests in `go/orchestrator/cmd/chitin-orchestrator/`; it is never promoted to a real spec.
