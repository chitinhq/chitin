# Driver Allowlist: Kernel-Approved Routing

> Spec-kit entry for ticket `t_7cb9cf49`
> Blocked until: `chitin-kernel drivers list --json` subcommand ships
> (tracked in `t_7c9d02b7`)

## Goal

`_pick_driver.py` currently picks a worker lane based on ELO heuristics encoded
in Python. Policy (who can do what) lives in the dispatch layer, not the kernel.
If `_pick_driver` routes a task to a driver that the kernel would deny, the
worker spins up, hits the gate, and we waste a dispatch cycle.

The kernel already knows the approved driver list (from `chitin.yaml` drivers
section). `_pick_driver` should read the kernel's driver registry — not
hardcode its own.

## Acceptance criteria

- [ ] `chitin-kernel drivers list --json` subcommand exists and outputs
      configured drivers with their identities
- [ ] `_pick_driver` consults the kernel driver list before routing; ELO/router
      logic still picks *which* approved driver, but the approved set comes from
      the kernel, not from `_pick_driver`'s own roster
- [ ] Unapproved driver (hardcoded in `_pick_driver` but missing from kernel
      config) is skipped with a logged warning
- [ ] No regression in dispatch success rate (measured by
      `test_pick_driver.py` green + manual spot-check of recent dispatches)
- [ ] Boundary: kernel subcommand fails or unavailable → `_pick_driver`
      falls back to current hardcoded roster with a **structured warning**
      and stamps the routing decision `approval_source=fallback`
- [ ] Boundary: kernel returns an empty driver list → `_pick_driver`
      falls back to hardcoded roster with a **structured warning**
      and stamps `approval_source=fallback` (does not silently treat
      empty as "no approval constraint")

## Boundaries

- **Kernel unavailable**: fallback to hardcoded roster with structured warning
  + `approval_source=fallback` stamp. Dispatch must continue; it just cannot
  validate against the kernel. The stamp ensures every dispatch is auditable
  for whether kernel approval was actually checked.
- **Empty driver list from kernel**: fallback-with-warning to hardcoded roster,
  stamped `approval_source=fallback`. A misconfigured kernel must not silently
  pass every driver through.
- **Driver in kernel but not in _pick_driver**: logged as info, not a warning —
  the kernel may list drivers that _pick_driver doesn't route to, and that's
  fine.
- **Driver list changes at runtime**: `_pick_driver` re-reads the kernel list
  on every dispatch call (no local cache that can go stale within a poller
  cycle).

## Scope

- New `chitin-kernel drivers list --json` subcommand
  (included here because this spec is blocked until it ships; if `t_7c9d02b7`
  lands first, this criterion is satisfied by that ticket)
- `_pick_driver.py` modifications to consult kernel driver list
- Fallback logic with structured warnings and `approval_source` stamps
- Tests for approved/unapproved/empty-list/kernel-unavailable scenarios

## Boundary coverage test plan

- **empty boundary**: kernel returns `[]`; `_pick_driver` falls back to the
  hardcoded roster, emits a structured warning, and stamps
  `approval_source=fallback`.
- **max boundary**: kernel returns a large driver registry containing every
  configured driver plus unknown future drivers; `_pick_driver` filters to
  locally routable cards without caching stale approvals.
- **error boundary**: `chitin-kernel drivers list --json` is unavailable,
  times out, exits non-zero, or emits malformed JSON; `_pick_driver` falls
  back with the same warning + `approval_source=fallback` stamp.

## Out of scope

- ELO score recalculation or routing algorithm changes (pick *which* driver
  is unchanged; this spec only constrains *which set* is eligible)
- Driver registration in `chitin.yaml` (already exists; this spec reads it,
  doesn't modify it)
- Removing hardcoded driver knowledge from `_pick_driver` entirely (fallback
  still needs it for kernel-unavailable scenarios)
