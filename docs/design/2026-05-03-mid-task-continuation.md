# Mid-task continuation — design proposal

Status: design only. This PR ships the kernel-side wire (the `escalation_requested` flag in the chain envelope + router-hook telemetry). The Temporal workflow loop that consumes it is intentionally deferred — the architectural commit is large enough that it warrants discussion before code.

Date: 2026-05-03
Driving need: when the router/advisor decides "this action shape exceeds the current driver's competence, escalate to a higher tier", the kernel should hand the in-flight task off to a higher-tier worker rather than killing the session and surfacing a human-pickup nudge.

## What "mid-task continuation" means

Today's flow on advisor-takeover-with-escalate:

```
[T1 copilot] → tool call N → router hook → advisor: takeover, escalate=true
                                            ↓
                               kernel writes decision=block + escalate=true
                                            ↓
                               copilot stops, surfaces deny to operator
                                            ↓
                          (next dispatcher tick re-spawns at T2)
```

What we want:

```
[T1 copilot] → tool call N → router hook → advisor: takeover, escalate=true
                                            ↓
                               kernel writes decision=block + escalate=true
                                            ↓
                               copilot stops, activity returns
                                            ↓
                       Temporal workflow sees escalation_requested
                                            ↓
                continueAsNew(executeRequestWorkflow, req with tier=T2,
                                                        escalation_context=...)
                                            ↓
                            [T2 claude-code] picks up with full prior context
```

## Today's gaps (post-this-PR)

The audit (`docs/observations/2026-05-03-mcp-gate-coverage-audit.md` companion entry) found:

1. **Router has the decision, but it's dead.** Pre-this-PR, `AdvisorResponse.Escalate bool` (`internal/router/types.go:67`) was set by the advisor and never read. THIS PR wires it through to the chain envelope (`escalation_requested: true`) and to router-hook telemetry — so any consumer can react.

2. **Activity has no loop.** `apps/runner/src/workflow.ts:20-22` — `executeRequestWorkflow` is a stateless proxy. It does NOT loop on tier; it does NOT inspect activity output for escalation markers; it just returns whatever the activity returns.

3. **ExecutionRequest has no escalation_context field.** `libs/contracts/src/execution-request.schema.ts` carries prompt + tier + bounds but no "prior driver tried this; here's what they found" channel. Without this, T2 starts cold.

4. **No tier-bump policy in workflow.** When does escalation stop? Cap at T4? Cap on cost? Cap on attempts? Today's only stopper is the `MAX_AGENT_ATTEMPTS` envelope cap (cross-workflow), not a per-task limit.

## Proposed implementation (next PR, not this one)

### Step 1: ExecutionRequest carries escalation context

Add to `libs/contracts/src/execution-request.schema.ts`:

```typescript
escalation_context: z
  .object({
    from_tier: z.string(),
    from_driver: z.string(),
    advisor_nudge: z.string(),
    prior_chain_session_id: z.string().optional(),
    attempt: z.number().int().min(1),
  })
  .optional(),
```

The activity passes this to the higher-tier driver via prompt prefix or environment variable so the new driver knows "here's what the prior driver was attempting + why we escalated".

### Step 2: Activity detects the escalation marker

`apps/runner/src/activity.ts` parses kernel chain output. When the LAST decision event has `escalation_requested: true`, the activity returns:

```typescript
return {
  ...activityResult,
  escalation_requested: { from_tier: req.tier, advisor_nudge: nudge, ... },
};
```

ActivityResult schema gets a new optional field.

### Step 3: Workflow loops on escalation

```typescript
export async function executeRequestWorkflow(req: ExecutionRequest): Promise<ActivityResult> {
  const result = await runAgentTurn(req);
  if (result.escalation_requested && shouldContinue(req)) {
    const next = bumpTier(req, result.escalation_requested);
    return continueAsNew<typeof executeRequestWorkflow>(next);
  }
  return result;
}
```

`shouldContinue` enforces caps: max-attempts (3), tier ceiling (T4), wallclock cap (5min). On cap-hit, return the takeover result + surface to dispatcher for the existing post-hoc tier ladder.

### Step 4: Dispatcher reads `escalated_to` for accounting

The dispatcher's existing tier-ladder (`dispatcher.ts:313-330`) currently observes "implementor exited with 0 commits → bump tier". With this change, it observes "implementor escalated mid-task to T+N → record in attempt log". This is metadata only — the actual tier-bump happened inside the workflow's `continueAsNew`.

## Why `continueAsNew` over signals

This worker has zero existing signal usage; introducing signals just for escalation drags in `signalWithStart`, signal-handler boilerplate, and a polling pattern that doesn't fit Temporal's history-replay model. `continueAsNew` is the idiomatic primitive for "same logical workflow, fresh execution context" and Temporal's tooling (Web UI, history, retries) handles it natively.

## Why `continueAsNew` over child workflow

Sibling child workflows (option A in the audit) lose context: the parent must re-marshal everything. `continueAsNew` keeps the workflow ID stable, which simplifies dispatcher accounting + chain correlation.

## What this PR ships

- Kernel side: `escalation_requested: true` in the chain envelope when advisor sets `Escalate=true` AND verdict is takeover OR continue
- Router-hook telemetry: `escalate` bool added to per-event JSON line
- Tests: 2 new (telemetry shape + envelope shape)

## What this PR does NOT ship

- ExecutionRequest schema change (Step 1)
- Activity result shape change (Step 2)
- Workflow loop (Step 3)
- Dispatcher escalated_to recording (Step 4)

These are deferred to a follow-up PR after design discussion. Building them speculatively without that discussion risks the same fate as the original `Escalate bool` — a field defined and never consumed.

## Open questions for the discussion

1. **Cap policy.** Is max-attempts=3 right? Should the cap be per-cost ($2 wallclock cap) instead of per-count?
2. **Prompt vs env carrier.** Pass `escalation_context` via prompt prefix (visible to model) or env var (mechanical, hidden)? Probably both — visible prefix for the model to read, env var for mechanical context the activity wraps around.
3. **Chain session ID continuity.** Should the higher-tier session inherit the same `session_id` so `chain replay` shows one timeline? Or new session_id with `parent_session_id` link? The latter is simpler; the former produces better operator UX.
4. **Failure modes.** What if T4 also requests escalation? Today: dispatcher's existing tier-cap rule (return result, mark entry escalation-exhausted, human pickup). New design should preserve this fallback.
5. **Cost accounting.** Each tier-bump costs more. Does the budget cap (`chitin-budget`) need to know "this attempt is a continuation, sum it under the original entry's bucket"?
