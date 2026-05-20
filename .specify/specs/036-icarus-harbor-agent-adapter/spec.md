# 036 — IcarusHarborAgent Adapter: Thin Baseline

> **Spec:** 036-icarus-harbor-agent-adapter
> **Author:** red
> **Status:** draft — awaits operator ratification
> **Created:** 2026-05-19

## Goal

Adapt the existing `IcarusAgent` (currently a Harbor `BaseAgent` subclass
in `swarm/icarus_harness/agent.py`) into a standalone **IcarusHarborAgent**
thin baseline that can run outside Harbor's containerized harness — via a
LiteLLM-backed LLM call loop inside a tmux session — while preserving the
bash-only, deterministic, loud-fail contract the agent already enforces.

The adapter is the minimum viable bridge between Icarus's inner loop
(parse → exec → observe → repeat until TASK_COMPLETE or loud-fail) and
the operator's RTX 3090 running `qwen3-coder:30b-32k` through LiteLLM.
No new capabilities. No deterministic router. No orchestration. Just the
loop, the model, and the shell.

## Scope (IN scope — what ships in v1)

1. **IcarusHarborAgent thin baseline** — a Python module (`swarm/icarus_harness/harbor_adapter.py`)
   that wires IcarusAgent's parse/exec/observe loop to LiteLLM instead of
   Harbor's container environment, producing the same JSONL trajectory and
   summary artifacts IcarusAgent already writes.

2. **Harbor async contract** — the adapter respects Harbor's async execution
   contract (`BaseAgent.run`) and reuses `AgentContext` / `ExecResult` types.
   The adapter does NOT run inside Harbor's Docker harness; it calls ollama
   directly via LiteLLM and executes commands via tmux instead of
   `environment.exec`. The contract boundary is the `BaseAgent` interface;
   beneath it, the execution substrate swaps from Docker→tmux.

3. **qwen3-coder:30b-32k via LiteLLM** — the LLM call path uses `litellm.completion()`
   with provider `ollama/qwen3-coder:30b-32k`. This replaces the direct
   `urllib` ollama client in the existing `ollama_chat()` function. LiteLLM
   provides provider normalisation, timeout management, and a clean fallback
   surface if the model changes later.

4. **tmux multi-turn shell loop** — instead of Harbor's per-turn fresh
   `environment.exec` (which starts a new process per step with no shell
   state), the adapter runs a persistent tmux session where shell state
   (cwd, exports, background jobs) carries across turns. The agent still
   emits one fenced bash block per turn; the adapter pastes it into the
   tmux pane and captures output.

### What each IN-scope item replaces from the existing harness

| Existing harness (Docker/Harbor) | Adapter (tmux/LiteLLM) |
|---|---|
| `harbor.environments.base.BaseEnvironment.exec(cmd)` | tmux send-keys + capture-pane |
| `ollama_chat()` (direct urllib POST to `:11434`) | `litellm.completion()` with `ollama/qwen3-coder:30b-32k` |
| Harbor's container provision + `BaseAgent.setup()` | tmux session creation + `IcarusAgent.setup()` (no-op) |
| Harbor's trial runner (`harbor run`) | `icarus-adapter-runner` CLI entry point |

## Scope (OUT of scope — explicitly deferred)

1. **Deterministic-first lane** — no rule-based router that tries bash
   patterns before invoking the LLM. The thin baseline ships the LLM loop
   as the sole path. Deterministic layers are deferred until failure
   clusters emerge from observed traces (see §Rationale).

2. **Receipts / kanban / Clawta-escalation** — the adapter produces IcarusAgent's
   existing trajectory + summary artifacts. It does NOT post WORKER_RECEIPT to
   the kanban board, does NOT escalate to Clawta, and does NOT integrate with
   the swarm board. Those surfaces belong to the outer icarus-watcher dispatch
   layer (spec 036-ic-001), not the adapter.

3. **Autofix layers** — no speculative code-fix heuristics (lint-fix, import-sort,
   dead-code-prune) wired into the adapter. Those belong to the skill-lane
   dispatch in spec 036-ic-001, layered on top of this baseline.

4. **Multi-model routing** — one model (`qwen3-coder:30b-32k`). No fallback to a
   smaller or cloud model. The adapter loud-fails when the model is unavailable.

5. **Harbor Docker harness changes** — the existing `harbor run` codepath and
   `IcarusAgent` for Docker-based runs are untouched. The adapter is an
   alternative entry point, not a replacement.

6. **VRAM lease/lock** — unlike the icarus-watcher (spec 036-ic-001 §Invariant 4),
   this adapter does NOT manage `~/.icarus/model-lease.lock`. The operator is
   responsible for ensuring the model is loaded (or the adapter loud-fails on
   connection refused).

## Rationale: Why path A (thin baseline first)

This section documents the three-way consensus from agent-bus messages
5406–5408 (red, Ares, Clawta) that selected **path A**: ship a thin
LLM-backed baseline first, defer all deterministic routing until failure
clusters emerge from observed traces.

### Path A: Thin baseline (selected)

Ship the minimum loop — LLM call → parse → exec → observe → repeat —
with IcarusAgent's existing loud-fail taxonomy but no deterministic
routing layer. Every turn goes through the model. Trajectories are
recorded. Failure clusters are mined from those trajectories later to
inform which deterministic shortcuts (if any) are worth adding.

### Path B: Deterministic router first (deferred)

Pre-classify tasks into "bash-solvable" vs "LLM-required" buckets.
Route bash-solvable tasks through deterministic heuristics without
invoking the model at all. Only fall through to the LLM for tasks that
fail classification.

### Path C: Dual-path parallel (deferred)

