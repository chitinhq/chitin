# Chitin Layer Contracts v1

Locked 2026-04-29. Driver-constraint clause restated 2026-05-13 to
match shipping reality (the 2026-04-30 addendum's deferral codified
into the contract instead of pending against it). These are the
architectural invariants that hold across the chitin system. New
code, specs, and plans must respect them.

## 1. Kernel Authority

All execution passes through `gov.Gate.Evaluate`. The kernel is the
only enforcement point. No driver, orchestrator, or adapter may
bypass it.

## 2. Driver Constraint

An execution request carries `allowed_drivers` as a typed, schema-
validated field on `ExecutionRequest` (`libs/contracts/src/execution-
request.schema.ts`). The schema enforces:

- non-empty (rejects `[]`)
- closed enum (rejects unknown driver ids; rejects ToS-disallowed
  drivers, e.g., raw `claude-code` as a worker subagent)

The dispatcher constructs the field; the orchestrator picks within
it (`swarm/workflows/_pick_driver.py` ranks by capability + cost
from the agent-cards). The orchestrator cannot expand to drivers
outside the enum (the schema rejects the request at parse time) and
cannot pick a driver absent from `allowed_drivers` (the schema
constrains `selected_driver` to the same enum).

The kernel remains authoritative: all execution performed by any
selected driver is still subject to `gov.Gate.Evaluate` at the leaf
hook surface. What this clause guarantees is upstream typing +
schema-enforced narrowing; what `gov.Gate` guarantees is downstream
enforcement. Together they form the authority chain.

### Deferred (active kernel narrowing)

The 2026-04-30 local-worker addendum deferred the active narrowing
path (`chitin-kernel task validate <req.json>` that asks the kernel
to shrink `allowed_drivers` from policy before dispatch). That path
remains deferred — the schema-typed contract is sufficient for
today's swarm pipeline. When ledger signal shows the cost-ranked
picker drifting from policy intent, wire the active primitive: a
`chitin-kernel task validate` subcommand that takes an
`ExecutionRequest`, applies the policy, and returns a possibly-
shrunk `allowed_drivers`. The contract above stands either way:
"policy can shrink, never expand."

## 3. Routing Scope

Routing within the orchestrator optimizes for capacity (latency,
availability, hardware) within the `allowed_drivers` set. Routing
cannot expand the set. With the active narrowing path deferred,
routing also cannot shrink it beyond schema-imposed constraints —
the shrink is the orchestrator's cost-and-capability ranking,
operating within the schema-typed enum.

## 4. Aggregation Role

The event chain informs future policy via post-hoc derivation and
replay. It does not affect live execution. OTEL emit is a projection
of the chain, never its source.
