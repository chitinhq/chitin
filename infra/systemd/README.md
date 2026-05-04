# Chitin systemd units (slice 7)

User-mode systemd units that run the autonomous swarm: worker daemon
+ dispatcher timer. Per-user — no sudo required to install.

## What's here

| Unit | Type | Purpose |
|------|------|---------|
| `chitin-worker.service` | long-running | Temporal worker, polls `chitin-worker-q`, runs activities. |
| `chitin-dispatcher.service` | oneshot | Single tick: reads backlog, picks next ready entry, submits workflow, runs apply step. |
| `chitin-dispatcher.timer` | timer | Fires the dispatcher every 5 minutes. |
| `chitin-researcher.service` | oneshot | Periodic research tasks, runs researcher script. |
| `chitin-researcher.timer` | timer | Fires the researcher every 4 hours. |
| `chitin-swarm-rollup.service` | oneshot | Daily swarm-health rollup: derives metrics from `tmp/result-swarm-*.json` + dispatcher journalctl, posts a digest to Slack. |
| `chitin-swarm-rollup.timer` | timer | Fires the rollup once per day. |
| `chitin-groomer.service` | oneshot | Daily groomer: reads roadmap candidates, drafts up to N (default 1) `in_design` backlog entries from arxiv-source candidates. The existing groom-pass.ts then classifies tier/file/loc. |
| `chitin-groomer.timer` | timer | Fires the groomer once per day. |
| `chitin-lessons.service` | oneshot | Daily lessons-learned extractor: scans merged swarm/* PRs, distills a one-sentence lesson per, appends to `docs/swarm-lessons.md`. The dispatcher prepends recent lessons to programmer prompts. |
| `chitin-lessons.timer` | timer | Fires the lessons extractor once per day. |
| `chitin-debt-curator.service` | oneshot | Daily debt-curator scan: greps the repo for TODO/FIXME/HACK/XXX markers, dedups, appends new finds to `docs/debt-ledger.md` at severity:'low' (operator promotes). |
| `chitin-debt-curator.timer` | timer | Fires the debt-curator once per day. |
| `chitin-alarm-feeder.service` | oneshot | Daily alarm-feeder: reads rollup `alarms[]`, dedups against existing `investigate-*` backlog entries, drafts in_design entries with role:researcher. Closes §7 telemetry → backlog flywheel. |
| `chitin-alarm-feeder.timer` | timer | Fires the alarm-feeder once per day. |
| `chitin-stale-doc-detector.service` | oneshot | Daily stale-doc detector: scans `docs/**/*.md` for project-relative path refs that no longer exist in the working tree, files debt-ledger entries at severity:'low'. Tech-writer's debt-detection half. |
| `chitin-stale-doc-detector.timer` | timer | Fires the stale-doc detector once per day. |
| `chitin-agent-unlock.service` | oneshot | Auto-recovery for agents lockdown'd by infrastructure-only denials (envelope-closed cascades, etc). Skips agents with any policy-violation history — those stay operator-only. |
| `chitin-agent-unlock.timer` | timer | Fires the agent-unlock check every 15 min. |

## Install

```bash
mkdir -p ~/.config/systemd/user
cp infra/systemd/*.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now chitin-worker
systemctl --user enable --now chitin-dispatcher.timer
systemctl --user enable --now chitin-researcher.timer
systemctl --user enable --now chitin-swarm-rollup.timer
systemctl --user enable --now chitin-lessons.timer
systemctl --user enable --now chitin-debt-curator.timer
systemctl --user enable --now chitin-alarm-feeder.timer
systemctl --user enable --now chitin-stale-doc-detector.timer
systemctl --user enable --now chitin-groomer.timer
systemctl --user enable --now chitin-agent-unlock.timer
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
journalctl --user -u chitin-researcher -f
journalctl --user -u chitin-swarm-rollup -f
journalctl --user -u chitin-lessons -f
journalctl --user -u chitin-debt-curator -f
journalctl --user -u chitin-alarm-feeder -f
journalctl --user -u chitin-stale-doc-detector -f
journalctl --user -u chitin-groomer -f

# Status
systemctl --user status chitin-worker
systemctl --user status chitin-researcher
systemctl --user list-timers --all

# Pause the swarm (stop dispatcher; worker keeps polling)
systemctl --user stop chitin-dispatcher.timer
systemctl --user stop chitin-researcher.timer

# Hard stop everything
systemctl --user stop chitin-dispatcher.timer chitin-researcher.timer chitin-swarm-rollup.timer chitin-worker
```

## Manual one-shot

```bash
# Dispatch a single tick on demand (no timer)
systemctl --user start chitin-dispatcher.service
systemctl --user start chitin-researcher.service

# Or run dispatcher directly (dry-run available)
cd ~/workspace/chitin
pnpm exec tsx apps/temporal-worker/src/dispatcher.ts --dry-run
pnpm exec tsx apps/temporal-worker/src/dispatcher.ts
pnpm exec tsx apps/temporal-worker/src/researcher.ts
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

## Slack notifications (optional)

The dispatcher posts events to a Slack incoming webhook so the operator
can stay aware of swarm activity without tailing journalctl. If the
webhook URL is unset, every notify call is a no-op — Slack is purely
opt-in.

**Setup:**

1. In Slack, create an incoming webhook (Apps → "Incoming Webhooks" →
   New). Pick the channel that should receive swarm events. Copy the
   `https://hooks.slack.com/services/T.../B.../...` URL.
2. Drop the URL into a per-user systemd environment file:
   ```bash
   mkdir -p ~/.config/systemd/user
   cat >> ~/.config/systemd/user/chitin.env <<'EOF'
   CHITIN_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/T.../B.../...
   # Optional: also post when a tick has nothing to do (defaults to silent).
   # CHITIN_SLACK_NOTIFY_IDLE=1
   EOF
   chmod 600 ~/.config/systemd/user/chitin.env
   ```
3. The unit files already include `EnvironmentFile=-%h/.config/systemd/user/chitin.env`,
   so `systemctl --user daemon-reload` picks it up. The leading dash
   means the file is optional — if it's missing, the unit still starts.

**What gets posted:**

| Event | Trigger | Example |
|-------|---------|---------|
| `dispatch_start` | entry picked, workflow successfully submitted | `🦞 swarm dispatch start <entry-id>` (only fires after `client.workflow.start()` returns; submit failures emit `dispatch_error` instead) |
| `dispatch_complete` | workflow + apply finished | `✅ <entry-id> — PR opened — #N` (or `🟢` `⚪` `❌` `⚠️` depending on outcome — apply failures render `⚠️` and link the operator to the paired `dispatch_error`) |
| `dispatch_error` | submit / workflow / apply failure (incl. silent `gh pr create` failure after a successful push) | `🚨 dispatch error <entry-id> at <stage>` with the failure's `error.message` (truncated to 2000 chars; no stack trace — stack contents can leak sensitive paths through Slack retention) |
| `dispatch_idle` | tick had nothing to do (no ready entry, or workflow already in flight) | `💤 dispatcher idle — <reason>`. Only emitted when `CHITIN_SLACK_NOTIFY_IDLE=1` (off by default since most ticks are idle) |

Failures during posting (timeout, 5xx) are logged at warn level and
swallowed — visibility is nice-to-have, dispatch correctness comes
first.

## Pause for governance changes

Slice 6 verified that the swarm cannot edit `chitin.yaml` (the
`no-governance-self-modification` rule blocks all agents). But if you
want a hard stop on the swarm during an incident, the timer-stop above
takes effect within seconds.