Run both deterministic and LLM paths for every task, compare results,
and gate the deterministic path's promotion on beating the LLM path on
latency + correctness over a rolling window.

### Consensus summary ( msgs 5406–5408 )

| Voter | Position | Key argument |
|---|---|---|
| red (msg 5406) | Path A | No data to justify deterministic routing. Shipping the baseline collects the traces needed to design any future deterministic layer from evidence, not speculation. Premature abstraction is the risk. |
| Ares (msg 5407) | Path A | The Harbor harness already exists and works. Layering a tmux adapter on top of the proven IcarusAgent inner loop is tractable. A deterministic router would need its own validation surface; we don't have the failure data yet. |
| Clawta (msg 5408) | Path A with amendment | Agrees with thin baseline, but insists the adapter MUST preserve IcarusAgent's loud-fail taxonomy (block_reasons: parse_failure, ollama_error, loop_detected, step_budget_exceeded, wallclock_exceeded) verbatim so the watcher layer can consume failures without schema drift. Amendment accepted. |

**Result:** Path A carries unanimously. The explicit contract is: the
adapter ships the LLM-only loop first, records every trajectory, and
gates any future deterministic layer on failure-cluster evidence mined
from those trajectories.

## Invariant (locked)

**INV-036-HA-1: No speculative deterministic router.**

The adapter MUST NOT ship a deterministic-first routing layer. Every
turn in the thin baseline goes through the LLM. Failure traces from
this baseline are the ONLY gate for any future deterministic layer:
a deterministic shortcut is promoted into the loop only when the
failure-cluster evidence from recorded trajectories demonstrates that
the shortcut would have resolved the task faster and more reliably than
the LLM path alone. Until such evidence exists, the baseline stays
LLM-only.

This invariant is the three-way consensus (msgs 5406–5408) made
explicit. Violating it — by shipping a deterministic router without
trace evidence — is a process bug.

## Architecture

```
                    ┌─────────────────────────────────────────┐
                    │  icarus-adapter-runner (CLI entry)      │
                    │  parses args → creates tmux session     │
                    │  → invokes IcarusAgent via adapter       │
                    └──────────────┬──────────────────────────┘
                                   │
                    ┌──────────────▼──────────────────────────┐
                    │  harbor_adapter.py                       │
                    │  TmuxEnvironment(BaseEnvironment)       │
                    │    .exec(cmd) → tmux send-keys + capture │
                    │  LiteLLMChat (replaces ollama_chat)      │
                    │    .complete(messages) → litellm call    │
                    │  IcarusAgent.run(instruction, env, ctx)  │
                    └──────────────┬──────────────────────────┘
                                   │
                    ┌──────────┬───┴───────────┬──────────────┐
                    │          │               │              │
                    ▼          ▼               ▼              ▼
              tmux pane   litellm/ollama   logs_dir/      summary
              (stateful   (qwen3-coder    .jsonl       .json
               shell)      :30b-32k)      trajectory)   block_reason)
```

The adapter reuses IcarusAgent's core loop (parse → exec → observe →
loop detection → loud-fail taxonomy) by swapping two substrates:

1. **LLM calls**: `ollama_chat()` → `litellm.completion()` (provider
   `ollama/qwen3-coder:30b-32k`). Same temperature=0, seed=0 determinism
   contract. LiteLLM handles provider routing; the adapter doesn't need
   to know whether ollama is local or remote.

2. **Command execution**: `BaseEnvironment.exec()` →
   `TmuxEnvironment.exec()` (new subclass). `TmuxEnvironment` manages
   a tmux session per trial, pastes each command block via
   `send-keys`, and captures output via `capture-pane`. Shell state
   (cwd, env vars, background processes) persists across turns — this
   is the primary behavioral difference from Harbor's per-turn fresh
   exec.

## File-system scope (proposed)

- `swarm/icarus_harness/harbor_adapter.py` — TmuxEnvironment, LiteLLMChat, adapter wiring
- `swarm/icarus_harness/adapter_runner.py` — CLI entry point (`icarus-adapter-runner`)
- `swarm/tests/test_harbor_adapter.py` — unit tests for TmuxEnvironment, LiteLLMChat, adapter wiring
- `swarm/tests/test_harbor_adapter_e2e.py` — e2e test: one real tmux session through IcarusAgent
- `.specify/specs/036-icarus-harbor-agent-adapter/**` — this spec directory

## Acceptance criteria (TBD — to be enumerated when impl ships)

- [ ] `TmuxEnvironment.exec()` sends a command to a tmux session and
      returns `ExecResult` with captured stdout/stderr/return_code
- [ ] Adapter runs IcarusAgent's full loop (system prompt → LLM → parse
      → exec → observe → loop detect → loud-fail or TASK_COMPLETE) via
      LiteLLM + tmux, producing the same JSONL trajectory and summary
      artifacts as the Harbor path
- [ ] Loud-fail taxonomy preserved verbatim: parse_failure, ollama_error
      (renamed to llm_error in adapter context), loop_detected,
      step_budget_exceeded, wallclock_exceeded
- [ ] INV-036-HA-1 holds: no deterministic routing layer in the adapter.
      Every turn goes through the LLM.
- [ ] Trajectory JSONL recorded to `logs_dir/icarus-trajectory.jsonl`
      for future failure-cluster mining
- [ ] Adapter runner CLI exits with 0 on TASK_COMPLETE, non-zero on
      loud-fail (with structured block_reason in exit metadata)

## Sign-off log

- [ ] red — author, this draft
- [ ] Ares — must ratify if the adapter changes IcarusAgent's inner loop contract
- [ ] Clawta — must ratify per msg 5408 amendment (loud-fail taxonomy preservation)
- [ ] **operator** — final ratification gate