# Kernel-gate escalation: chitin as the in-tool-call router

Status: superseded by the 2026-05-08 cull.
Companion to / partial supersession of:
  - `2026-05-05-conformance-substrate.md` (the routing query API §148)
  - `apps/runner/src/dispatcher.ts` (the static TIER_DRIVER map)

Date: 2026-05-06

## Superseded note

This proposal is retained as historical context only. Its core shape
is no longer allowed: the kernel must not synchronously spawn peer CLIs
or run an LLM advisor inside the tool-call hot path. The accepted
post-cull shape is:

1. `chitin-kernel gate` produces the authoritative allow/deny decision.
2. `internal/router` computes pure-Go blast-radius, floundering, and
   drift signals, plus deterministic plugin pre-block checks.
3. The router stamps advisory `router.signal` decision rows onto the
   chain.
4. Hermes, OpenClaw, or an operator-wired chain consumer decides what
   to do with those signals.

See:

- `docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md`
- `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`
- `docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md`

## What this is, in one sentence

The chitin kernel sits in every tool call. So escalation isn't a
separate dispatch event — it's a **side effect inside the tool-call
gate**: when heuristics + advisor say "this needs a stronger model,"
the kernel synchronously spawns the next-tier CLI (claude-code,
copilot, etc.), captures its result, and returns that as the tool
call's response. The agent never knows it was escalated.

## Core invariant (operator decision, 2026-05-06)

> Kanban chooses work.
> Hermes attempts execution.
> Chitin decides whether Hermes is allowed to continue or must be
> replaced/escalated.
> Peer execution is a kernel-mediated substitution, not a dispatcher
> decision.

This is cleaner than v1 because routing, enforcement, telemetry, and
conformance all collapse into one authority boundary. Any future
behavior that violates the invariant — hermes self-escalating,
dispatcher picking peer CLIs, peer spawning peer — is a bug, not a
feature.

## Why the existing architecture missed this

`apps/runner/src/dispatcher.ts` has a static `TIER_DRIVER_DEFAULTS`
map: T0→openclaw, T2→copilot, T4→claude-code-headless. Each tier
maps to one driver. Escalation today means **chitin's runner
re-dispatches the whole task** at the next tier with a different CLI.
That's a coarse boundary — entire workflow restarts to switch model.

The kernel router (`go/execution-kernel/internal/router/`) already
runs heuristics (floundering, blast_radius, drift) + an advisor
(`claude -p` consulting opus) on every tool call. Today it just sets
`escalation_requested: true` and the downstream runner picks that up
on the next tick. The kernel could **synchronously act on the
escalation right there**, but it doesn't.

The user's insight: **chitin's gate IS the side-effect surface**.
A tool call comes in, the gate runs, the gate can do anything (deny,
allow, or **spawn a peer CLI and return its output as the result**).
This collapses three things I was treating as separate:

1. Tier-to-driver map (dispatcher.ts) → kernel escalation policy
2. Per-tier fallback chains → kernel "if X fails escalate to Y" rules
3. Capability-vector routing (`routeFor`) → kernel-side runtime decision

All three are the same function: *given a signal + current quota +
matrix data, pick which CLI to spawn for this tool call.*

## The architecture

```
Hermes kanban → spawn hermes (always, glm-flash)
       ↓
Hermes makes tool call
       ↓
chitin-router-hook intercepts
       ↓
Go kernel:
   1. deterministic policy check  (today)
   2. heuristics (floundering / blast_radius / drift)  (today)
   3. advisor (claude -p consultation) if heuristic fires  (today)
   4. If verdict=takeover OR escalate=true:
        a. Look up escalation policy in chitin.yaml
        b. Pick peer CLI via routeFor(signal, context, quota)  (NEW)
        c. Synchronously spawn peer CLI with tool-call context  (NEW)
        d. Capture peer's output  (NEW)
        e. Return peer's result as the tool call response  (NEW)
   5. Else → emit kernel verdict (allow / deny / nudge)  (today)
       ↓
Hermes receives tool result
       ↓
[hermes never knows it was escalated]
```

