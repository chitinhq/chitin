# Chitin systemd units (slice 7)

User-mode systemd units that run the autonomous swarm: worker daemon
+ dispatcher timer. Per-user — no sudo required to install.

## What's here

| Unit | Type | Purpose |
|------|------|---------|
| `chitin-worker.service` | long-running | Temporal worker, polls `chitin-worker-q`, runs activities. |
| `chitin-dispatcher.service` | oneshot | Single tick: reads backlog, picks next ready entry, submits workflow, runs apply step. |
| `chitin-dispatcher.timer` | timer | Fires the dispatcher every 5 minutes. |

## Install

```bash
mkdir -p ~/.config/systemd/user
cp infra/systemd/*.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now chitin-worker
systemctl --user enable --now chitin-dispatcher.timer
```

To survive logout (start at boot):

```bash
sudo loginctl enable-linger $USER
```

## Operate

```bash
# Live logs
journalctl --user -u chitin-worker -f
journalctl --user -u chitin-dispatcher -f

# Status
systemctl --user status chitin-worker
systemctl --user list-timers --all

# Pause the swarm (stop dispatcher; worker keeps polling)
systemctl --user stop chitin-dispatcher.timer

# Hard stop everything
systemctl --user stop chitin-dispatcher.timer chitin-worker
```

## Manual one-shot

```bash
# Dispatch a single tick on demand (no timer)
systemctl --user start chitin-dispatcher.service

# Or run dispatcher directly (dry-run available)
cd ~/workspace/chitin
pnpm exec tsx apps/temporal-worker/src/dispatcher.ts --dry-run
pnpm exec tsx apps/temporal-worker/src/dispatcher.ts
```

## What gets dispatched

The dispatcher's invariants:

- **At most one swarm workflow in flight at a time.** If any workflow
  with id matching `swarm-*` is RUNNING in Temporal, the tick exits
  without dispatching. Sequential by design — no parallel runs eating
  the queue out of order.
- **Each backlog entry dispatched at most once per origin.** If a
  branch matching `swarm/swarm-<entry-id>-*` exists on origin (open
  or merged PR), the entry is skipped. Re-dispatch requires deleting
  the branch.
- **T5 entries are never dispatched** — those are human-only
  (governance changes, irreversible decisions, ambiguous strategy).

## Tier → driver routing (slice 7c)

| Tier | Driver | Wall timeout | Cost |
|------|--------|--------------|------|
| T0 | `local-qwen` (qwen3-coder:30b) | 180s | $0 (local) |
| T1 | `copilot` (GPT-4.1 free) | 240s | $0 |
| T2 | `claude-code-headless` (haiku) | 360s | low |
| T3 | `claude-code-headless` (sonnet) | 600s | medium |
| T4 | `claude-code-headless` (opus) | 600s | high |

## Failure modes

- **Workflow hangs:** wall_timeout SIGKILL (slice 7a) propagates to
  the process group, agent dies within ~1s of the timer, activity
  returns `exit_code=-1`. Apply step skips PR. Operator sees in
  `gov-decisions` chain + Temporal UI.
- **Apply step fails (push or PR open):** worktree may have unpushed
  commits; envelope file persists in `tmp/result-<wfid>.json`. Operator
  re-runs `apply-workflow-result.ts --result <path> --apply`.
- **Worker crashes:** `Restart=on-failure` brings it back in 10s.
- **Dispatcher tick errors:** systemd records exit code, next timer
  tick retries.

## Pause for governance changes

Slice 6 verified that the swarm cannot edit `chitin.yaml` (the
`no-governance-self-modification` rule blocks all agents). But if you
want a hard stop on the swarm during an incident, the timer-stop above
takes effect within seconds.
