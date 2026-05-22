# Contracts: Driver Governance & Telemetry Integrity

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

The interface contracts this feature establishes or stabilises. These are the
behavioural promises downstream consumers (watchdogs, the console, the
orchestrator's self-improvement loop, operators) may rely on.

## C1 — Governance Decision record

The record every driver's governance path emits. The contract: **schema parity
across drivers** — a consumer reads one shape regardless of source driver.

- MUST include `ts`, an attributed actor (`driver` or `agent`, non-empty),
  `action_type`, `allowed`, `rule_id`, `reason`.
- SHOULD include `session_id`, `chain_id`, `cost_usd`, `tier`, `envelope_id`.
- An emitted record with neither `driver` nor `agent` violates the contract
  (INV-001 / FR-003).
- Decisions from every driver MUST be reachable through the unified read
  interface (C3); codex decisions MUST additionally reach the central sink
  (FR-005).

## C2 — `chitin doctor` per-driver verdict

`chitin doctor` reports one verdict per driver. The contract: **the verdict
reflects live behaviour, not file state.**

| Verdict | Promise |
|---|---|
| `OK` / governed | a live probe produced an attributed Governance Decision |
| `FAIL` / ungoverned | a live probe ran and produced none |
| `UNVERIFIED` | the driver's CLI could not run; governance is unknown |

- MUST NOT report `OK` on a hook-file marker check alone (FR-012).
- MUST report `OK` for a driver governed by a global (non-project) hook (FR-013).
- The verdict MUST match an independent probe (quickstart.md) for every driver
  (SC-005).

## C3 — Unified telemetry query interface

A single read interface over the three telemetry sinks
(`gov-decisions-*.jsonl`, `codex-events-*.jsonl`,
`events-openclaw-clawta-*.jsonl`).

- MUST return Governance Decisions for **all** drivers from one query — no
  caller needs sink-specific knowledge (FR-004, SC-004).
- Is **read-only** — it does not write the chain or merge sink files
  (constitution §1: only the kernel writes).
- MUST support filtering by driver, by time window, and by verdict.

## C4 — Kernel-redeploy outcome

The `install-kernel.sh` redeploy makes these promises (US2):

- On success: the running kernel binary reflects the merged `main` source.
- On failure: the prior working kernel is left in place — **no governance
  outage** — and the failure is surfaced to the operator (not only logged).
- Kernel staleness (running binary older than merged source) is detectable via
  `chitin health` (FR-011).
- The redeploy's version-control step uses a single explicit ref
  (`merge --ff-only origin/main`) and cannot fail on multiple merge heads.

## C5 — Driver dispatch is governed (orchestrator)

- An orchestrator work unit dispatched into a fresh worktree MUST execute under
  governance — the worktree is not a blind spot (FR-008, SC-008).
- A work unit with no resolvable governance path MUST be flagged, not run
  silently ungoverned.
