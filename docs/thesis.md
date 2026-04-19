# Thesis

Chitin v2 is an observability-first substrate for AI coding agents. The
near-term system is hybrid: cloud reasoning via services like Ollama Cloud
and Claude, plus bounded local execution on an RTX 3090 workstation via
local Ollama and tools like OpenClaw. No execution surface gets governance
or automation before it gets observability.

## Order of operations

1. **Observability** — capture, normalize, persist, replay.
2. **Governance** — policy, invariants, enforcement.
3. **Automation** — dispatch, scheduling, swarm.

Phase 1 instruments Claude Code on the 3090 box. Phase 1.5 extends the
same contract to OpenClaw and local Ollama. Governance begins only after
both surfaces are fully observable and the event model has proven
surface-neutral.

## Principle

> Claude Code, OpenClaw, and local/cloud Ollama-backed agent execution all
> require observability before governance or automation.
