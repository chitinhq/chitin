# Swarm Runtime Guards

These helpers are the repo-audited runtime guardrails around the autonomous
swarm loop. They are intended to run under **OpenClaw cron**, not as hidden
operator-local shell glue.

## Ownership

- `swarm/bin/clawta-poller` owns dispatch.
- `swarm/bin/clawta-blocked-escalator` owns blocked-ticket escalation.
- `swarm/bin/clawta-stale-worker-watchdog` owns stale `in_progress` cleanup.
- `swarm/bin/install-clawta-poller.sh openclaw` registers the OpenClaw cron
  jobs for all three.

Systemd remains a fallback only for `clawta-poller` on boxes without
OpenClaw. The guard scripts depend on OpenClaw-era process/log surfaces, so
their canonical owner is the OpenClaw cron substrate.

## Guard behavior

### Blocked escalator

Command: `clawta-blocked-escalator`

- Scans `blocked` tickets whose assignee is not `red`.
- Reassigns them to `red`.
- Adds a kanban comment explaining that blocked swarm work is now
  operator-owned until unblocked, re-groomed, or manually repaired.

Dry-run:

```bash
clawta-blocked-escalator --dry-run --json
```

### Stale worker watchdog

Command: `clawta-stale-worker-watchdog`

A ticket is blocked and escalated to `red` only when all of these are true:

- status is `in_progress`
- no `pr_opened` task event exists
- no matching active worker process is still running
- age is at least `CLAWTA_STALE_AFTER_SECONDS` (default `2700`, 45 minutes)
- dispatch log quiet time is at least `CLAWTA_QUIET_AFTER_SECONDS`
  (default `1200`, 20 minutes)

The watchdog blocks via `scripts/kanban-flow block`, then reassigns to `red`
and leaves an audit comment explaining the exact age/quiet values used.

Dry-run:

```bash
clawta-stale-worker-watchdog --dry-run --json
```

## OpenClaw cron schedules

The installer registers these jobs:

- `clawta-kanban-poller` every `2m`
- `clawta-blocked-escalator` every `10m`
- `clawta-stale-worker-watchdog` every `10m`

Each cron job is idempotent and responds with a closing `ok` token so OpenClaw
records the run as successful.
