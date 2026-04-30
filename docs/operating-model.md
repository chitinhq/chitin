# Operating Model

How chitin actually runs today, and what each subsystem owns.

## Topology

Single-box. One Linux workstation with an RTX 3090 hosts the entire dev + dogfood loop.

```
                  ┌─────────────────────────────────────────────┐
                  │  Local workstation (RTX 3090, this machine) │
                  │                                             │
   Anthropic ─────┤── Claude Code ──┐                           │
   (cloud)        │                  │                          │
                  │                  ├──► chitin-kernel ──► .chitin/
   GitHub ────────┤── Copilot CLI ──┤    (gov.Gate gates       │
   (cloud)        │                  │     every tool call)    │
                  │                  │                          │
   local Ollama ──┤── openclaw ─────┘                           │
   (3090 GPU)     │                                             │
                  └─────────────────────────────────────────────┘
```

- **No Hetzner box.** Multi-machine observability was a Phase 3 framing in older docs; the box is dead. Collapse any "cross-machine" reading into single-box.
- **Murphy's Mac** is separate — chitin is not installed there. Out of scope for current dogfooding.
- **Ollama Cloud Pro + Anthropic + GitHub Copilot** are cloud reasoning surfaces; they are not chitin's scope. Chitin only sees what the local CLI/driver does on this box.

## Subsystem ownership

| Subsystem | Owner | Live? |
|-----------|-------|-------|
| Capture (event chain) | Go kernel `emit` | ✅ shipped Phase 1.5, 2026-04-19 |
| Replay | TS `libs/telemetry` + `apps/cli` | ✅ shipped Phase 1 |
| Governance (`gov.Gate`) | Go kernel `internal/gov` | ✅ shipped 2026-04-28 (PR #45 + #51) |
| Cost envelope (cross-process) | Go kernel `internal/envelope` | 🔄 cost-gov v3 in flight (committed 2026-04-29, c1ecbf9) |
| Drivers — Claude Code hook | `libs/adapters/claude-code` + kernel install path | ✅ shipped (PR #66) |
| Drivers — Copilot CLI (v1, wrapping) | Kernel `drive copilot` | ✅ shipped (PR #51) |
| Drivers — openclaw (acpx config) | One-line config; no chitin code | ✅ shipped |
| OTEL emit (projection) | Go kernel `internal/emit` (TBD) | 🔄 F4 — ships before 2026-05-07 talk |
| Souls library | `souls/canonical/` + `souls/experimental/` | ✅ shipped Phase 1.5 |

## Order of operations (current, not aspirational)

Older docs stated "observability → governance → automation, in that order, per surface." That ordering described Phase 1's implementation sequence; it is not a design constraint anymore. Today all three coexist:

1. **Observability** is always-on. Every driver emits events to the chain by default.
2. **Governance** is on by default in `mode: guide`. Policies start permissive and tighten as the debt ledger surfaces real-world denials worth enforcing.
3. **Automation** (the swarm / self-building product north star) lives downstream of both.

## The three analysis output streams

The event chain feeds three distinct triage categories — this is the shape of the debt ledger:

- **What needs to be fixed** — bugs, hardening candidates, kernel correctness issues. Feeds the issue backlog.
- **What needs determinism** — non-deterministic agent behavior worth pinning down via policy (rewrites, denials, forced outputs). Feeds new `chitin.yaml` rules.
- **Which soul is best at what** — empirical soul-routing data. Correlates `soul_id` in `session_start.payload` with outcome quality. Feeds the souls library as evidence, replacing self-reported `best_stages` with measured ones.

## Dogfooding posture

Dogfooding is permanent, not a phase. Every agent action on this box flows through chitin. The only tunable is cadence (how often we close the loop from chain → policy update).

## Local-first

All capture and gating works fully offline. OTEL emit (post-F4) is opt-in and reaches a configured collector; the kernel-write-survives-OTEL-failure invariant means losing the network never loses the chain.

## What chitin does not do

- It does not run an agent loop. The drivers do.
- It does not ship a cloud SaaS. That is step 5 on the strategic arc, not today.
- It does not replace the agent. It observes and gates.
