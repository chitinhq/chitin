# Thesis

> Chitin is an execution kernel for AI coding agents. Every tool call across Claude Code, Copilot CLI, and openclaw is gated by a single policy and recorded in a hash-linked event chain that also emits OTEL spans into your existing observability stack.

## What chitin is

A small Go kernel that sits between AI coding agents and the operating system. Three patterns are gated through it today:

- **Claude Code** — via a `PreToolUse` hook installed into `~/.claude/settings.json`.
- **Copilot CLI** — via the in-kernel SDK driver (`chitin-kernel drive copilot`).
- **openclaw** — via an `acpx` config-override (one-line install, no chitin-side wrapper code).

Same `gov.Gate` API across all three. Same canonical event chain on disk. Same OTEL projection out the back.

## What chitin is not

- Not a tick-loop / agent-runner. Hermes is dead (2026-04-23). Drivers run on their own surfaces; chitin gates their tool calls.
- Not an OTEL ingest target. Chitin **emits** spans as a projection of its canonical chain. The chain is the source of truth; OTEL is one-way out.
- Not a cloud product (yet). Phase 1 is local-only. Cloud is the monetization step on the strategic arc — it follows ecosystem distribution, not the other way around.

## The closed loop

```
   real execution
        │
        ▼
   deterministic capture (chain canonical)
        │
        ▼
   policy derivation (debt ledger → chitin.yaml)
        │
        ▼
   enforced constraints (gov.Gate)
        │
        ▼
   new execution ── back to top
```

Most agent-observability tools are open-loop (capture only) or heuristic (LLM-judges). Chitin's loop is grounded in real captured execution; that's the moat.

## Strategic arc

1. **Aggregate** — always-on capture across the three drivers.
2. **Align policies to data** — governance rules emerge from the debt ledger, not from a-priori spec.
3. **Ecosystem distribution** — chitin runs inside agent ecosystems (openclaw, future frameworks), not just Claude Code.
4. **Policy packs** — reusable governance bundles for everyone running chitin-instrumented agents.
5. **Cloud offering** — centralized trace/policy surface as a service.
6. **North star** — autonomous swarm, plus a product that builds itself, both governed by chitin.

## Audience sequencing

A1 (agent framework builders) → A2 (platform/infra) → A4 (security/compliance). A3 (solo operators) is a side channel. A1 messaging is not diluted with platform/dashboard/cost narratives.

## Principle

> Real execution before policy. Policy before automation. Automation gated by the same kernel.

See [`architecture.md`](./architecture.md) for the layer map and [`architecture/layer-contracts.md`](./architecture/layer-contracts.md) for the four invariants this thesis rests on.
