# Local 24/7 Worker — Design Addendum (three-plane reframe)

**Date:** 2026-04-30 (same day as parent spec).
**Status:** addendum to `2026-04-30-local-worker-design.md`. Supersedes the openclaw-as-orchestrator framing and the chitin-owned task queue. Invariants, bootstrap rules, observability loop, and acceptance criteria from the parent spec stand.
**Trigger:** in-session strategic input identifying that the parent spec stretches openclaw into orchestration (workflow durability, queueing, scheduling) — the failure mode `project_hermes_killed_chitin_as_governance.md` already records ("don't let chitin own the tick-loop") applied recursively: *don't let openclaw own it either*.

---

## What this addendum locks

The parent spec's worker loop lived inside openclaw. That stretches openclaw beyond its lane (execution substrate / agent runtime). Workflow durability, retries, queue semantics, and scheduling belong in a control plane *above* openclaw, not inside it.

The three-plane decomposition:

```
┌─────────────────────────────────────────────────────────────┐
│  CONTROL PLANE                                              │
│    Temporal (local, Postgres-backed via docker-compose)     │
│    owns: durable workflows, queue, retries, scheduling,     │
│           workflow_id / run_id, cross-task budget rollup    │
└─────────────────────────┬───────────────────────────────────┘
                          │ workflow.execute_activity(execution_request)
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  EXECUTION PLANE                                            │
│    OpenClaw (acpx spawn of Claude Code or local-coder)      │
│    owns: agent turn, tool dispatch, session lifecycle       │
└─────────────────────────┬───────────────────────────────────┘
                          │ PreToolUse hook → chitin-kernel gate evaluate
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  ENFORCEMENT PLANE                                          │
│    Chitin (gov.Gate, --worker-mode, bootstrap rules,        │
│            event chain, OTEL projection, decisions stream)  │
│    owns: action authorization, audit truth, replay          │
└─────────────────────────────────────────────────────────────┘
```

Invariant restated: **Temporal schedules. OpenClaw executes. Drivers (Claude Code / Copilot / local) decide proposed actions. Chitin authorizes. Event chain records truth. OTEL is projection.**

## What this changes in the parent spec

| Parent-spec component | Status under three-plane |
|---|---|
| Worker-loop plugin (openclaw) | **Superseded.** Loop moves to a Temporal worker process. Open Question #3 (where worker plugin code lives) dissolves: it's not an openclaw plugin. |
| Task queue (chitin) at `~/.chitin/worker-queue.jsonl` + CLI claim/complete | **Superseded as a queue.** Temporal owns queue semantics. Chitin owns the *task contract* (`execution_request`, schema below) and a *submission* CLI that posts to Temporal. |
| Model routing per task via `--model` at acpx spawn | **Stands**, but classification → driver+model mapping is now a Temporal activity input (`execution_request.allowed_drivers`), evaluated by chitin before the activity dispatches. |
| Worktree provisioning, draft-PR-only output, bootstrap rules, `worker-mode` gate flag, `worker_task_id` end-to-end | **All stand unchanged.** `worker_task_id` becomes Temporal's `workflow_id` (or `run_id` — TBD in slice 1). |
| Invariants 1–5 of parent spec | **All stand unchanged.** Plane-orthogonal. |
| Observability loop (chain, OTEL projection, decisions stream consumes worker-tagged decisions) | **Stands.** Worker tag becomes `workflow_id`. |
| Acceptance criteria | **Stands**, with one rename: "openclaw worker plugin published" → "Temporal worker app deployed (`apps/runner/`)." |

## The `execution_request` contract

The wire-format between Temporal (control) and Chitin (enforcement) / OpenClaw (execution). Lives in `libs/contracts/` as zod, regenerated to Go.

