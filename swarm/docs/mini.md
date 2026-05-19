# Mini — operator README

`mini` is the slice-1 deliverable of spec 038. It is the persistent
kitty + Claude Code + Discord bridge that proves the interface seam
before the Octi workflow engine (slices 2-4) is built on top.

**Slice 1 surface**: `mini open / status / nudge / watch / stop`.

## Install

```sh
swarm/bin/install-mini.sh
```

What it does:
- Symlinks `swarm/bin/mini`, `swarm/bin/octi`, and `swarm/bin/octi-worker`
  into `~/.local/bin/`.
- Creates `~/.swarm/octi/` as the default state root.
- Verifies kitty is installed and remote control is enabled.
- Warns if `OCTI_DISCORD_WEBHOOK_URL` is unset (`watch` requires one).

## Required kitty config

```
# ~/.config/kitty/kitty.conf
allow_remote_control yes
listen_on unix:/tmp/kitty-rc
```

Restart kitty after editing.

## Quickstart

```sh
mini open --goal "Fix the dispatch race in clawta-poller"
# → opens a new kitty tab running `claude --dangerously-skip-permissions`
#   inside ~/workspace/chitin-octi-<goal-id>/
#   on branch octi/<goal-id> off origin/main
#   with the initial prompt instructing Claude to write status.json

mini status              # read status.json (auto-discovers goal-id from cwd)
mini nudge --message "/please also add a regression test"
mini watch &             # filtered Discord tailer
mini stop                # close window, mark state=failed
```

Recovery:

```sh
mini open --recovery <goal-id>
# re-binds to the existing state dir + relaunches kitty if window is gone
```

## Status contract

Claude is instructed by the initial prompt to write
`.swarm/octi/<goal-id>/status.json` periodically with six fields:

| Field | Type | Notes |
|-------|------|-------|
| `state` | string enum | `starting`, `working`, `blocked`, `verifying`, `done`, `failed`, `needs_review` |
| `updated_at` | unix seconds | Primary liveness signal — not terminal output |
| `summary` | string | one-line current action |
| `next` | string | one-line next intended step |
| `blockers` | list[string] | empty list when nothing blocks |
| `verify` | string | shell command exiting 0 iff goal satisfied |

Slice 2 (`octi`) runs `verify` independently and persists the result to
`controller_verdict.json` — see `swarm/docs/octi.md`. Slice 1 (`mini`)
only reports.

## Env vars

| Var | Purpose | Default |
|---|---|---|
| `OCTI_DISCORD_WEBHOOK_URL` | Primary Discord webhook (`#octi`) | unset → `watch` errors |
| `OCTI_DISCORD_SWARM_WEBHOOK_URL` | `#swarm` coordination summary webhook | unset → skip swarm posts |
| `OCTI_OPERATOR` | Identity used for input lease `holder` | `getpass.getuser()` |
| `MINI_CLAUDE_CMD` | Command launched in kitty tab | `claude --dangerously-skip-permissions` |
| `MINI_STATE_ROOT` | State root override (for tests) | `<cwd>/.swarm/octi` |
| `MINI_PRIMARY_CHECKOUT` | Primary repo checkout for worktree mgmt | `~/workspace/chitin` |
| `MINI_TRANSCRIPT_POLL_SEC` | `watch` poll interval | `2` |
| `MINI_KITTY_BIN` | kitty binary override (tests) | `kitty` |

## Troubleshooting

| Exit | Meaning | Fix |
|------|---------|-----|
| 2 | Usage error / missing recovery state dir | Re-read `--help`; for `--recovery`, the goal-id must already exist. |
| 3 | kitty remote control off | Edit `kitty.conf` and restart kitty. |
| 4 | Webhook URL missing on `watch` | Export `OCTI_DISCORD_WEBHOOK_URL` or drop a `webhook.url` in the state dir. |
| 5 | `status.json` malformed | Inspect manually; `mini nudge --message "rewrite status.json now"`. |
| 6 | `status.json` not yet written | Wait for the model to write the first status. |
| 7 | Input lock held by another holder | Wait for lease expiry (default 60s). |
| 8 | Worktree path collision | Use `--recovery <goal-id>` or remove the dir manually. |
| 9 | goal-id collision | Use `--recovery <id>` or re-phrase `--goal` to mint a fresh hash. |

## Tests

```sh
# unit + boundary tests (no live kitty)
python3 -m unittest discover -s swarm/tests -k mini -v

# live kitty tests (operator box)
MINI_TEST_LIVE_KITTY=1 python3 -m unittest \
    swarm.tests.test_mini_open_live \
    swarm.tests.test_mini_nudge_live \
    swarm.tests.test_mini_stop
```

## Boundary

Mini is **interface**. It does not own:

- Kanban state machines.
- Spec gate decisions.
- Branch/worktree lifecycle decisions (it executes them; it does not decide).
- PR lifecycle.
- Retry policy.
- Temporal workflow state.

These belong to **Octi** (slices 2-4). Octi imports only `MiniSession`
from this package — no internals. AC10 enforces this by grep. Slice 2
(controller loop + verifier) ships in `swarm/bin/octi` — see
`swarm/docs/octi.md`.
