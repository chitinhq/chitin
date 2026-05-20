# ic-001 — Icarus: Local-LLM Driver on RTX 3090

> **Tier:** spec (operator-ratification gated)
> **Status:** DRAFT

## Goal

Add a deterministic local-LLM worker to the swarm.

## Acceptance criteria

- [ ] AC1: icarus-watcher polls swarm board every 1 min
- [ ] AC2: All 5 lanes have deterministic post-checks
- [ ] AC3: Loud-fail-on-ceiling escalates to Clawta

## Boundary cases

1. **GPU OOM during Icarus run** → ollama returns error → loud-fail.
2. **Ollama daemon crashed** → connection refused → loud-fail.
3. **Multiple tickets ready simultaneously** → process serially.

## Open questions

- **Q1** — Channel: own #icarus or share #swarm? Proposed: own.
- **Q2** — Default model? Proposed: qwen3-coder:30b.

## Slice plan

- **Slice 1** — Stub watcher with lint-fix lane only.
- **Slice 2** — Enable log-pattern + triage-classify.
- **Slice 3** — Enable doc-from-code + mechanical.