# 048 — Octi HA migration template (tripwired `start-dev` → HA cluster)

> Parent: spec 040 (Octi scaffolding §R10).
> Status: **template** — not actionable until tripwire fires.

## Summary

`temporal server start-dev` (single-binary, SQLite-backed) is the
right deployment shape for 3-7 agents. The day Octi crosses a
measurable production-grade boundary, we owe ourselves a real HA
Temporal cluster: Postgres-backed persistence, Elasticsearch for
visibility, and the four Temporal services (Frontend, History,
Matching, Worker) running on at least three nodes.

This spec defines the **tripwire conditions** and the **migration
runbook** so that when the boundary is crossed, the migration is
a known one-week project, not an emergency.

This spec is templated — it does NOT mandate the migration today.
Operator runs `octi ha tripwire-status` to see how close we are.

## Tripwire conditions (any one fires)

| # | Condition | How measured |
|---|---|---|
| T1 | Sustained >7 concurrent workflows for 7 days | `octi snapshot` (spec 041 §R10) histogram |
| T2 | Any customer-facing workflow ships | New workflow's spec carries `customer_facing: true` in front-matter |
| T3 | Sustained workflow p99 latency >5 seconds for 24 hours | OctiEvent stream (spec 041) → derived metric |
| T4 | Octi worker process restart >3 times in 1 hour | Worker systemd logs |
| T5 | SQLite persistence file exceeds 5 GB | `du -sh ~/.octi/dev.db` |

If a tripwire fires, the operator opens this spec to a ratified
state and the migration becomes work.

## File-system scope (when activated)

### MAY write under

- `swarm/octi/deploy/ha/` — HA deployment artifacts
  - `docker-compose.ha.yml` — local-multi-node HA emulation
  - `terraform/` — production HA cluster IaC (later, if needed)
  - `migrations/sqlite-to-postgres/` — one-shot migration tooling
- `swarm/bin/octi-ha-migrate` — migration CLI
- `swarm/bin/octi-ha-status` — HA cluster health check
- `~/.octi/ha.toml` — HA-mode worker config (mirrors
  `~/.octi/worker.toml` shape)
- `.specify/specs/048-octi-ha-migration-template/**`

### MUST NOT write under

- `chitin.yaml` (kernel policy unchanged — HA cluster is just a
  bigger Temporal, same gate boundary)
- `swarm/octi/workflows/`, `swarm/octi/activities/` (workflow code
  is backend-agnostic by Temporal SDK design — no code changes for
  HA cutover)

## Goal (when activated)

`octi ha migrate --plan` produces a step-by-step migration
checklist. `octi ha migrate --execute` runs it. Workflows in flight
during cutover are paused at the workflow boundary, persisted state
is migrated SQLite → Postgres, and workflows resume on the HA
cluster with no data loss. Total downtime budget: ≤ 30 minutes.

## Requirements (when activated)

### R1 — HA cluster shape

Minimum production deployment:

| Service | Replica count | Purpose |
|---|---|---|
| Frontend | 3 | gRPC entry, request routing |
| History | 3 | Workflow event-history shards |
| Matching | 3 | Task queue distribution |
| Worker | 3 | System workflows (replication, schedule, etc.) |
| Postgres | 3 (primary + 2 replicas) | Persistence backend |
| Elasticsearch | 3 nodes | Visibility / search |

For local emulation (CI, dev box): `docker-compose.ha.yml` runs the
same topology in containers.

### R2 — Backend-agnostic workflow code (verified)

Workflow code (`swarm/octi/workflows/`) and Activity code
(`swarm/octi/activities/`) MUST work unchanged on either backend.
This invariant is asserted by CI: the same test corpus runs
against `start-dev` and against a `docker-compose.ha.yml` cluster
each PR.

### R3 — Migration plan (when executed)

1. **Snapshot** — `octi ha migrate --snapshot` captures the
   current SQLite state to `~/.octi/migration-snapshot-<ts>.db`
2. **Pause** — `octi ha migrate --pause-workflows` flags every
   running workflow with a `migration_paused` signal; workflows
   transition to `paused` state at the next workflow boundary
3. **Export** — convert SQLite shape to Postgres shape via
   `migrations/sqlite-to-postgres/`
4. **Switch** — point worker config at the HA cluster
5. **Resume** — workflows un-pause, continue from their last
   persisted event
6. **Verify** — `octi ha migrate --verify` runs a smoke workflow
   end-to-end on the new cluster, asserts replay matches
7. **Decommission** — only after 7 days of stable HA operation,
   the SQLite-backed `start-dev` instance is shut down

### R4 — Rollback

