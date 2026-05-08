# Cull the in-gate LLM advisor; scaffold `apps/advisor` as a chain consumer

**Date:** 2026-05-08
**Status:** Accepted
**Related:** `2026-05-08-cull-escalate-defer-to-hermes.md` (operator
approvals) — same lens, different vector.

## Context

`internal/router/advisor.go::CallAdvisor` spawned `claude -p` from
inside the kernel's PreToolUse hook whenever a router heuristic
crossed an "advisor-worthy" threshold (`blast_radius_above_threshold`
/ `floundering_detected` / `kernel_denied` / `plugin_fired`). Its
parsed `<<<ROUTER_ADVISOR>>>{...}` verdict was composed into the
deny / allow response back to Claude Code — i.e. the kernel was
synchronously waiting on an LLM subprocess inside the gate hot path.

The audit Tier 6 review through the Sun Tzu lens flagged this as
**symmetric drift** vs hermes' `approvals.mode: smart`:

| Capability chitin's in-gate advisor offered | Hermes (and every other substrate) already has |
|---|---|
| LLM second-opinion on dangerous tool calls | `approvals.mode: smart` runs an LLM-shaped check inline |
| Operator-prompt + reply parse | Native gateway flow (see 2026-05-08-cull-escalate-defer-to-hermes.md) |
| Kanban / chat surfacing of agent decisions | Hermes kanban + native chat surface |
| "Ask another model" composition | Every chain consumer can do this — it's not a kernel responsibility |

The kernel's asymmetric value is **signal computation**: blast-radius
scoring across all drivers from one canonical action vocabulary,
floundering detection over the chain envelope, drift detection
against declared intent. Those run nowhere else, by anyone. Spawning
an LLM in the gate just because we had the heuristic data was the
mistake — handing the data to whatever consumer wants it is the
right shape.

## Decision

1. **Delete `internal/router/advisor.go`** (the `claude -p` subprocess
   invocation + prompt builder + `<<<ROUTER_ADVISOR>>>` parser).
2. **Delete `internal/router/evaluate.go` + `evaluate_test.go`** (the
   "build an `AdvisorRequest` envelope so an external caller can run
   the advisor" pipeline — the external caller no longer exists in
   the kernel).
3. **Delete the `AdvisorRequest` / `AdvisorResponse` types** from
   `internal/router/types.go`. `AdvisorConfig` is retained as a
   parse-and-ignore stub so existing operator `chitin.yaml` files
   with a `router.advisor:` section continue to load cleanly.
4. **Edit `cmd/chitin-kernel/router_hook.go`** to remove the entire
   advisor-consult block (~140 LOC). The hook now:
   - Runs the kernel verdict via `evalHookStdin` (unchanged)
   - Runs the heuristics: blast-radius, floundering, drift, plugins
   - Pre-action plugin block short-circuits on `Block=true`
   - Stamps heuristic signal scores onto the chain via a router-
     stamped `gov.Decision` row (rule_id prefix
     `router-heuristic:`)
   - Returns the kernel verdict — never altered by heuristic
     scoring; kernel + plugin pre-block remain the only
     authoritative verdicts
5. **Extend `gov.Decision`** with four optional (`omitempty`) fields:
   `predicted_blast`, `floundering_score`, `drift_score`,
   `routing_decision`. Pre-router rows and router-disabled
   invocations keep the existing on-disk shape.
6. **Edit `cmd/chitin-kernel/simulate_cmd.go`** to drop the advisor
   call. `simulate` is now pure-Go, deterministic, and free of LLM
   latency. The `--no-advisor` flag is retained as a no-op for
   script backwards-compat.
7. **Scaffold `apps/advisor/`** as a chain-consumer app. README
   documents what it consumes, what it does, what it doesn't, and
   wiring shapes (hermes-cron-driven, standalone CLI, kanban-
   profile-spawned). The implementation is the operator's choice
   and lives outside the kernel.

## What stays in the kernel

The asymmetric pieces — only chitin can compute these from cross-
driver chain telemetry:

- `internal/router/blast_radius.go` — combined irreversibility/
  scope/visibility/counterparties score
- `internal/router/floundering.go` — loop / stall / denial-cascade
  detection
- `internal/router/drift.go` — out-of-scope writes vs declared
  intent
- `internal/router/route_for.go` + `routes.go` — heuristic routing
  decision (which peer driver/model to escalate to)

## What chain consumers do downstream

`apps/advisor/README.md` lists wiring shapes; the kernel's contract
is just: stamp the four signals on the row and let consumers
decide. Examples include filing a kanban ticket on
`floundering_score > 0.8`, asking another model on
`predicted_blast > 0.7`, paging the operator on `drift_score ==
0.8`, etc.

## Counterfactual

Had we cleaned the advisor before the operator-approval cull on
2026-05-08, the operator-approval pipeline (PRs #380–#396) would
likely never have shipped — the in-gate `claude -p` call is what
gave the impression that "calling an LLM from the gate" was a
sensible primitive, which made "calling the operator from the gate"
feel like the natural next step. Both were the same mistake at
different layers.

## Followups

- The kanban-driven advisor wiring shape (apps/advisor → hermes
  kanban) is the operator's first build-out target. Confirms the
  contract works end-to-end.
- The TS implementation in `apps/runner/src/router/` — historically
  the design substrate that informed the Go port — should be
  audited for the same cull. If `apps/runner` is also spawning
  `claude -p` in its router pipeline, the same Sun Tzu reasoning
  applies.
- `chitin-routes.yaml` peer-spawn machinery (`route_for.go` step 3
  onwards) was scoped to step 2 of the original kernel-gate-
  escalation design. Now that the LLM advisor is gone, the peer-
  spawn pathway becomes the primary "kernel suggests an action"
  channel — worth re-evaluating the deferred steps against the
  reduced surface.
