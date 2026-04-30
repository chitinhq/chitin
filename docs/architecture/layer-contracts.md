# Chitin Layer Contracts v1

Locked 2026-04-29. These are the architectural invariants that hold across the chitin system. New code, specs, and plans must respect them.

## 1. Kernel Authority

All execution passes through `gov.Gate.Evaluate`. The kernel is the only enforcement point. No driver, orchestrator, or adapter may bypass it.

## 2. Driver Constraint

The kernel exposes an `AllowedDrivers` primitive that returns the kernel-derived feasible driver set for a given execution request. The orchestrator must consume this primitive; it cannot derive its own allowed set or override the result.

The kernel remains authoritative: all execution performed by any selected driver is still subject to `gov.Gate.Evaluate`.

## 3. Routing Scope

Routing within the orchestrator optimizes for capacity (latency, availability, hardware). Routing cannot expand the allowed set returned by `AllowedDrivers`.

## 4. Aggregation Role

The event chain informs future policy via post-hoc derivation and replay. It does not affect live execution. OTEL emit is a projection of the chain, never its source.
