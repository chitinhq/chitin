# Chitin Orchestrator — Engine Options & Recommendation

**2026-05-20 · operator: red · prepared by: Claude Code**

> The decision: replace the orchestration sprawl — ~36 cron jobs across two
> gateways, 52 `swarm/bin/` shell scripts, lobster dispatch, the agent-bus —
> with **one** deterministic, observable orchestrator: **Chitin Orchestrator**.
> Goal: determinism *and* telemetry in orchestration, robust enough to run a
> fully autonomous swarm.

## Requirements (the scoring lens)

1. **Determinism** — durable execution; a workflow survives restarts and
   replays identically. Non-negotiable.
2. **Telemetry-native** — every workflow run is inspectable and replayable.
   This is half the point.
3. **Go-friendly** — the Chitin Kernel is Go; one language is less sprawl.
4. **Low-ops** — single operator, one box. The orchestrator must not become
   its own maintenance burden.
5. **Maturity** — it runs the autonomous swarm; it cannot be the flaky part.

## First: LangGraph is a different layer — not a candidate

LangGraph and Temporal are **complementary, not competitors** (this is the
clear 2026 consensus). LangGraph models *agent reasoning flow* — the cyclic
logic inside one agent's head. Temporal-class engines run *durable
macro-orchestration* — the multi-hour lifecycle, retries, scheduling,
state across restarts.

Chitin's orchestration need is the **macro layer**: pull loops, the
dispatch pipeline, pollers, the board engine, the Icarus bench loop —
replacing crons and scripts. The agents (Ares, Clawta, Claude Code)
already do the reasoning. So LangGraph is the wrong tool for *this* layer.
(It could later live *inside* an agent as an activity — not here, not now.)

## The real options — durable execution engines

| Axis | **Temporal** | **Restate** | **DBOS** |
|---|---|---|---|
| Durable execution / determinism | Gold standard — workflow replay | Yes — journaled invocations | Yes — Postgres-backed steps |
| Telemetry ("into orchestration") | **Best** — full event-history UI; every run inspectable + replayable | Good — has a UI | Lighter; via DBOS console |
| Go SDK (matches the Kernel) | First-class | Yes | **No — Python/TS only** |
| Operational weight | Heaviest — runs a server (`temporal server start-dev` is a single binary; Temporal Cloud exists) | Light — single binary | **Lightest** — a library + a Postgres |
| Maturity | Most mature, proven at scale | Newer (2024+), growing fast | Newer, smallest footprint |
| Code-as-workflow | Yes (Temporal's explicit stance: code > graph) | Yes | Yes (decorators) |

## Recommendation: **Temporal**

For Chitin specifically:

1. **Telemetry is the product.** The operator's stated requirement is
   "telemetry into orchestration." Temporal's event-history UI *is* that —
   every workflow execution is a complete, inspectable, replayable timeline.
   No other option matches it out of the box. This alone is decisive given
   the observability thesis.
2. **Determinism is the product.** Workflow replay is Temporal's core
   guarantee — the most battle-tested in the field.
3. **Go-first.** First-class Go SDK matches the Chitin Kernel — one
   language across kernel + orchestrator. And the retired "Octi" specs
   (040–048) already chose **Temporal Go** — that design thinking is
   salvage, not waste.
4. **Maturity de-risks the autonomous-swarm goal.** The orchestrator runs
   everything; it must be the *least* flaky component. Temporal is proven.
5. **Ops weight is the one knock — and it's mitigated.** `temporal server
   start-dev` is a single binary; for a one-box dogfood deployment that is
   entirely adequate. Temporal Cloud is the escape hatch if scale ever
   demands it (it won't soon).

**Honest counter-case:** if minimum operational footprint were the *only*
priority, **DBOS** wins — durable execution as a library on a Postgres you
already run, ~7 lines to adopt. But DBOS has **no Go SDK** (would force the
orchestrator into Python) and a lighter observability story — both lose
against the Kernel-language fit and the telemetry requirement. **Restate**
is the credible middle option (lighter than Temporal, Go SDK, a UI) and is
the fallback if Temporal's server proves annoying in practice.

## What "Chitin Orchestrator on Temporal" replaces

Each becomes a Temporal workflow; the cron/script vanishes:

| Today | Becomes |
|---|---|
| `kanban-pull-loop` crons (Ares + Clawta) | A durable pull-loop workflow per agent |
| Clawta dispatch pipeline + `swarm/bin/clawta-*` (13 scripts) | A dispatch workflow with typed activities |
| `autonomous-board-engine`, pollers, watchdogs | Scheduled workflows |
| `icarus-bench.service` loop | A bench workflow (durable, replayable runs) |
| Truncation retries, stuck-ticket recovery | Native Temporal retries + timeouts |
| Telemetry "did the cron run?" guesswork | The Temporal UI — definitive |

The agent-bus is already going; crons retire as their workflow lands.

## Migration path (incremental, low-risk)

1. Stand up `temporal server start-dev` on the box; wire it to Chitin Telemetry (OTel).
2. Migrate **one** workflow first — the pull-loop — and run it beside the cron until trusted.
3. Then the dispatch pipeline; then pollers/watchdogs; then the Icarus bench loop.
4. Retire each cron/script as its workflow proves out. The 52-script `swarm/bin/` collapses into workflow + activity code.
5. Salvage specs 040–048 as the design basis — re-homed under "Chitin Orchestrator," Octi name dropped.

This is **one named effort** with a clear end state, not 36 moving parts.

## Decision for the operator

- Confirm **Temporal** as the Chitin Orchestrator engine? (Restate is the
  documented fallback; DBOS noted but language-mismatched.)
- On confirm: this becomes a spec — driven properly through the spec-kit
  skills — re-homing 040–048 as the Chitin Orchestrator spec set.