Within 7 days of cutover, `octi ha migrate --rollback` reverses
steps 4-5 (point back at SQLite, resume from snapshot). After
7 days, rollback becomes a from-scratch operation and this spec
flags the boundary loud.

### R5 — Worker config

`~/.octi/ha.toml` mirrors `worker.toml` with HA-specific fields:

```toml
[temporal]
target_host = "octi-ha.internal:7233"
namespace   = "default"
tls_enabled = true
tls_cert    = "~/.octi/tls/client.crt"
tls_key     = "~/.octi/tls/client.key"

[shard_balancing]
strategy = "consistent_hash"
shards   = 4
```

### R6 — Visibility via Elasticsearch (not bypassed)

Spec 041 already requires audit reconstruction to come from
OctiEvent mirror, not Temporal visibility. HA cluster's
Elasticsearch is available for fast workflow search BUT MUST NOT
become a load-bearing audit surface. Invariant 7 still holds.

### R7 — TLS-required

HA cluster requires mTLS between worker and frontend.
Certificates managed via existing chitin trust store
(`~/.chitin/trust/`).

### R8 — Chitin gate floor preserved

HA cutover does not change Activity → chitin gate flow. Every
Activity still gates per spec 040 §R7. The HA Temporal cluster is
fatter plumbing, not new policy.

### R9 — Capacity test

Before `--execute`, `octi ha migrate --capacity-test` runs the
production workload (shadow) against the HA cluster for 24 hours.
Asserts:
- p99 workflow latency < production budget
- No event loss vs `start-dev` shadow
- Worker memory steady-state < 2 GB

### R10 — Tripwire monitor

`swarm/bin/octi-ha-tripwire-monitor` runs as a Temporal Schedule
(daily) checking T1-T5. If any tripwire shows ≥80% of its
threshold for 7 days, the monitor opens an operator-attended
issue.

## Acceptance criteria (when activated)

1. `octi ha tripwire-status` reports current values for T1-T5 and
   distance to threshold.
2. Tripwire monitor surfaces an operator issue when any condition
   hits 80% threshold.
3. `octi ha migrate --plan` produces a step-by-step checklist.
4. `docker-compose.ha.yml` brings up a 6-node Temporal HA cluster
   locally; smoke workflow runs to completion.
5. CI runs the full test corpus against both `start-dev` and the
   compose-HA cluster; all tests pass on both.
6. `--snapshot` → `--pause` → `--export` → `--switch` → `--resume`
   completes on a fixture workload with ≤ 30 min downtime.
7. `--rollback` within 7 days restores `start-dev` operation
   cleanly.
8. mTLS verified end-to-end via worker → frontend gRPC handshake
   capture.
9. Chitin gate invariant: random sample of 100 Activity calls
   post-cutover, all show preceding `chitin.GateEval` decisions
   in `gov-decisions-*.jsonl`.
10. Invariant 7 (replay from OctiEvent mirror alone) still holds
    on HA cluster — `octi-replay-from-mirror` works unchanged.

## Test coverage (when activated)

- `swarm/octi/deploy/ha/tests/cluster_smoke_test.go` — **e2e**:
  AC4
- `swarm/octi/deploy/ha/tests/migration_e2e_test.go` — **e2e**:
  AC6, AC7
- `swarm/octi/deploy/ha/tests/cross_backend_corpus_test.go` —
  **e2e**: AC5 (runs full corpus on both backends)
- `swarm/octi/e2e/tripwire_monitor_test.go` — **e2e**: AC2

## Invariants

- **I1**: workflow code is backend-agnostic; same code runs on
  `start-dev` and HA without changes.
- **I2**: invariant 7 (replay from OctiEvent mirror) is preserved
  on HA; Elasticsearch is convenience, not audit.
- **I3**: tripwire monitor is the single source of truth for "is
  HA needed yet" — opinion / vibes don't trigger migration.
- **I4**: downtime budget is ≤ 30 minutes for cutover, ≤ 1 hour
  for rollback.
- **I5**: chitin gate flow is unchanged; HA is plumbing, not
  policy.

## Out of scope (until activated)

- This entire spec body — until a tripwire fires, this is a
  template. Operator's standing instruction: do not start
  implementation work on 048 until at least one tripwire is at
  ≥80% threshold.
- Multi-region HA (cross-AZ within a region is in scope; multi-
  region is a later spec)
- Self-hosted Elasticsearch vs managed (OpenSearch, AWS) —
  decision deferred until activation

## References

- Parent: spec 040 §R10
- Temporal HA deployment docs:
  https://docs.temporal.io/self-hosted-guide
- Invariant 7 source: ratification thread 17 msg 7702
- Chitin trust store: `~/.chitin/trust/`
