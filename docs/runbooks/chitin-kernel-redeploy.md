# chitin-kernel-redeploy runbook

> Closes the deploy-lag pattern documented in
> `docs/observations/2026-05-03-low-success-alarm-investigation.md`:
> kernel/policy fixes can sit in `main` for hours-to-days because
> nobody manually rebuilds, and the swarm runs against a stale
> binary. This runbook narrows the window to ~15 minutes.

## What it does

A user-level systemd timer (`chitin-kernel-redeploy.timer`) fires
every 15 minutes and runs `scripts/install-kernel.sh`, which:

1. Fetches `origin/main` and ff-pulls if behind.
2. Decides whether to rebuild — only if either (a) the new commits
   touch `go/` or `chitin.yaml`, OR (b) the binary at
   `~/.local/bin/chitin-kernel` is older than tracked sources.
3. Saves the prior binary to `~/.local/bin/chitin-kernel.prev` for
   rollback.
4. Runs `go build -o ~/.local/bin/chitin-kernel
   ./go/execution-kernel/cmd/chitin-kernel`.
5. Smoke-tests the new binary by piping a canned `Task` PreToolUse
   payload through `gate evaluate --hook-stdin`. The smoke is
   version-aware: with the closed-enum normalizer (PR #171) Task
   resolves to `delegate.task` (allowed); without it, the call falls
   through to `default-deny-unknown` (denied). Smoke pass = exit 0
   within 2 seconds.
6. On smoke failure, rolls back to `chitin-kernel.prev` and exits
   non-zero so the systemd unit reports failure.
7. Logs one structured JSON line per run to
   `~/.cache/chitin/install-kernel.jsonl`. Stderr mirror lands in
   journald.

## Install

```bash
cd ~/workspace/chitin

# Symlink the units into the user systemd dir.
mkdir -p ~/.config/systemd/user
ln -sf "$PWD/ops/systemd/chitin-kernel-redeploy.service" \
       ~/.config/systemd/user/chitin-kernel-redeploy.service
ln -sf "$PWD/ops/systemd/chitin-kernel-redeploy.timer" \
       ~/.config/systemd/user/chitin-kernel-redeploy.timer

systemctl --user daemon-reload
systemctl --user enable --now chitin-kernel-redeploy.timer
```

Verify:

```bash
systemctl --user list-timers chitin-kernel-redeploy.timer
# NEXT                        LEFT       LAST                        PASSED       UNIT
# 2026-05-03 14:50:00 EDT     14min left 2026-05-03 14:35:00 EDT      8s ago       chitin-kernel-redeploy.timer
```

Trigger one run manually to confirm wiring:

```bash
systemctl --user start chitin-kernel-redeploy.service
journalctl --user -u chitin-kernel-redeploy.service -n 20 --no-pager
```

## Verify it ran

The script writes a JSON line per run:

```bash
tail -5 ~/.cache/chitin/install-kernel.jsonl
# {"ts":"2026-05-03T18:35:55Z","kind":"noop","msg":"no rebuild needed","old_sha":"ab3dfd1e..."}
# {"ts":"2026-05-03T18:50:00Z","kind":"ok","msg":"redeploy-success","old_sha":"ab3dfd1e...","new_sha":"a1b2c3d4...","build_dur_ms":"3421","changed":"go/execution-kernel/internal/gov/escalation.go"}
```

The `kind` field is the line's verdict:

| kind | what it means |
|---|---|
| `noop` | no rebuild needed (no relevant commits, binary current) |
| `ok` | rebuild + smoke passed |
| `rollback` | rolled back to `.prev` after a build or smoke failure |
| `fail` | something went wrong; see `msg` field |

## Suspend / pause

```bash
# Pause the timer. The script will stop firing but the existing
# binary keeps working.
systemctl --user stop chitin-kernel-redeploy.timer

# Permanently disable.
systemctl --user disable chitin-kernel-redeploy.timer

# Resume.
systemctl --user enable --now chitin-kernel-redeploy.timer
```

Suspend is the right move when:

- Doing a coordinated multi-PR rollout that needs the operator to
  control which commit is actually running.
- The rig is hosting a long-running benchmark and you don't want a
  surprise rebuild mid-run.
- You're debugging a rollback path and need the binary state to
  stay frozen.

## Manual override

Run the script directly any time:

```bash
~/workspace/chitin/scripts/install-kernel.sh
```

Force a rebuild even when the script thinks no-op (useful after a
manual `git checkout` to a different commit):

```bash
# Touch a kernel source file so the find -newer check trips.
touch ~/workspace/chitin/go/execution-kernel/cmd/chitin-kernel/main.go
~/workspace/chitin/scripts/install-kernel.sh
```

## Rollback

The script auto-rolls-back on smoke failure. To roll back manually
(e.g., a fresh build builds and smokes fine but you observe a
regression downstream):

```bash
cp -a ~/.local/bin/chitin-kernel.prev ~/.local/bin/chitin-kernel
# Optionally suspend the timer until the bad commit is reverted in main:
systemctl --user stop chitin-kernel-redeploy.timer
```

The next manual rebuild or timer fire will overwrite `.prev` with
the current binary, so the rollback window is one cycle.

## Configuration

The script reads three environment variables (all optional):

| var | default | purpose |
|---|---|---|
| `CHITIN_REPO` | `~/workspace/chitin` | path to the chitin git checkout |
| `CHITIN_KERNEL_BIN` | `~/.local/bin/chitin-kernel` | path to the installed binary |
| `CHITIN_INSTALL_KERNEL_LOG` | `~/.cache/chitin/install-kernel.jsonl` | structured log path |

Set them in `~/.config/environment.d/chitin.conf` if your layout
differs:

```
CHITIN_REPO=%h/code/chitin
CHITIN_KERNEL_BIN=%h/.local/bin/chitin-kernel
```

## Exit codes

The script's exit code distinguishes failure modes for telemetry
consumers:

| code | meaning | timer behavior |
|---|---|---|
| 0 | no-op or rebuild + smoke success | unit succeeds, timer continues |
| 1 | git pull conflict | unit fails; operator must resolve manually |
| 2 | build failure (rollback attempted) | unit fails; investigate compile error |
| 3 | smoke failure, rollback succeeded | unit fails; the bad commit is in main and should be reverted |
| 4 | smoke failure AND rollback failed | unit fails hard; binary in undefined state |

Exit code 4 is the page-the-operator case. The timer will keep
firing on its 15-minute cadence and may eventually self-heal if a
revert-PR lands in main, but the rig is running a non-functional
binary in the meantime.

## Why a 15-minute cadence

- 5-min cadence is unnecessarily aggressive — most builds are
  no-ops, and we don't need single-minute deploy lag.
- 1-hour cadence is too sparse — the alarm that motivated this
  runbook (2026-05-03 low-success) sat for 20 hours; we want a
  ceiling well under "operator notices a problem and asks why."
- 15 minutes is the right balance: bounded staleness, low
  background CPU/IO impact, and operator can always run the
  script manually for instant push.

## Related

- Investigation: `docs/observations/2026-05-03-low-success-alarm-investigation.md`
- Backlog entry: `docs/swarm-backlog.md#auto-rebuild-redeploy-chitin-kernel`
- Companion symlink-style installer: `scripts/install-kernel-symlink.sh`
  (used when the binary is provisioned out-of-band; this script is
  the rebuild-from-source variant)
