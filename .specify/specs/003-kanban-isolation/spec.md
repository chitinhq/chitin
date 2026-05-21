# Kanban Mutation Isolation: Single Sanctioned Channel


**Status**: shipped (40e1356, #691)
> Spec-kit entry for ticket `t_77f5b407`
> Source spec: `docs/superpowers/specs/2026-05-13-isolate-swarm-kanban-mutations.md` (merged via #580)

## Goal

Make `scripts/kanban-flow` the only sanctioned mutation channel into the kanban
DB from any `swarm/*` code path; record forensic evidence of any bypass.

## Acceptance criteria

- [ ] `scripts/check-swarm-kanban-isolation.sh` exists; CI-wired via
      `check-swarm-kanban-isolation` make target or CI step; greps
      `swarm/` for direct write SQL against the kanban DB; rejects on hit
      (excluding `swarm/tests/` and `kanban-flow` itself)
- [ ] Mutation audit: `kanban-flow` wraps every write connection with a
      helper that sets a tagged `application_id` and logs the mutation
      (table, op, task_id, timestamp) to `kanban_mutations_log`.
      SQLite triggers are best-effort only — `application_id` is
      database-level, not per-connection, so the helper provides the
      reliable provenance layer.
- [ ] `kanban-flow` creates `kanban_mutations_log` on first write
      connection (not fail if absent)
- [ ] All identified mutation paths in `swarm/` go through `kanban-flow`
      (verified by the isolation check script passing in CI)

## Boundaries

- **No mutations table**: if `kanban_mutations_log` doesn't exist,
  `kanban-flow` creates it on first write (not fail)
- **Read-only paths exempt**: `SELECT` queries from `swarm/` are fine; only
  `INSERT/UPDATE/DELETE` must go through `kanban-flow`
- **Test exemptions**: `swarm/tests/` may write directly during test runs;
  the isolation check must exclude test files
- **kanban-flow itself**: the isolation check must not flag `kanban-flow`'s
  own SQL writes (it IS the sanctioned channel)
- **application_id**: documented constant in `kanban-flow`; collision with
  other apps is prevented by registration convention, not enforced at the
  SQLite level

## Scope

- `swarm/` directory mutation audit
- Mutation helper + log table in `kanban-flow`
- CI wiring for `check-swarm-kanban-isolation.sh`

## Out of scope

- Hermes bridge mutation implementation, unless audit finds a direct-write
  bridge path; implementation must verify current bridge writes route through
  `kanban-flow` before excluding them from the migration work
- Dashboard read paths (SELECT-only)
- Rewriting test fixtures to use `kanban-flow` (separate effort)
- SQLite trigger-based forensic provenance (unreliable for per-connection
  identity; helper-based logging is the authoritative layer)
