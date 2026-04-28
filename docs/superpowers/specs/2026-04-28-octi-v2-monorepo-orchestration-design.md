# Octi v2 — Monorepo Orchestration Layer Design

**Date:** 2026-04-28
**Status:** Design. Captures first-principles questions and lessons-learned. Ready for user review, then handoff to writing-plans for an execution plan AFTER 2026-05-07 talk + Phase F completion.
**Predecessor (to be archived):** `github.com/chitinhq/octi` (octi v1 — standalone Go binary + Redis, last push 2026-04-17).
**Related architectural memory:** Two-driver pattern (open vs closed vendor); chitin-as-governance-around-openclaw-and-claude-code; aggregate→policy→ecosystem→cloud strategic ordering.
**Related specs:** `2026-04-22-chitin-governance-v1-design.md` (the gov.Gate this orchestrator will call), `2026-04-28-claude-code-hook-driver-design.md` (one of the drivers it will dispatch to).

## Preamble

octi v1 was created 2026-03-29 as the swarm coordination layer for AI agent fleets — admission control, cost-cascade routing (Local Ollama → GH Actions → Subscription → CLI → API per-token), per-agent budget tracking, kanban with backpressure, and a five-stage pipeline. It is a single Go binary backed by Redis, sitting in its own repository (`chitinhq/octi`). It works today; last push was 2026-04-17.

Five things have happened since octi v1's design that materially change what the orchestrator needs to be:

