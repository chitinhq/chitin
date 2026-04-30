# F4 — OTEL Emit MVP Plan

**Date:** 2026-04-29
**Spec:** [`2026-04-29-otel-emit-mvp-design.md`](../specs/2026-04-29-otel-emit-mvp-design.md)
**Branch:** `feat/f4-otel-emit-mvp`
**Worktree:** `/home/red/workspace/chitin-f4-otel`
**Forcing function:** 2026-05-07 talk demo beat (8 days)

## Task breakdown

| # | Task | Files | Ship-blocking? |
|---|------|-------|----------------|
| 1 | Span-projection package skeleton | `internal/emit/otel.go`, `internal/emit/otel_test.go` | yes |
| 2 | Implement `projectToSpan(*event.Event) span` for 4 event types | `otel.go` | yes |
| 3 | Implement parent-rule logic (within-chain + cross-chain + root) | `otel.go` (helper `parentSpanID`) | yes |
| 4 | OTLP/HTTP JSON body encoder (`encodeRequest([]span) []byte`) | `otel.go` | yes |
| 5 | HTTP POST with timeout, fire-and-forget goroutine | `otel.go` (helper `postSpan`) | yes |
| 6 | Wire into `emit.Emit` after `tx.Commit` (config-gated) | `emit.go` | yes |
| 7 | Configuration via `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` env | `otel.go` (helper `endpointFromEnv`) | yes |
| 8 | Test 1 — `TestProjectToSpan_Mapping` (table-driven, 4 event types) | `otel_test.go` | yes |
| 9 | Test 2 — `TestParentSpanIdRules` (3 branches) | `otel_test.go` | yes |
| 10 | Test 3 — `TestKernelSurvivesOTELFailure` (endpoint refused) | `otel_test.go` | yes |
| 11 | Demo runbook entry — local otelcol-contrib + verification | `docs/superpowers/demo-runbook.md` | yes (talk demo) |
| 12 | Update `docs/event-model.md` → mark OTEL projection as live (not just specced) | `docs/event-model.md` | post-merge |
| 13 | Update `docs/observations/governance-debt-ledger.md` if any decision-needed entries surface | `docs/observations/governance-debt-ledger.md` | optional |

## Order of operations

```
1 → 2 → 3 → 4 → 5 → 6 → (7 in parallel with any of 4–6)
     ↓
     8 → 9 → 10 (run after each of 2–6 lands; tighten as code stabilizes)
                ↓
                11 (manual integration verification)
                ↓
                12 → 13 (post-merge cleanup)
```

Tasks 8–10 are **continuous** — they run alongside implementation, not after. CI green on each PR push.

## Validation gates

- **Knuth gate (boundary correctness):** for each of the 4 event types, what does `projectToSpan` emit when (a) prev_hash is nil, (b) parent_chain_id is nil, (c) both are nil, (d) duration_ms is missing, (e) tool_name is missing, (f) decision is missing? Each branch named in tests before code. Heuristic 4 from Knuth's lens.
- **Da Vinci gate (observation over dogma):** before merging, run a real otelcol-contrib container with the talk's collector config; capture an actual chitin-instrumented session; verify spans land. The "OTLP-compatible" claim must be observed, not assumed. Heuristic 2 from da Vinci's lens.
- **External contract verify:** before encoding the OTLP body, fetch the [OpenTelemetry Protocol Specification — JSON encoding section](https://github.com/open-telemetry/opentelemetry-proto/blob/main/docs/specification.md) and confirm the proto3-JSON field names and `unixNano`-as-string convention. Per `feedback_verify_external_contracts.md` — adjacent code is not proof, the spec is.

## Cuts if F4 slips

In order of cut priority (cut bottom first):

1. Test 4 manual integration (move to demo-day morning checklist instead of pre-merge)
2. `service.version` attribute (hard-code "0.0.0", still ship)
3. `input_bytes` attribute (drop entirely; demo doesn't need it)
4. **Hard floor (do not cut):** mapping for all 4 event types + parent rule + failure invariant + env-var config + Tests 1–3.

If even the hard floor slips, fallback talk plan: keep the chain-canonical / OTEL-projection slide, replace the live OTEL trace beat with a static screenshot of the design + "shipping next week" message. Per memory `feedback_forcing_functions_are_exceptions.md` — talks pull focus, but the strategic answer (chain canonical, projection one-way) doesn't require live OTEL on stage.

## Review process

Per [`project_review_process.md`](../../../...) (memory): code → non-draft PR → Copilot review → adversarial pass → fixes → merge on all-green.

Branch is `feat/f4-otel-emit-mvp`. Worktree: `/home/red/workspace/chitin-f4-otel`. PR opens to `main` after Tests 1–3 are green locally + manual integration test (Test 4) passes against a local otelcol.

## Success criteria

- [ ] All 4 event types produce valid OTLP/HTTP JSON spans
- [ ] Parent rule covers all 3 branches (within-chain, cross-chain, root)
- [ ] Endpoint refused → kernel commit still succeeds, function returns nil
- [ ] Local otelcol-contrib receives spans from a real chitin-instrumented Claude Code session
- [ ] No regressions in existing `emit_test.go` (unrelated tests pass)
- [ ] Demo runbook updated with the OTEL beat steps
- [ ] PR merged to `main` before 2026-05-06 (talk minus 1 day)
