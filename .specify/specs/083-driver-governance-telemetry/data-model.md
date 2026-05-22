# Phase 1 Data Model: Driver Governance & Telemetry Integrity

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

This feature ships no new datastore. The "data model" is the shape of the
governance evidence the feature restores and unifies: the decision record, the
per-driver status, and the kernel-build identity.

## Entities

### Governance Decision

One kernel evaluation of one agent tool call — the unit of governance
telemetry. Today written to `gov-decisions-*.jsonl` (claude),
`codex-events-*.jsonl` (codex), `events-openclaw-clawta-*.jsonl` (openclaw).

| Field | Meaning | Notes |
|---|---|---|
| `ts` | decision time | RFC3339 |
| `driver` / `agent` | the attributed actor | **MUST be non-empty** (FR-003) |
| `session_id` | the bound work session | links a run's decisions; carries `spec_id` once spec-084 lands |
| `chain_id` | the event-chain id | #861 fixed kernel chain-id stamping |
| `action_type` / `action_target` | what the tool call did | e.g. `shell.exec` |
| `allowed` | the verdict | `true` / `false` |
| `rule_id` | the rule that decided | `codex-post-hoc` is the degraded codex case |
| `reason` | human-readable rationale | |
| `cost_usd`, `tier`, `envelope_id` | governance context | |

**Invariant INV-001**: every Governance Decision has a non-empty `driver` *or*
`agent`. An unattributed decision is a defect (FR-003, SC-003).

### Driver

An agent runtime the system dispatches to.

- **id** — `claude` | `codex` | `copilot` | `gemini` | `hermes` | `openclaw`.
- **governance path** — `hook` (claude, codex, gemini, openclaw-plugin) or
  `kernel-shim` (copilot).
- **governance status** — see below.
- **telemetry sink** — where its decisions land (central / per-session / per-runtime).

### Driver Governance Status

The tri-state a driver resolves to (FR-014). Replaces today's implicit binary.

| State | Definition | Example |
|---|---|---|
| `governed` | a live probe produced an attributed decision | claude, openclaw |
| `ungoverned` | a live probe ran but produced no decision | copilot (pre-fix) |
| `unverified` | no probe could run (CLI unauthenticated / unavailable) | gemini |

State transitions: `unverified → governed|ungoverned` once the CLI can run;
`ungoverned → governed` once the driver's governance path is fixed.

### Telemetry Sink

A store of Governance Decisions. Today three exist; US4 unifies the **read**
side behind one interface — it does not merge the files or add a writer
(constitution §1: only the kernel writes).

| Sink | Holds | US4 disposition |
|---|---|---|
| `gov-decisions-YYYY-MM-DD.jsonl` | central, daily-rotated | the canonical sink; codex routed here (Decision 2) |
| `codex-events-<session>.jsonl` | per codex session, post-hoc | read by the unified interface; historical |
| `events-openclaw-clawta-*.jsonl` | per OpenClaw runtime | read by the unified interface |

### Kernel Build

The running kernel binary's identity, for staleness detection (US2).

- **binary** — `~/.local/bin/chitin-kernel` (and the legacy `chitin`).
- **source revision** — the merged `main` commit the kernel *should* reflect.
- **staleness** — `binary.revision < source.revision` ⇒ stale ⇒ surfaced via
  `chitin health` (FR-011).

### Hook

The per-driver mechanism routing a tool call through the kernel.

- **scope** — `global` (`~/.<agent>/…`) or `project` (`<repo>/.<agent>/…`).
- **liveness** — installed-and-fires vs installed-but-ignored-by-CLI. Only a
  live probe distinguishes them — the basis of the rebuilt `chitin doctor`
  (FR-012).

## Relationships

```text
Driver ──has── GovernancePath ──emits──▶ Governance Decision ──lands in──▶ Telemetry Sink
  │                                            │
  └──resolves to── Driver Governance Status     └──attributed to── Driver/Agent (INV-001)

Kernel Build ──governs──▶ all Drivers ;  staleness ⇒ regressed governance (US1 root cause)
```
