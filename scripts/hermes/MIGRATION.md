# Migration: autonomy v1 → staged-tick v1

Run these steps on the machine that hosts the hermes worker, AFTER this
PR is merged to main.

## 1. Find and remove the existing cron

```bash
hermes cron list | grep -A3 autonomous-worker
```

Note the id (first column / `Id:` line). Then:

```bash
hermes cron rm <id>
```

## 2. Tombstone the retired prompt + script

```bash
mkdir -p ~/.hermes/scripts/retired-$(date -u +%Y-%m-%d)
mv ~/.hermes/scripts/autonomous-worker-orders.txt \
   ~/.hermes/scripts/autonomous-worker-context.py \
   ~/.hermes/scripts/retired-$(date -u +%Y-%m-%d)/
```

## 3. Pull the merged branch

```bash
cd ~/workspace/chitin
git pull origin main
```

Verify `scripts/hermes/tick.sh` is present and executable:
```bash
test -x scripts/hermes/tick.sh && echo OK
```

## 4. Register the new cron (system crontab — NOT `hermes cron`)

`hermes cron` is designed to run `hermes chat` sessions on a schedule
(optionally injecting a Python script's stdout into the prompt). It is
not a generic shell-script runner. `tick.sh` is a standalone bash
orchestrator that invokes `hermes chat` itself for each stage, so it
must be registered with the system's own cron (`cron.service`), not
with `hermes cron`.

Append a tick.sh entry to your user crontab (the PATH and HOME lines
are required — cron's default env lacks the dirs where `hermes` and
`gh` live):

```bash
(crontab -l 2>/dev/null; cat <<'CRONEOF'
PATH=/home/red/.local/bin:/snap/bin:/usr/local/bin:/usr/bin:/bin
HOME=/home/red
*/10 * * * * /home/red/workspace/chitin/scripts/hermes/tick.sh >> /home/red/chitin-sink/cron-tick.log 2>&1
CRONEOF
) | crontab -
```

Adjust `/home/red` if your home dir differs. Verify:
```bash
crontab -l
```

System cron must be running; on Debian/Ubuntu systemd:
```bash
systemctl is-active cron        # expected: active
```

## 5. Dry-run before letting cron fire

```bash
cd ~/workspace/chitin
./scripts/hermes/tick.sh --dry-run
```

Inspect the newest tick dir under `~/chitin-sink/ticks/` and confirm
`plan.json` and `act-log.txt` (containing `WOULD-RUN:` lines) look sane.

## 6. Let one real tick fire

Wait until the next `:00` / `:10` / `:20` / `:30` / `:40` / `:50`
minute mark plus a few seconds, then:

```bash
ls -lt ~/chitin-sink/ticks/$(date -u +%Y-%m-%d)/ | head
```

Look for a new tick dir whose `env.txt` shows `HERMES_TICK_DRY_RUN=0`
and a PATH matching your crontab line (not your interactive shell's
PATH — that's how you distinguish cron from manual runs). Inspect its
`plan.json` and `tick.log` for sane stage transitions.

`~/chitin-sink/cron-tick.log` stays empty on healthy ticks (tick.sh
writes only to tick artifact files, never to stdout/stderr). Non-empty
content there indicates unexpected output and is worth investigating.

## Optional: wire the 3-tick streak to WhatsApp

`tick.sh` maintains `~/chitin-sink/ollama-unreachable-streak.txt`. The
existing `daily-summary` cron does not read it yet. To enable the
spec's 3-tick unreachable alert, add a `tail -c 10
~/chitin-sink/ollama-unreachable-streak.txt` check to
`~/.hermes/scripts/daily-summary-orders.txt` so the cron surfaces
`"⚠ ollama unreachable for N ticks — 3090 likely off"` when N ≥ 3.
Left as a follow-up to keep this PR focused on orchestrator
correctness.

## Observability watchlist (first 10 ticks)

Runtime behaviors that only surface against the real models, not the stubs. Inspect the artifact dirs under `~/chitin-sink/ticks/` for each.

- **O1 — Fenced JSON from Stage 1.** glm-5.1 sometimes wraps JSON output in ```json fences despite the prompt forbidding it. tick.sh's `jq -e 'type=="object"'` rejects fenced output → skip tick (fail-safe). If this fires in >10% of ticks, add a de-fence pre-filter to tick.sh or strengthen the Stage 1 prompt's output prelude.
- **O2 — Literal `\n\n` in PR body.** Stage 3's code action step 7 passes `--body "Closes #N\n\n<intent>"` to gh. The `\n` is a literal backslash-n unless the shell or model expands it. Inspect the body of the first hermes-opened PR; if it shows literal `\n`, switch Stage 3 step 7 to `--body-file` with a heredoc.
- **O3 — DRY_RUN actual behavior.** Run `./tick.sh --dry-run` once and confirm `act-log.txt` contains `WOULD-RUN:` lines and ends with `DRY-RUN COMPLETE` (not real tool invocations).
- **O4 — Empty-diff retry loop.** If Stage 2 emits an empty diff on the same issue across multiple ticks, Stage 1 is repeatedly proposing a code action that qwen3 can't implement. Watch for the same `issue_number` appearing in `plan.json` with `action==code` across consecutive ticks without a PR landing. If observed, Stage 1 should re-plan to `external/comment` instead.
- **O5 — ARG_MAX on Stage 2 for large code files.** Stage 2's `-q` arg is `$(cat PROMPT_CODE) + separator + plan + separator + file_dump`. If `diff_request.files` contains large files (say >100KB total), bash may hit the OS ARG_MAX limit on the `hermes chat -q ...` invocation. Current behavior on overflow: `hermes` exits nonzero, tick.sh logs `stage 2 failed`, exit 0. Watch for this failure mode on real-world canary ticks; if it fires, patch by writing the stage2 prompt to a file and invoking via a hermes prompt-file mechanism (needs research on hermes CLI — no documented flag as of 2026-04-22) or by truncating per-file contents with clear markers.

## Rollback

If the new worker misbehaves, edit the crontab:
```bash
crontab -e
```
Comment out the `*/10 * * * * .../tick.sh` line (prefix with `#`),
save, exit. The `PATH=` and `HOME=` env lines can stay. To re-enable,
remove the `#`.

To fully remove (destructive — also drops the PATH/HOME lines; only
use if this machine's user crontab has nothing else you care about):
```bash
crontab -r
```

Work reverts to whatever manual `hermes chat` invocations you run. The
retired v1 prompt + context script are at
`~/.hermes/scripts/retired-<date>/` if you want to re-register the v1
hermes cron (`hermes cron create '<schedule>' '<prompt>' --script
autonomous-worker-context.py --name autonomous-worker --deliver
local`).
