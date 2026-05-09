# Thesis

> Chitin is an execution kernel for AI coding agents. Every tool call across Claude Code, Codex CLI, Gemini CLI, Hermes, Copilot CLI, and openclaw is gated by a single policy and recorded in a hash-linked event chain that also emits OTEL spans into your existing observability stack.

## What chitin is

A small Go kernel that sits between AI coding agents and the operating system. Six vendor surfaces are gated through it today:

- **Claude Code** — `PreToolUse` hook in `~/.claude/settings.json`.
- **Codex CLI** — `PreToolUse` hook in `~/.codex/config.toml` (`[features] codex_hooks=true` + `[[hooks.PreToolUse]]`); same wire shape as Claude Code.
- **Gemini CLI** — `BeforeTool` hook in `~/.gemini/settings.json`; same wire shape as Claude Code (renamed event).
- **Hermes** — `pre_tool_call` hook in `~/.hermes/config.yaml`; same wire shape as Claude Code.
- **Copilot CLI** — in-kernel SDK driver (`chitin-kernel drive copilot`); wrapping orchestrator pattern (closed vendor).
- **openclaw** (`local-*` drivers: qwen / glm / glm-flash / deepseek) — `before_tool_call` plugin path.

Same `gov.Gate` API across all six. Same canonical event chain on disk. Same OTEL projection out the back. Vendor-specific normalization lives in `internal/driver/<vendor>/normalize.go`.

## What chitin is not

- Not a tick-loop / agent-runner. Hermes owns orchestration, approvals, kanban, and scheduling; chitin gates the tool calls Hermes and the standalone drivers attempt.
- Not an OTEL ingest target. Chitin **emits** spans as a projection of its canonical chain. The chain is the source of truth; OTEL is one-way out.
- Not an LLM provider abstraction. Chitin doesn't proxy or re-call models. Each vendor's CLI talks to its own backend (codex → OpenAI under your ChatGPT Plus auth; gemini → Google under Pro auth; etc.); chitin governs the actions those CLIs take.
- Not a SaaS. Chitin is local-only: the operator's box, the operator's data.

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

1. **Aggregate** — always-on capture across supported drivers.
2. **Align policies to data** — governance rules emerge from the debt ledger, not from a-priori spec.
3. **Ecosystem distribution** — chitin runs inside agent ecosystems (openclaw, future frameworks), not just Claude Code.
4. **Policy packs** — reusable governance bundles for everyone running chitin-instrumented agents.
5. **Local operator leverage** — better policy packs, replay, and chain-derived analytics without sending the operator's governance data to a service.

## Audience sequencing

A1 (agent framework builders) → A2 (platform/infra) → A4 (security/compliance). A3 (solo operators) is a side channel. A1 messaging is not diluted with platform/dashboard/cost narratives.

## Principle

> Real execution before policy. Policy before automation. Automation gated by the same kernel.

See [`architecture.md`](./architecture.md) for the layer map and [`architecture/layer-contracts.md`](./architecture/layer-contracts.md) for the four invariants this thesis rests on.
