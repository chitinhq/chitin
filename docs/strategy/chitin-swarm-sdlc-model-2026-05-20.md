# Chitin — The Swarm Against the SDLC

**2026-05-20 · operator: red** · supersedes the role-lane / board-pull model

> How the Chitin swarm runs the software-development lifecycle **after** the
> 2026-05-20 refocus: the **orchestrator drives**, the kernel gates, telemetry
> observes — and the board is demoted from "the driver" to a read-surface.

## The model in one diagram

```mermaid
flowchart LR
  subgraph SDLC["Autonomous SDLC loop — spec-kit driven"]
    direction LR
    SPEC["Spec<br/>speckit-specify"] --> PLAN["Plan<br/>speckit-plan"]
    PLAN --> TASKS["Tasks<br/>speckit-tasks → task DAG"]
    TASKS --> ANALYZE["Analyze<br/>speckit-analyze (gate)"]
    ANALYZE --> IMPL["Implement"]
    IMPL --> REVIEW["Review<br/>Copilot + a peer agent"]
    REVIEW --> MERGE["Merge → main"]
  end

  ORCH[["CHITIN ORCHESTRATOR (Temporal)<br/>deterministically sequences the task DAG —<br/>derives what runs next mathematically, not from a board.<br/>THE DRIVER."]]
  TASKS -->|"task DAG (deps)"| ORCH
  ORCH -->|"dispatches work units"| IMPL
  ORCH -->|"schedules / sequences"| REVIEW

  subgraph AGENTS["Agents — agent-agnostic. Each work unit = one worker in its own git worktree."]
    ARES["Ares"]
    CLAWTA["Clawta"]
    CC["Claude Code"]
    COPILOT["Copilot (review)"]
    OWN["future: first-party Chitin agent"]
  end
  ORCH --> AGENTS

  KERNEL[["CHITIN KERNEL<br/>gates every tool call vs chitin.yaml"]]
  AGENTS -.->|"every tool call"| KERNEL

  subgraph TELEM["CHITIN TELEMETRY — observe, never drive"]
    CHAIN["chitin chain<br/>hash-linked audit"]
    ARGUS["Argus / Sentinel<br/>anomaly detection, coaching"]
    LOG["activity log<br/>(the former kanban — a READ surface)"]
  end
  KERNEL --> CHAIN
  ORCH --> CHAIN
  ORCH --> LOG
  MERGE --> TELEM
  TELEM -->|"feedback → the next spec"| SPEC
```

## What changed (the 2026-05-20 refocus)

1. **The orchestrator drives — not the board.** Work sequencing and
   scheduling are **derived deterministically** from the spec's task DAG
   (`tasks.md` → dependency graph). The orchestrator computes what runs next
   *mathematically*; there is no heuristic optimizer and no human-managed
   kanban deciding order.

2. **The kanban is demoted to telemetry.** A board / activity log still
   exists — but only as a **read surface**: a projection of orchestrator
   state you can look at. It never decides what work happens. The **Hermes
   Kanban is end-of-life** — its driving role moves into the orchestrator;
   its readable role moves into Chitin Telemetry.

3. **Agent- and driver-agnostic.** The orchestrator dispatches *work units*;
   which agent fulfils one (Ares, Clawta, Claude Code, Copilot, or a future
   **first-party Chitin agent**) is a routing decision, not an architectural
   dependency. No reliance on Hermes plugins or the Hermes Kanban.

4. **Workers + worktrees, always.** Every work unit runs as a worker in its
   own dedicated git worktree (constitution §2; spec 070 FR-013/14). The
   shared checkout is never a work surface.

5. **Determinism end-to-end.** Spec-kit makes the *intent* deterministic
   (spec → plan → tasks); the Temporal orchestrator makes the *execution*
   deterministic (durable, replayable workflows). Telemetry closes the loop.

## The three layers

| Layer | Role | Drives? |
|-------|------|---------|
| **Chitin Kernel** | gates every tool call against policy | gate, not driver |
| **Chitin Orchestrator** | sequences + schedules + dispatches the SDLC | **the driver** |
| **Chitin Telemetry** | the chain + Argus/Sentinel + the activity log | observe, never drive |

Agents are interchangeable executors inside this frame. The board is a
window, not a steering wheel.

> Status: design doc, drawn while spec 070 (Chitin Orchestrator) is in
> implementation. Reconcile `docs/strategy/chitin-swarm-operating-model.svg`
> (the older board-pull model) against this once 070 lands.
