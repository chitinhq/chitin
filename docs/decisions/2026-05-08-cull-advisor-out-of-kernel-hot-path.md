# Cull the in-gate LLM advisor out of chitin's kernel hot path

**Date:** 2026-05-08
**Status:** Accepted
**Related:** `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`
(same lens, prior tier of the same audit pass)

## Context

`internal/router/advisor.go::CallAdvisor` shelled out to `claude -p`
from inside `cmd/chitin-kernel/router_hook.go`'s PreToolUse evaluator.
It produced a structured `{verdict, nudge, escalate}` response that the
hook composed into the kernel's deny/allow envelope.

The Sun Tzu lens audit on 2026-05-08 flagged this as **symmetric drift
vs hermes' built-in `approvals.mode: smart`**. Hermes' smart mode is
also an aux-LLM risk classifier with the same shape — it consumes a
proposed action, returns a "ask the human / proceed / block" verdict,
and feeds the result back to the gating loop. The earlier 2026-05-08
audit fix (commit `4a8b59e`-adjacent) added a long disclaimer comment
to `advisor.go` arguing the asymmetry was deliberate ("chitin's advisor
runs from the cross-driver canonical action vocabulary; smart mode owns
the chat surface"). Operator's read on review:

> "Don't waste time building things that already exist and are
> maintained. The disclaimer is the giveaway — if I have to defend the
> shape that hard, I'm building a parallel implementation."

Same lesson as PR #399 (Phase 3 escalate cull): the gate-deny path
already routes to hermes' approval system for hermes-driven tools, and
operator-wired chain consumers can do the same for non-hermes drivers.
Chitin's job is the asymmetric signal computation that nothing else
ships, not running an LLM in the gate hot path.

## What's gone

Deleted:

- `go/execution-kernel/internal/router/advisor.go` — `CallAdvisor`,
  `buildAdvisorPrompt`, `parseAdvisorOutput`. The `claude -p`
  subprocess + the `<<<ROUTER_ADVISOR>>>` marker parser.
- `go/execution-kernel/internal/router/evaluate.go` and
  `evaluate_test.go` — `EvaluateHeuristics` + `EvaluateResult`. These
  built the `AdvisorRequest` envelope; nothing in the kernel consumes
  it after the cull.
- `AdvisorRequest` and `AdvisorResponse` types from
  `internal/router/types.go`.

Edited:

- `cmd/chitin-kernel/router_hook.go` — the entire advisor-consult flow
  is gone: the `wantAdvisor` evaluation loop walking
  `policy.Advisor.When`, the `router.CallAdvisor` invocation, the
  `if advice.Verdict == "takeover"` deny-composition path, the
  `escalation_requested` field stamping, the kernel-allow nudge
  emission via `hookSpecificOutput`. The router_hook is now: kernel
  verdict + heuristics + plugin pre-block + signal stamping. ~150 LOC
  removed from this file.
- `cmd/chitin-kernel/simulate_cmd.go` — the `--advisor-call` branch
  removed; `--no-advisor` flag kept as a documented no-op so operator
  scripts that pass it keep working.
- `cmd/chitin-kernel/router_hook_test.go` — replaced the
  `TestWriteRouterTelemetry_EscalateFlagThrough` and
  `TestTakeoverEnvelope_CarriesEscalationRequested` tests (which pinned
  fields the cull deletes) with `TestWriteRouterTelemetry_StableSchema`
  (asserts the escalate field is now absent) and
  `TestHasNonZeroSignal_BoundaryCases` (pins the predicate that decides
  whether to stamp a signal row).

## What stays in the kernel

The asymmetric signal-computation path — only chitin can compute these
from cross-driver chain telemetry:

- `internal/router/blast_radius.go` — four-axis blast-radius score from
  the proposed action shape.
- `internal/router/floundering.go` — loop / stall / denial-cascade
  detection over the chain tail.
- `internal/router/drift.go` — out-of-scope-write detection vs declared
  intent. Now wired into the router_hook (was implemented but unused
  before the cull).
- `internal/router/route_for.go` and `routes.go` — the heuristic
  ROUTING decision engine + its `chitin-routes.yaml` schema. Pure
  function from (signal, severity, policy) → candidate. No subprocess.
- The plugin runner (`plugin_runner.go`, `plugins/`) — operator-
  declared subprocess heuristics. Pre-action `block=true` plugins
  remain authoritative deny-now signals; they are deterministic
  checks, not judgment calls.

## New behavior: heuristic signals stamped on the chain

`gov.Decision` (in `internal/gov/policy.go`) gains four optional
`omitempty` fields, serialized inline by `WriteLog`:

```go
PredictedBlast   float64 `json:"predicted_blast,omitempty"`
FlounderingScore float64 `json:"floundering_score,omitempty"`
DriftScore       float64 `json:"drift_score,omitempty"`
RoutingDecision  string  `json:"routing_decision,omitempty"`
```

The router_hook stamps a SECOND `gov.Decision` row per tool call when
at least one signal is non-zero (`hasNonZeroSignal`):

- `RuleID`: `router-heuristic:<allow|deny|pre-action-block:<plugin>>`
- `Mode`: `monitor` (advisory; the kernel's preceding row is the
  authoritative verdict)
- `Action.Type`: `router.signal`
- `Action.Target`: `<tool_name>:<file_path|notebook_path|command-truncated>`
- The four signal fields populated from the heuristic outcomes

Chain consumers join the stamping row with the kernel's enforcement
row via `(ts, action_target)`. Pre-router rows keep the existing
on-disk shape — all four new fields are `omitempty`.

Sub-threshold scores ARE stamped (the `hasNonZeroSignal` predicate is
"any score > 0", not "any score >= threshold"). The training signal
for tuning thresholds is in the sub-threshold band.

## Where LLM consultation lives now

- **Hermes-driven tools:** Hermes' `approvals.mode: smart` in
  `~/.hermes/config.yaml`. Hermes runs the aux-LLM risk classifier
  inside its own loop, surfaces the approval prompt natively, and
  blocks/resumes the tool call inline. Chitin's deny lands first
  (hermes calls chitin's gate via `pre_tool_call`); if chitin allows
  + the heuristic signal is non-zero, hermes' smart mode reads the
  stamped scores off the chain and decides whether to prompt.
- **Non-hermes drivers (Claude Code, Codex, Gemini):** operator-wired
  chain consumer. Read `~/.chitin/gov-decisions-<utc-date>.jsonl`,
  filter for `rule_id` prefix `router-heuristic:`, decide what to do
  with the signals (cron a follow-up review, post to a kanban,
  pipeline into an LLM second-opinion if you want one). Chitin
  doesn't ship one — the operator's call, not chitin's job.

The signal IS the deliverable. The reaction is downstream.

## Counterfactual

If we had checked hermes' approvals system before authoring the
in-gate advisor in the first place, we wouldn't have built it. Same
lesson as the escalate cull on 2026-05-08:

> when the operator says "I want a new feature," check if the
> substrate already provides it before designing a parallel
> implementation. The signal chitin chases — universal-tool-call
> interception, cross-driver canonical actions, audit chain — is
> unique. The signal it duplicated — "ask another model whether this
> looks off" — is not.

The disclaimer comment on `advisor.go` was the tell. Defending a
shape that hard means it isn't pulling its weight as differentiation.

## Net change

- **-425 LOC** (3 files deleted, 6 files edited)
- 0 new dependencies
- All tests green (`go test ./...` 963 passed in 26 packages)
- Build clean (`go build ./...`, `go vet ./...`)

## Follow-up: stub removal (2026-05-13)

The "parse-and-ignore" `AdvisorConfig` stub kept on 2026-05-08 (for
backwards-compat with chitin.yaml files that still had a
`router.advisor:` block) is removed in this follow-up:

- `AdvisorConfig` struct deleted from `internal/router/types.go`.
- `Policy.Advisor` field deleted; `DefaultPolicy()` no longer
  populates it.
- The `section == "advisor"` parser branch in
  `internal/router/policy.go` is replaced with a silent ignore — an
  old chitin.yaml that still has the block continues to load (the
  block's keys are read but discarded), but no `Advisor*` symbol
  exists in the kernel anymore.
- The `router.advisor:` block in `chitin.yaml` is removed by the
  operator (governance-self-modification rule prevents the kernel
  from rewriting its own gate config).

Rationale: the stub's purpose was a graceful migration window. A
week later, every operator-controlled chitin.yaml has been audited;
the stub is now just confusing residue that suggests an active
advisor path. Removing it makes the architecture honest: chitin's
router emits signals, downstream consumers handle LLM second
opinions, full stop.

Net change (this follow-up):
- ~50 LOC removed across `types.go`, `policy.go`, `chitin.yaml`
- Tests still green
- `--no-advisor` flag on `simulate_cmd.go` kept as a documented
  no-op (unchanged from the 2026-05-08 cull)
- Historical breadcrumb comments preserved in `router_hook.go`,
  `simulate_cmd.go`, `gov/{decision,policy}.go`, and the test
  sentinels in `router_hook_test.go` — those are tripwires for
  "the advisor path crept back in," not residue
