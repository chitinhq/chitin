# Thesis

> Chitin is an execution kernel for AI coding agents — and the swarm
> that drives them. Every tool call across Claude Code, Codex CLI,
> Gemini CLI, Hermes, Copilot CLI, and openclaw is gated by a single
> policy and recorded in a hash-linked event chain that emits OTEL
> spans into your existing observability stack. The chitin-owned swarm
> composes hermes (kanban) and openclaw (Lobster + agent runtime) as
> substrates; chitin contributes governance, identity, and chain at
> every hop.

## What chitin is

A small Go kernel that sits between AI coding agents and the
operating system, plus the swarm that dispatches work to those agents.
Six vendor surfaces are gated through it today:

- **Claude Code** — `PreToolUse` hook in `~/.claude/settings.json`.
- **Codex CLI** — `PreToolUse` hook in `~/.codex/config.toml` (`[features] codex_hooks=true` + `[[hooks.PreToolUse]]`); same wire shape as Claude Code.
- **Gemini CLI** — `BeforeTool` hook in `~/.gemini/settings.json`; same wire shape as Claude Code (renamed event).
- **Hermes** — `pre_tool_call` hook in `~/.hermes/config.yaml`; same wire shape as Claude Code.
- **Copilot CLI** — in-kernel SDK driver (`chitin-kernel drive copilot`); wrapping orchestrator pattern (closed vendor).
- **openclaw** (`local-*` drivers: qwen / glm / glm-flash / deepseek, plus the clawta orchestrator agent itself) — `before_tool_call` plugin path via `apps/openclaw-plugin-governance/`. Different shape from the hook drivers by design: plugin runtime, not native hook.

Same `gov.Gate` API across all six. Same canonical event chain on
disk. Same OTEL projection out the back. Vendor-specific
normalization lives in `internal/driver/<vendor>/normalize.go` for
the four hook drivers; in the openclaw plugin source for openclaw;
in the in-kernel SDK driver for copilot.

The four hook-based PreToolUse drivers (Claude Code, Codex, Gemini,
Hermes) all point at the same compiled Go shim `bin/chitin-router-hook`,
which stamps `CHITIN_DRIVER` and dispatches to the per-vendor
normalizer.

## The chitin-owned swarm

Six drivers gated is necessary but not sufficient. Chitin also owns
the swarm that drives those drivers — a four-hop pipeline composing
hermes (kanban substrate) and openclaw (Lobster + agent runtime
substrate):

```
hermes kanban (substrate)
  → clawta tick (chitin: poller + dispatch wrapper)
    → openclaw kanban-dispatch.lobster (substrate: workflow + acpx)
      → frontier-coder CLI (gov.Gate at the leaf)
```

Every hop emits a chain event keyed by `CHITIN_DRIVER`. Every leaf
tool call is gated by `gov.Gate.Evaluate`. The chain is the unifying
contract.

The swarm lives in `swarm/` in this repo (not hermes, not openclaw)
because the unifying contracts — chain schema, `CHITIN_DRIVER`
identity, policy authoring, the router-hook binary — are chitin's. The
substrates underneath are upstream. See
[`docs/decisions/2026-05-13-swarm-readopted-composing-substrates.md`](./decisions/2026-05-13-swarm-readopted-composing-substrates.md)
for the boundary.

## What chitin is not

- Not a kanban server. Hermes owns the kanban data + UI; chitin reads
  and writes via `kanban-flow`, which goes through the hermes CLI.
- Not a workflow engine. Lobster (openclaw-side) executes
  `kanban-dispatch.lobster`; chitin authors the workflow but doesn't
  ship its own runner.
- Not an agent runtime. Openclaw's acpx + pi-agent-core run the
  agents; chitin's plugin gates their tool calls.
- Not an OTEL ingest target. Chitin **emits** spans as a projection
  of its canonical chain. The chain is the source of truth; OTEL is
  one-way out.
- Not an LLM provider abstraction. Chitin doesn't proxy or re-call
  models. Each vendor's CLI talks to its own backend (codex → OpenAI
  under your ChatGPT Plus auth; gemini → Google under Pro auth;
  etc.); chitin governs the actions those CLIs take.
- Not a SaaS. Chitin is local-only: the operator's box, the
  operator's data.

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

Most agent-observability tools are open-loop (capture only) or
heuristic (LLM-judges). Chitin's loop is grounded in real captured
execution; that's the moat. The swarm is the workload that keeps the
loop fed.

## Strategic arc

1. **Aggregate** — always-on capture across supported drivers.
2. **Align policies to data** — governance rules emerge from the
   debt ledger, not from a-priori spec.
3. **Compose with orchestration substrates** — we're here. Chitin's
   swarm runs on hermes (kanban) + openclaw (Lobster + acpx),
   contributing chain + identity + gate at every hop. The four-hop
   pipeline is the load-bearing artifact.
4. **Policy packs** — reusable governance bundles for everyone
   running chitin-instrumented agents.
5. **Local operator leverage** — better policy packs, replay, and
   chain-derived analytics without sending the operator's governance
   data to a service.

## Audience sequencing

A1 (agent framework builders) → A2 (platform/infra) → A4
(security/compliance). A3 (solo operators) is a side channel. A1
messaging is not diluted with platform/dashboard/cost narratives.

## Principle

> Real execution before policy. Policy before automation. Automation
> gated by the same kernel. Substrates owned upstream, composition
> owned here.

See [`architecture.md`](./architecture.md) for the layer map,
[`architecture/layer-contracts.md`](./architecture/layer-contracts.md)
for the four invariants this thesis rests on, and the 2026-05-13
substrate-composition decision for the swarm boundary.