## Invariants

A few claims this design REQUIRES to hold:

1. **The gate is the only escalation point.** No other path
   (dispatcher tick, kanban poll, manual intervention) is needed for
   per-tool-call escalation. Cross-task escalation (whole workflow
   re-tries) remains the runner's job.
2. **Escalation is synchronous within the gate.** The hermes worker
   blocks on its tool call until the peer CLI returns. No async
   handoff, no queue, no resume token. Worst-case latency = peer
   CLI's wall time + small overhead.
3. **Peer CLI gets enough context to act.** The tool call payload +
   recent chain events + the heuristic that fired + the advisor's
   nudge are passed in the spawn. Peer's response is treated as the
   ground-truth tool result.
4. **One peer CLI per gate event.** No nested escalation. If the peer
   itself flounders, that's a future-tick concern, not a recursive
   in-gate problem.

## The escalation policy schema (chitin.yaml)

```yaml
escalation:
  # Map (signal, severity) → which CLI to spawn. Operator-extensible.
  # Each entry's `route` is the routeFor() target — a named
  # optimization category from the conformance-substrate doc:
  # cheap+stable / patch_quality / recovery / reasoning_depth / latency.
  rules:
    - signal: floundering
      severity: ">= 2 loops"
      route: patch_quality
      max_per_hour: 10                    # rate-limit per quota cap
    - signal: blast_radius
      severity: "> 25 files"
      route: reasoning_depth
      max_per_hour: 5
    - signal: drift
      severity: "advisor.verdict=takeover"
      route: reasoning_depth
      max_per_hour: 3

  # Named optimization categories → ordered list of (driver, model)
  # candidates. routeFor() walks this list filtering by quota
  # availability + benchmark score.
  routes:
    cheap+stable:
      - { driver: copilot, model: gpt-4.1 }            # x0 (free on Enterprise)
      - { driver: gemini, model: gemini-2.5-flash }    # 1/1500 daily
    patch_quality:
      - { driver: copilot, model: gpt-5.4 }            # 81.8% terminal-bench
      - { driver: claude, model: claude-opus-4-6 }     # Max sub absorbs
    reasoning_depth:
      - { driver: claude, model: claude-opus-4-7 }
      - { driver: copilot, model: gpt-5.5 }            # x7.5, last resort
    recovery:
      - { driver: copilot, model: claude-haiku-4.5 }   # x0.33
      - { driver: copilot, model: gpt-5.4-mini }
    latency:
      - { driver: copilot, model: gpt-4o-mini }        # FREE + fast
      - { driver: gemini, model: gemini-2.5-flash-lite }
```

The matrix data we've built (operator_cost_band + benchmark scores)
becomes the **default** when no operator override exists. Routes
walk the candidate list and pick the first one that fits remaining
quota. That's the runtime equivalent of "the matrix told us which
to use."

## The kernel side: routeFor()

```go
type RouteRequest struct {
    Signal       string             // "floundering" | "blast_radius" | "drift"
    Severity     string             // human-readable severity description
    ToolCall     ToolCallPayload
    ChainTail    []ChainEvent       // last N events for context
    QuotaState   QuotaSnapshot      // live remaining quota per driver
}

type RouteDecision struct {
    Driver      string
    Model       string
    Rationale   string             // one-line WHY this candidate won
    QuotaCost   QuotaImpact        // estimated impact on each affected sub
}

func RouteFor(req RouteRequest, policy EscalationPolicy) (RouteDecision, error)
```

The function:
1. Looks up the matching `escalation.rules` entry by signal + severity
2. Walks `escalation.routes[matched.route]` candidates in order
3. For each candidate, checks QuotaState — does it have headroom?
4. Returns the first candidate that fits, with rationale logged
5. If NONE fit (all quota exhausted), returns an error → kernel
   falls back to the heuristic-only deny verdict

QuotaState is populated by reading the operator_matrix.json
(refreshed per session) plus live polling for fast-changing quotas
(copilot Premium Interactions changes per call).

