# Swarm Runtime Guards

These helpers are the repo-audited runtime guardrails around the autonomous
swarm loop. Dispatch now runs through the tracked `swarm-controller` path, not
through the retired poller cron.

## Ownership

- `swarm/bin/swarm-controller` owns dispatch.
- `swarm/bin/clawta-blocked-escalator` owns blocked-ticket escalation.
- `swarm/bin/clawta-stale-worker-watchdog` owns stale `in_progress` cleanup.
- `swarm/bin/install-swarm-controller-cron.sh` installs the tracked controller
  loop; the old bundled poller installer path is retired.

The guard scripts still rely on the same process/log surfaces, but the
controller loop is now the canonical dispatch scheduler.

## Guard behavior

### Router circuit breaker

If `~/.openclaw/logs/swarm-controller.log` shows `_pick_driver.py` timeouts
repeatedly over a short window, treat the OpenClaw router as degraded and trip
the controller-side circuit breaker.

Recommended v1 procedure:

- If `_pick_driver.py timed out` fires 3 times in 10 minutes, set
  `ROUTER_MODE=deterministic` in the controller environment and restart the
  controller loop.
- If you need to pin all routing to one lane during the incident, also set
  `FORCE_DRIVER=codex` or `FORCE_DRIVER=gemini` before the
  restart.
- Remove the override after the OpenClaw gateway is healthy again so routing
  can return to the normal `_pick_driver.py` mode.

These env vars are consumed by `_pick_driver.py`; `swarm-controller` passes
them through from its process environment.

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

## Scheduling

The old bundled poller schedule retired with the legacy poller. Verify the
current controller and guard cadences from their
tracked installers before relying on them operationally.

## Diagnostics

For live worker/process inspection, prefer the narrow process view:

```bash
ps -o pid,ppid,stat,rss,etime,command -C codex -C gemini -C chitin-kernel -C lobster
```

This keeps diagnostics readable and avoids dumping long ticket or diff text by
default. Worker launchers now pass large prompts through stdin where the driver
supports it; if deeper inspection is needed, check the relevant log file rather
than relying on full argv snapshots.