```ts
// libs/contracts/src/execution-request.schema.ts (shipped — slice 1a)
export const ExecutionRequestSchema = z.object({
  schema_version: z.literal('1'),
  workflow_id:    TemporalIdSchema,            // Temporal workflow id
  run_id:         TemporalIdSchema,            // Temporal run id (per attempt)
  repo:           z.string().regex(/^[^/\s]+\/[^/\s]+$/),
  files:          z.array(z.string().min(1)).optional(),
  task_class:     z.enum(['refactor','test_writing','doc_update','bug_fix','scaffolding','exploration']),
  risk_level:     z.enum(['low','medium','high','irreversible']),
  // claude-code intentionally absent — Anthropic ToS forbids it as a worker driver
  // (see project_anthropic_tos_constraints.md). Interactive CLI / /schedule only.
  allowed_drivers: z.array(z.enum(['copilot','local-qwen','local-glm','local-deepseek'])).min(1),
  network_policy: z.enum(['none','allowlist','open']),
  write_policy:   z.enum(['none','worktree','branch','main']),
  bounds: z.object({
    max_tool_calls: z.number().int().positive(),
    max_cost_usd:   z.number().nonnegative(),
    wall_timeout_s: z.number().int().positive(),
  }),
  prompt: z.string().min(1),
});
```

Two enforcement points use this contract:

1. **Pre-activity gate** (DEFERRED — not in slice 1/2). Will validate the request via `chitin-kernel task validate <req.json>` and narrow `allowed_drivers` (policy can shrink, never expand) before Temporal dispatches the activity. Slice 1 ships zod-parse-only at the submit boundary (`apps/runner/src/submit.ts`); the kernel-side validate + narrow path is a future slice. Tracking item: implement `chitin-kernel task validate` and route submit through it.
2. **Per-tool-call gate (existing).** Inside the agent session, every tool call still passes `gov.Gate.Evaluate()` via the PreToolUse hook. The contract carried at session-start tags every gov-decision row with `workflow_id` so the analysis layer can group decisions by workflow.

## Locked sub-decisions (this session)

