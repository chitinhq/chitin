# Chitin is execution governance, not an agent framework

Status: durable positioning. Captures the operator's strategic
reframing on 2026-05-06 PM — explicit so it doesn't get lost across
sessions or in informal memory.

Date: 2026-05-06

## What chitin IS

> Execution governance infrastructure for heterogeneous coding
> agents.

A programmable execution-governance runtime with extensible
intervention hooks. Stable primitives (tool invocation, execution
authority, policy enforcement, observability, escalation) wrapped
around an unstable substrate (drivers, models, benchmarks).

## What chitin is NOT

- An "AI agent framework"
- A "swarm orchestrator"
- A "model router"
- A "persistent multi-agent system"

The "swarm" and "agent" framings got us here but they understate
the substrate. Future external positioning should use:

- "adaptive execution governance"
- "AI runtime supervision"
- "execution policy middleware"
- "programmable control plane"

These map to enterprise-legible categories (observability,
compliance, security, cost, benchmarking) rather than research-y
agent hype.

## The moat (rank-ordered)

What chitin uniquely owns:

1. **Universal tool-call interception** — every tool call from every
   driver passes through the kernel gate. No competitor has this
   vantage.
2. **Execution visibility** — normalized chain across drivers
   (claude-code, copilot, codex, gemini, openclaw, hermes). One
   schema, one query.
3. **Replayability** — every gate event is recoverable. Audits,
   retroactive policy derivation, training data extraction all
   become possible.
4. **Policy enforcement** — kernel is the single side-effect
   authority. Plugins advise; kernel decides.
5. **Cross-driver telemetry normalization** — same schema for cost,
   tool calls, denials, escalations across every CLI we wrap.
6. **Observed (not declared) compatibility** — per-(driver, model,
   workload) capability vectors derived from real dispatches, not
   leaderboard scrapes.

The advisor / peer-spawn / matrix work is INSTRUMENTATION around
these primitives. The advisor is not the moat — it accelerates data
collection on top of the moat.

## Core architectural invariants

These follow from the positioning. Code that violates any of these
should be reverted regardless of how convenient it is.

### Kernel = single side-effect authority

The Go kernel is:
- boring
- deterministic
- auditable
- minimal

Plugins (advisor, heuristics, peer-spawn, etc.) can:
- advise
- annotate
- escalate
- deny
- rewrite requests
- inject guidance

Plugins CANNOT:
- own execution authority
- mutate policy decisions outside the kernel's verdict path
- emit side effects the kernel didn't authorize

If a plugin needs to do something authoritative, that capability
belongs in the kernel, not the plugin.

### Heuristics are sparse, high-precision, confidence-thresholded

Escalation is CPU-interrupt-shaped: rare per tool call, expensive,
high-value. The reconciliation with "escalation isn't rare" (the
operator's earlier framing): per-tool-call escalation should be
rare; per-workflow aggregate escalation is common because workflows
have many tool calls. The MOB-PROG model holds: T0 does most tool
calls; only the few that need help trigger T1 escalation; fewer
trigger T2; very rare to T4.

Specifically:
- Floundering threshold: looped >= 2x identical tool call (existing).
- Blast radius: > 25 files in a single change (existing).
- Drift: advisor explicit takeover-vote (existing).
- Future heuristics must justify why they're high-precision before
  being added — false positives compound across the chain.

### Synchronous escalation latency is the biggest technical risk

Per the architectural review: "If every questionable tool call
pauses, serializes replay, spawns another CLI, rehydrates context,
waits for analysis... you can accidentally create 5–20x latency
amplification, deadlocks, recursive escalation loops, degraded UX."

Mitigations baked into the mob-prog design (see
`2026-05-06-mob-programming-escalation.md`):
- `max_total_per_workflow` cap (proposed 10; this review suggests
  going LOWER — 5 with operator override)
- Per-tier short timeouts (T1=30s, T4=180s)
- Strict bounded recursion (max 4 levels)

Future async escalation (peer runs in background, returns context
later) is the path past this constraint but not in v1.

### Replay hydration is the hidden scaling problem (not tokens)

OTEL traces grow. Prompts grow. File diffs grow. Tool histories
grow. As escalation chains accumulate, advisors become
context-bound by THEIR OWN replay context — not by underlying
model context.

Mitigations to design before they're forced:
- Summarization layers (chain → digest)
- Semantic compression (drop redundant denials, keep deltas)
- Execution snapshots (state-anchor instead of full replay)
- Rolling context windows (last N events for advisors)
- Causal slicing (replay only the events the consultant actually
  needs, not the full chain)

These are all PRE-MATURE today (we have ~60 events/day) but become
load-bearing as we add more drivers + more dispatches.

## Long-term economic bet

Tier-0 quality rises (open-source models keep improving). Frontier
model pricing pressure remains volatile. Chitin's architecture
benefits asymmetrically:

- T0 (free local) gets better → escalation frequency falls
- Higher tiers stay constant cost (per-token API or fixed sub)
- Governance + observability + replay value remain stable
- Result: cost collapses while moat capabilities stay constant

The "shift-left" pattern (mob-prog doc) is the mechanical realization
of this bet: capture every escalation chain, derive what T0 needs to
absorb, ship that to T0 prompts/skills/scaffolding, repeat.

## Removing Temporal was correct (for current stage)

Temporal was orchestration substrate. Chitin's leverage is in:
- the gate (every tool call)
- the chain (every event)
- the policy (every verdict)

Not in:
- workflow durability
- DAG orchestration
- distributed retries
- activity history

Hermes Kanban absorbs the orchestration role at a fraction of the
maintenance cost. If we re-add durable workflow infra later, it
should be because invariant clarity demands it, not because
"orchestrator" felt missing.

## Things to NOT do (durable warnings)

- **Don't grant plugins execution authority.** Even if it's
  convenient. Replay breaks otherwise.
- **Don't overfit to "swarm" language.** Use governance / runtime /
  control-plane framing externally.
- **Don't add heuristics without precision evidence.** False
  positives in the gate compound expensively.
- **Don't add asynchronous in-gate spawn before sync is proven
  problematic.** Adds significant complexity for marginal gain
  today.
- **Don't ignore replay-size growth.** It's the next-but-one
  scaling cliff.

## What this changes immediately

Decisions on the open mob-prog escalation questions (from
`2026-05-06-mob-programming-escalation.md`):

1. `max_total_per_workflow` — **lower to 5**, not 10. Per "rare and
   expensive" framing.
2. `max_to_tier_T4` — **stay at 1**. T4 is interrupt-grade.
3. `CHITIN_ESCALATION_DEPTH=0` override — **yes**, equivalent to
   today's behavior, operator-explicit-disable lever.
4. Entry `tier:` field — **ceiling, not start**. Per the original
   shift-left framing.

These ship in step 7a (schema) of the recursive-escalation work
when we get to it.