1. **2026-04-22: chitin governance v1** shipped (`2026-04-22-chitin-governance-v1-design.md`, PR #20 merged 2026-04-20). The `gov.Gate.Evaluate(action, agent) → Decision` API now exists in the chitin Go kernel. octi v1 was designed without this primitive; v2 should consume it directly.
2. **2026-04-23: hermes was killed** as a runtime. Chitin's stated direction became "governance around openclaw + claude code." Octi v1 was designed when hermes was still the autonomous-runner assumption; the driver landscape has changed.
3. **2026-04-23–28: openclaw was confirmed as the autonomous driver** (already drives Copilot CLI via acpx since 2026-03-09; already first OTEL GenAI consumer; already on multica's supported-agent list). Octi v1 lists openclaw as one of many; v2 can plan around it as the primary autonomous driver.
4. **2026-04-25: Copilot CLI governance v1 (PR #51)** built a chitin-native, Go SDK-embedded driver for Copilot CLI. This is a third driver shape (in-process Go extension), distinct from the openclaw wrapper and the upcoming Claude Code hook.
5. **2026-04-28: Claude Code hook driver spec** (`2026-04-28-claude-code-hook-driver-design.md`) named harness-level hooks as a third architecture pattern beyond the two-driver (open / closed vendor) frame. Octi v1 was designed with two driver shapes in mind, not three.

These five points are the genuinely-new context that justifies a clean-room. Without them, the right move would be to refactor octi where it sits. With them, the right move is to bake octi into the chitin monorepo where it can share `gov.Gate`, the chitin event chain, and the canonical `Action` vocabulary.

The cost of clean-roomming is real. The "bias toward substrates, not full-stack rebuilds" memory exists because clawta → hermes → octi is the third such rewrite in this lineage. v2 must justify itself by *what it does that v1 cannot reasonably be patched to do*, not by aesthetics.

## One-sentence invariant

Every task that crosses the chitin governance boundary lands on exactly one driver, picked by a deterministic policy that considers `(action_class, capacity, cost, vendor_constraints)` — and the choice itself is logged to the chitin event chain as a `route_decision` event so that "why did this task run on the cloud and not locally" is auditable, replayable, and policy-revertible.

## Scope

### In scope

- **`go/orchestrator/` package** inside the chitin monorepo (sibling to `go/execution-kernel/`). New module path `github.com/chitinhq/chitin/go/orchestrator/...`. Reuses chitin's existing `event`, `chain`, `gov`, `kstate`, `emit` packages.
- **Library-first, daemon-second.** Core orchestration logic ships as a Go library importable by any process (CLI, cron job, long-running daemon, embedded in another tool). The reference daemon is a thin wrapper around the library; not the only deployment shape.
- **`gov.Gate.Route(task, capacity) → driver`** as a sibling to `gov.Gate.Evaluate(action, agent) → Decision`. Same code shape, same policy file, same audit trail. Cost-cascade routing becomes a policy primitive, not a separate subsystem.
- **State unification.** Kanban transitions (`accepted`, `dispatched`, `running`, `completed`, `failed`) emit chitin events. No Redis. Replay = replay chitin's existing event log.
- **Driver-agnostic task envelope.** Orchestrator hands a typed `Task` envelope to a driver and receives a typed `Result` envelope back. Orchestrator does not know how to call ollama, Anthropic, or GitHub.
- **Three driver-shape adapters in v1**: openclaw (autonomous wrapper), Claude Code hook (post-talk), Copilot CLI (PR #51). Each driver registers its capability advertisement (model classes supported, current capacity, cost per call class) on a chitin-event-stream channel; orchestrator's router consumes it.
- **Capacity model** is observable, not internal: every driver self-reports `health/capacity` to the chitin event stream on a heartbeat. Router reads recent events; no Redis-backed in-memory state to keep consistent. Slower than v1's Redis approach; orders of magnitude simpler.
- **Cost-cascade policy** as YAML rules driven by `(task_class, model_fit_minimum, deadline)`. Default cascade: `ollama_local → ollama_cloud → anthropic_haiku → anthropic_sonnet → github_copilot → human_review`. Policy can override per task class or per-action.
- **Single-host, solo-operator default.** Multi-machine support deferred — first dogfood is "Jared on his 3090 + cloud subs."
- **Tombstone for octi v1.** `chitinhq/octi` README updated to point at the chitin monorepo. Existing config-format ideas captured before archival.

### Out of scope

- **Multi-host coordination / distributed scheduler.** v1 single-host. v2 candidate — but not the v1 problem.
- **Web UI.** Operator interacts via CLI + chitin's existing event tooling. UIs are option-value, not load-bearing.
- **Workflow definition layer.** Orchestrator dispatches one-shot tasks. Multi-step workflows belong above this layer (Archon, LangGraph, or DIY) — out of v1.
- **Goose / OpenCode / Mastra integration.** Open-vendor agent frameworks — option-value escape hatches if the existing two drivers prove insufficient. Not in v1's driver set.
- **Multica integration.** Multica is a team coordination layer; solo-operator default doesn't need it. Re-evaluate if the operator turns into multiple operators.
- **Auto-scaling / spot capacity / GPU sharing.** v1 assumes a fixed set of providers. Elastic capacity is post-Phase-F.
- **Cross-task dependencies (DAGs).** v1 is a flat queue of independent tasks. DAG support belongs in the workflow layer above, not the dispatcher.
- **Rate-limit-aware backpressure.** v1 uses naive "if capacity unavailable, defer" semantics. Smarter rate-limit-respecting concurrency is a v2 refinement once we have real load data.
- **Readybench/bench-devs content.** Chitin is OSS; content boundary rule applies. Octi v2 ships only OSS-suitable workloads.

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│  chitin monorepo                                                      │
│                                                                       │
│  ┌────────────────────┐   route(task)   ┌────────────────────────┐    │
│  │  orchestrator      │────────────────▶│  gov.Gate              │    │
│  │  (library + thin   │◀────────────────│  .Route(task,capacity) │    │
│  │   daemon)          │   driver pick   │  .Evaluate(action,id)  │    │
│  └────────┬───────────┘                 └────────────────────────┘    │
│           │                                       │                   │
│           │ dispatch                              │ event emit        │
│           ▼                                       ▼                   │
│  ┌──────────────────────────────────────────────────────────┐         │
│  │  driver registry (in-process Go interface)               │         │
│  │                                                          │         │
│  │   ┌──────────────┐ ┌──────────────┐ ┌──────────────┐     │         │
│  │   │ openclaw     │ │ claude-code  │ │ copilot-cli  │     │         │
│  │   │ adapter      │ │ hook driver  │ │ (PR #51)     │     │         │
│  │   └──────┬───────┘ └──────┬───────┘ └──────┬───────┘     │         │
│  └──────────┼─────────────────┼────────────────┼────────────┘         │
│             ▼                 ▼                ▼                      │
│       ollama (3090,        Anthropic      GitHub Copilot              │
│        cloud)              API            (subscription)              │
└──────────────────────────────────────────────────────────────────────┘
        │
        │ event emit (every action, every route, every result)
        ▼
   ~/.chitin/events-<run_id>.jsonl   ←   single canonical timeline
```

Two pieces are load-bearing:
- **`gov.Gate.Route` is the router.** Routing is policy. Same primitive that decides allow/deny decides which driver gets the call.
- **The chitin event chain is the only state store.** Kanban state, capacity advertisements, route decisions, results — all emit events. Replay rebuilds state. No Redis, no separate store.

## First-principles questions to resolve before plan

These are the live design tensions. The plan must close each one with a concrete commitment.

### Q1: Server or library?

**Tension:** v1 is a long-running daemon. Daemon-shape binds to a deployment (start it, monitor it, restart it on crash, run Redis alongside). Library-shape lets the same logic embed in cron, CI, IDE plugins, etc.

**Recommendation:** Library-first. The reference deployment is `chitin-kernel orchestrator run` (a thin daemon wrapper) but the library (`go/orchestrator/`) is the contract. Anything that imports it gets orchestration, no matter the deployment shape.

**To resolve:** confirm. If yes, plan starts with the library API; daemon comes after.

### Q2: How does state actually get unified?

**Tension:** Kanban needs ordered transitions, idempotent recovery, fast lookup. Chitin's event chain gives you ordered + idempotent (chain index) but not fast-lookup-by-task-id without a derived index.

**Recommendation:** Chitin event chain is the source of truth. A small in-process projection (Go map keyed by task_id) is rebuilt on startup by reading recent events. Replay is the recovery model. No Redis.

**To resolve:** is the projection rebuild fast enough at expected event volumes? (Estimate: ~10⁴ tasks/day × 5 events/task = 50k events/day. SQLite + JSONL handles this trivially.)

### Q3: What's in the Task envelope?

**Tension:** Task envelope is the contract between callers and orchestrator. Too narrow → can't represent real work. Too wide → leaks driver-specific concerns.

**Working draft:**
```go
type Task struct {
    ID             string             // ULID
    Class          string             // "research" | "wiki_update" | "code_review" | "refactor" | ...
    Goal           string             // human-readable description
    ContextRefs    []string           // chitin chain IDs / file paths / URLs that scope context
    ModelFitMin    string             // "hands" | "brain" | "either"
    Deadline       time.Time          // soft deadline; informs router
    BudgetCents    int                // hard budget cap
    SubmittedBy    string             // agent_id of the submitter (could be a human or another agent)
    Labels         map[string]string  // for policy filtering
}
```

**To resolve:** is this the minimum viable shape? What's missing? Specifically: how does the caller pass *attached state* (a working directory, a partial diff, an open PR) without leaking driver concerns?

### Q4: How does the router actually decide?

**Tension:** The router needs `(task_class, model_fit_min, capacity_now, cost_per_class, deadline)` to make a decision. Some of these are slow-moving (cost), some fast-moving (capacity). Router must not be a bottleneck.

**Working draft:**
- Cost: static YAML, reloaded on file change.
- Capacity: read from `~/.chitin/events-<run_id>.jsonl` — last `capacity_advertisement` event per driver, with TTL (~30s).
- Decision: deterministic function of `(static_policy, current_capacity, task)`. Same inputs → same output. Decision is itself an event.

**To resolve:** how much capacity-state freshness is required? If a driver crashes, how long until the router stops sending it work? (Heartbeat interval × N missed = TTL.)

### Q5: What's the boundary between orchestrator and driver?

**Tension:** Driver mechanics (HTTP to ollama, subprocess to Claude Code, GraphQL to Copilot) must not leak into orchestrator. But orchestrator needs enough info from the driver to route well.

**Working draft:** Driver interface:
```go
type Driver interface {
    Capabilities() Capabilities                      // static — what it CAN do
    Capacity(ctx) (Capacity, error)                  // dynamic — what it CAN do RIGHT NOW
    Dispatch(ctx, Task) (Result, error)              // do the work
    HealthCheck(ctx) error                           // are we alive
}
```
Static `Capabilities` is registration-time. Dynamic `Capacity` is heartbeat-emitted. Orchestrator only ever calls these four methods.

**To resolve:** is `Dispatch` blocking or async? If async, how does the result come back? (Most likely: blocking from the driver's POV, but orchestrator can dispatch in goroutines.)

### Q6: What's the migration path from octi v1?

**Tension:** v1 has a config format, possibly some tasks in flight, and (probably) some hand-rolled driver shims. Throwing them all out wastes work; carrying them all over loses the clean-room benefit.

**Working draft:**
- v1's YAML cost cascade structure → port verbatim (it's the right shape; just changes home).
- v1's task envelope → review against Q3, port the parts that survive.
- v1's Redis-backed kanban → discard. Replay from chitin events.
- v1's HTTP API → discard. Library-first.
- v1's driver shims → drop. Adapters get rewritten against the new Driver interface.

**To resolve:** what's actually in v1 that's non-obvious from the README? Need a careful read of `chitinhq/octi` before plan.

## Lessons baked in from prior rewrites

These are the explicit "do not repeat" notes from clawta → hermes → octi-v1 → octi-v2:

- **Don't conflate the runtime, the governor, and the workflow.** Hermes was all three; each was done badly. v2: chitin is governor, drivers are runtime, octi is just the dispatcher. Workflow stays out of scope.
- **Don't build state stores you have to maintain.** Chitin already has SQLite + JSONL. Don't add Redis.
- **Don't design for hypothetical multi-tenant.** Solo first. Multi-host is a v2 problem if it ever exists.
- **Don't pretend prompt-level governance is governance.** The 2026-04-21 hermes incident proved this. octi v2 routes through `gov.Gate`, which is a real gate at the tool-call boundary.
- **Don't ship without dogfood.** First production workload of octi v2 is *chitin's own wiki maintenance*, against the operator's 3090 + cloud subs. If it can't keep its own knowledge base current without intervention, it's not ready for anyone else's workloads.

## Open questions (defer to plan, not block on)

- **Driver registration: static or dynamic?** Static (compile-time `init()`) is simpler; dynamic (drivers register over an IPC channel) lets out-of-process drivers participate. v1: static. v2 candidate: dynamic.
- **Per-driver isolation.** If openclaw's adapter panics, does the orchestrator crash? In-process means yes unless we wrap in `recover()`. Out-of-process gives isolation but adds IPC cost.
- **Backpressure semantics.** When all drivers are saturated, does the orchestrator buffer or fail-fast? v1: bounded buffer with timeout-on-deadline.
- **Event-stream consumer for capacity advertisements.** Is there a tail-the-JSONL helper in chitin already? (Likely; but need to confirm.) If not, adds a small subsystem.
- **Policy hot-reload.** v1: read on every call (acceptable cost at solo-operator volumes). v2 candidate: file-watch-driven reload.

## Tombstone for octi v1

When v2 lands and is dogfooded enough to call stable:

1. Final commit on `chitinhq/octi` adds a tombstone README pointing at `github.com/chitinhq/chitin/go/orchestrator/` and naming the migration date.
2. Repo archived (read-only) on GitHub.
3. The five lessons above are added to the chitin CLAUDE.md or equivalent so future octi-vN-rewrite-itch is met with documented prior reasoning.
4. A short retrospective is written at `docs/observations/octi-v1-retrospective.md` capturing what v1 got right and wrong. (This is what makes a clean room a clean room and not just amnesia.)

## Strategic ordering

Per the aggregate-first strategic memory and the 2026-05-07 talk forcing function:

1. **Pre-talk (now → 2026-05-07):** this spec is capture-only. No implementation. Aggregate-phase work (PR #36 finish, Phase F plan) keeps priority.
2. **Post-talk, post-Phase-F:** plan + implement. Plan first; specifically nail Q1–Q6 above. Implementation in iterations, each one dogfoodable.
3. **Workload-first sequencing of v1:** wiki maintenance is the first dogfood. Code review on incoming PRs is the second. Anything customer-facing is well downstream of those two.

This is post-talk, post-Phase-F work. No code on this spec until both gates are cleared.