## In-gate spawn — what's passed to the peer CLI

Per-driver spawn templates (kernel-side):

- `claude-code-headless`: `claude -p --model <model> --print` with
  the tool call payload + chain tail piped via stdin as a structured
  prompt. Output captured from stdout.
- `copilot`: Copilot Chat API for code-shaped tools, `gh copilot
  suggest -t shell` for shell-shaped tools. Token from `gh auth token`.
- `codex`: `codex exec --model <model> -p '<prompt>'`. Output captured.
- `gemini`: `gemini -p '<prompt>' --model <model>`. Output captured.

**Working directory** (operator decision, 2026-05-06): peer always
runs in a **fresh worktree**, not the worker's dirty tree. Isolation
+ reproducibility + clear audit boundary. Snapshot-and-diff-apply
deferred — adds complexity too early.

**Recursive escalation guard** (operator decision, 2026-05-06): the
spawn env always carries `CHITIN_NO_ESCALATE=1`. Peer can be gated,
denied, logged, advised — but **cannot spawn another peer**. Without
this guard, peer-spawns-peer creates recursive arbitration with
unbounded cost and weird deadlocks.

Spawn timeout = `escalation.spawn_timeout_seconds` (default 60s).

## Peer output normalization — ToolCallResult, not raw stdout

(Operator design correction, 2026-05-06): "Peer's output IS the tool
call result" was right but only AFTER normalization. The kernel wraps
peer output as a structured `ToolCallResult` with full provenance, NOT
raw CLI stdout. This preserves replay, audit, and future scoring
(without it the conformance loop can't attribute outcomes back to the
peer that produced them).

```go
type ToolCallResult struct {
    // The actual content the worker (hermes) sees in place of its
    // original tool result. Shape matches whatever the original tool
    // call expected (string, JSON, file diff, etc.).
    Content        any

    // Provenance — required, not optional. Every escalated result
    // carries this so /mine + conformance extractors can join.
    Provenance struct {
        EscalationID    string         // unique per peer-spawn
        WorkerWorkflowID string        // hermes' workflow_id
        TriggerSignal   string         // "floundering" / "blast_radius" / "drift"
        Severity        string         // human-readable
        Route           string         // "patch_quality" etc.
        Candidate       struct {
            Driver string
            Model  string
        }
        SpawnedAt       time.Time
        DurationMs      int64
        PeerExitCode    int
        PeerQuotaImpact QuotaImpact    // what this cost on which sub
    }

    // Raw peer output for replay — never shown to the worker, but
    // kept in the chain so audits can re-derive Content.
    RawPeerStdout   string
    RawPeerStderr   string
}
```

Telemetry: every peer spawn writes ONE chain event of type
`peer_escalation` carrying the full Provenance + RawPeer* fields.
The worker's chain only sees the normalized Content. Two-layer audit
trail: worker's view of "what happened in this tool call" + chain's
view of "what really happened underneath."

## Migration path (operator-approved sequence, 2026-05-06)

Six incremental, reversible steps. Each independently shippable.

1. **Schema only** — add `escalation:` block to chitin.yaml schema +
   loader. No behavior change. Operators can declare policies;
   nothing reads them yet.

2. **routeFor(signal, severity, context) only** — Go kernel adds the
   function + policy walker; reachable from tests but not wired into
   the gate. Returns RouteDecision struct purely; no spawn yet.

3. **spawnPeer(route) behind flag** — kernel function that ACTUALLY
   spawns the peer CLI (fresh worktree, CHITIN_NO_ESCALATE=1 env,
   per-driver template) and normalizes its output into ToolCallResult.
   Reachable from tests but no gate path calls it. Default flag off.

4. **Wire in-gate escalation path** — `escalation.enabled: true` in
   chitin.yaml turns it on; default off. The gate's existing
   "heuristic + advisor → escalation_requested" branch now also
   calls `routeFor()` + `spawnPeer()` and returns the normalized
   result. Operator opt-in to test.

