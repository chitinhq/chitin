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

### Router circuit breaker

If `~/.openclaw/logs/clawta-poller.log` shows `_pick_driver.py` timeouts
repeatedly over a short window, treat the OpenClaw router as degraded and trip
the poller-side circuit breaker.

Recommended v1 procedure:

- If `_pick_driver.py timed out` fires 3 times in 10 minutes, set
  `CLAWTA_ROUTER_MODE=deterministic` in the poller environment and restart the
  poller.
- If you need to pin all routing to one lane during the incident, also set
  `CLAWTA_FORCE_DRIVER=codex` or `CLAWTA_FORCE_DRIVER=gemini` before the
  restart.
- Remove the override after the OpenClaw gateway is healthy again so routing
  can return to the normal `_pick_driver.py` mode.

These `CLAWTA_*` env vars are consumed by `swarm/bin/clawta-poller`, which
passes them through to `_pick_driver.py` as `ROUTER_MODE` and `FORCE_DRIVER`.

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

## Diagnostics

For live worker/process inspection, prefer the narrow process view:

```bash
ps -o pid,ppid,stat,rss,etime,command -C codex -C gemini -C chitin-kernel -C lobster
```

This keeps diagnostics readable and avoids dumping long ticket or diff text by
default. Worker launchers now pass large prompts through stdin where the driver
supports it; if deeper inspection is needed, check the relevant log file rather
than relying on full argv snapshots.