1. **SDK language for the Temporal worker: TypeScript.** Reasons: `libs/contracts` is zod-native (worker imports `ExecutionRequest` with zero codegen); openclaw's surface is TS/jiti so spawning agent turns is cheap; Go-side stays Go (gate, kernel, decisions). Tradeoff: two languages in the loop, but that's already true.
2. **Worker code location: `apps/runner/` in the chitin monorepo.** Pins contract version. Talks to the Go kernel via `chitin-kernel gate evaluate` subprocess (same contract Claude Code's hook uses). Spawns openclaw via `acpx`.
3. **Temporal runtime: docker-compose with Postgres-backed Temporal server.** `start-dev` SQLite path doesn't survive `docker system prune` and 24/7 means *survives reboots*. ~5min of infra setup buys real durability.
4. **First driver: Claude Code → ollama `qwen3-coder:30b` only.** Don't build the routing tier in v1; prove the loop with one driver, one model. Tier policy (P0/P1/P2/P3) is slice 2.
5. **Submission CLI goes through chitin, not directly to Temporal.** `chitin-kernel task submit` validates the `execution_request`, runs the pre-activity gate, then posts to Temporal. Keeps validation in the enforcement plane.

## Smallest provable slice (slice 1)

Goal: one workflow, one activity, one tool call, all four planes wired.

```
$ chitin-kernel task submit \
    --title "echo hello from worker" \
    --action-class read \
    --risk-level low \
    --allowed-drivers claude-code \
    --prompt "Use the Bash tool to run: echo hello-from-temporal-worker"

# chitin validates execution_request → posts to Temporal
# Temporal CLI shows running workflow
# apps/runner/ activity picks up
# acpx spawns claude-code with ANTHROPIC_BASE_URL=ollama, model=qwen3-coder:30b
# claude-code emits one Bash tool call
# chitin gate evaluates in worker-mode, allows
# tool call runs, result returns to activity
# workflow completes, gov-decision row written with workflow_id + run_id
```

What slice 1 explicitly does **not** include (deferred to slices 2+):

- Multiple drivers / tier routing
- Worktree provisioning + draft PR opening (slice 2)
- `--worker-mode` gate flag with full bootstrap rule set (slice 2)
- WorkflowGate at the orchestration boundary (still undesigned — see open question)
- Multi-worker concurrency (single 3090 → single worker)
- Heartbeat schema (parent spec Q4)
- Re-classification policy (parent spec Q5)
- AI-generated tasks (out of scope per parent spec)

## Open questions (new under three-plane)

1. **WorkflowGate.** The moment Temporal can retry an activity, "should this retry happen given the budget?" is a policy decision that doesn't belong in the per-tool-call gate. Needs a sibling design — `gov.WorkflowGate` or a new `evaluation_point: 'workflow_start' | 'activity_start' | 'tool_call'` field on `gov.Decision`. **Sibling design required before slice 2.**
2. **`workflow_id` vs `run_id` for the audit-row tag.** Run-id is per-attempt (changes on retry); workflow-id is stable across retries. Probably both — workflow_id for grouping, run_id for replay debugging. Decide before slice 1 ships.
3. **How does chitin's pre-activity validate map to a Temporal interceptor vs a wrapper CLI?** Wrapper CLI (`chitin-kernel task submit` posts) is simpler in v1; interceptor (Temporal SDK middleware) is cleaner long-term. Lean: wrapper CLI for slice 1, interceptor when slice 3 adds multi-tenant.
4. **Failure mode: Temporal server down on a reboot.** Docker-compose restart policy `unless-stopped` + chitin operator command `chitin-kernel worker doctor` to verify all four planes are up. Trivially solvable; flag for the runbook.

## Carry-forward from parent spec (unchanged)

- Invariants 1–5: gate authority, worktree isolation, bounded autonomy, single source of policy, output is review-gated.
- Cold-start safety bootstrap rules: `worker:no-trunk-write`, `worker:no-pr-merge`, `worker:no-recursive-delete`, `worker:no-network-egress-out-of-allowlist`, `worker:acpx-spawn-allowlist`. All apply when `worker-mode` is set on the gate.
- Observability loop: every event lands in the chain; gov-decisions accumulate with `workflow_id` tag; F4 OTEL emit projects everything.
- Spike evidence (lines 219–232 of parent spec): three primitives verified firing on real traffic — direct ollama at `:11434`, recursive-delete denial, envelope exhaustion. Three-plane reframe doesn't invalidate the spike; it relocates the orchestration component.

## Acceptance criteria (slice 1 only)

Slice 1 is "done shipping" when:

- [ ] `libs/contracts/src/execution-request.ts` exists; Go regeneration produces `go/execution-kernel/internal/contracts/execution_request.go`.
- [ ] `apps/runner/` exists with one workflow + one activity that consumes `ExecutionRequest`.
- [ ] `docker-compose.yml` for local Temporal + Postgres, `chitin-kernel worker doctor` reports green.
- [ ] `chitin-kernel task submit` validates and posts; `chitin-kernel task list` shows status from Temporal.
- [ ] One end-to-end task: submit → workflow → activity → claude-code (qwen3-coder:30b via ollama) → one Bash tool call → chitin gate → gov-decision row tagged with `workflow_id` + `run_id` → workflow completes.
- [ ] Survives `docker compose down && docker compose up` (workflow state persists; in-flight tasks resume).

Slices 2+ acceptance criteria carry over from the parent spec, scoped to their slice boundaries.

---

**Layer Contracts compliance check (per `docs/architecture/layer-contracts.md` v1):**

- *Kernel authority:* chitin remains the only policy authority on tool calls. Temporal does not gate; Temporal schedules. ✓
- *Driver constraint:* `allowed_drivers` is a policy output, not a workflow input the orchestrator can override. Temporal may *choose within*, never *expand*. ✓
- *Routing scope (capacity-only):* Temporal handles capacity (queue depth, retries, scheduling); does not make policy decisions. ✓
- *Aggregation role:* event chain canonical, OTEL projection. Temporal's own event history is a *third* projection (workflow-internal, not the audit truth). Chain remains source of truth for audit. ✓

The four-rule check passes. The reframe is plane-correct.