5. **Remove dispatcher CLI selection** — once in-gate is proven,
   delete `TIER_DRIVER_DEFAULTS` + `pickTierDriver` + the
   `CHITIN_TIER_DRIVER_T<N>` env overrides from
   `apps/runner/src/dispatcher.ts`. Dispatcher trims to "always spawn
   hermes." Tier stays as a card metadata field — kernel-side hint,
   not CLI selector.

6. **Conformance feedback loop** — the routing-effectiveness
   dimension (shipped in `2026-05-05-conformance-substrate.md` §161)
   gives routeFor() observed-vs-declared compatibility per (driver,
   model). RouteDecision starts factoring observed data, not just
   leaderboard scores. Closes the loop: matrix → routing → measure →
   re-update matrix.

Steps 1-3 are pure additions. Step 4 is opt-in. Step 5 removes code.
Step 6 is the conformance flywheel.

## What the dispatcher.ts looks like after migration

```typescript
// Before: pick driver per tier, spawn that CLI
const driver = TIER_DRIVER[entry.tier];
const result = await spawnDriver(driver, entry.tier, model, ...);

// After: always spawn hermes; kernel handles in-tool-call escalation
const result = await spawnHermes(entry, ...);
// Hermes runs glm-flash, makes tool calls, kernel transparently
// escalates per chitin.yaml policy. result.escalation_log captures
// which peer CLIs the kernel invoked during the run.
```

`TIER_DRIVER_DEFAULTS`, `pickTierDriver`, `CHITIN_TIER_DRIVER_T<N>`
env overrides — all removed. The `tier` field on backlog cards
becomes a hint to the kernel ("this card is bulk T0 work, prefer
cheaper escalation routes") not a driver-selection key.

## Decisions locked (operator, 2026-05-06)

These were the open questions from the first draft. All resolved
before any code lands.

1. **Peer working directory: fresh worktree.** Best default. Don't
   run peer in worker's dirty tree. Snapshot+diff is elegant but adds
   complexity too early. Fresh worktree gives isolation,
   reproducibility, and clearer audit boundaries.

2. **Cost attribution: worker workflow_id + child escalation_id.**
   Parent workflow owns the burn; peer needs `escalation_id`,
   `route`, `candidate`, `trigger_signal` for analysis. Without this
   the conformance feedback gets muddy. Implemented as the
   ToolCallResult.Provenance struct above.

3. **Recursive escalation guard: CHITIN_NO_ESCALATE=1 mandatory.**
   Peer can still be gated, denied, logged, advised — but cannot
   spawn another peer. Otherwise: recursive agent arbitration with
   unbounded cost and weird deadlocks.

4. **Hermes advisor under chitin: disabled / non-authoritative.**
   Chitin is the single escalation authority. Hermes can emit local
   hints (telemetry, flagging) but cannot independently escalate,
   reroute, or mutate policy decisions. Otherwise multi-authority
   ambiguity reintroduced.

## Out of scope

- **Cross-task escalation** (whole workflow re-tries because T0
  repeatedly flounders) stays in the dispatcher. In-gate covers
  per-tool-call; dispatcher covers per-task.
- **Peer-to-peer escalation** (peer CLI spawns another peer). Per
  invariant 4: one level only.
- **Async escalation** (kernel queues the work, returns "pending,"
  worker continues other tool calls, peer result lands later).
  Adds significant complexity; defer.
- **Per-task-class escalation policies.** All cards share the same
  `escalation.rules` initially. Per-class overrides (e.g., refactor
  cards prefer patch_quality, exploration cards prefer
  reasoning_depth) is a phase-2 schema extension.

## Why this is the right shape

It collapses everything I've been chasing for two days — tier
fallback chains, capability-vector routing, budget-aware downshift,
matrix-driven driver selection — into ONE place: the kernel's
escalation policy + routeFor(). And it does it at the only
substrate that already sees every tool call, has every chain event,
knows every quota state, runs heuristics + advisor, and has a
gate-shaped contract for "synchronously alter what the worker
sees." That's chitin's actual moat — the in-tool-call gate as the
universal router. Nothing else has that vantage.
