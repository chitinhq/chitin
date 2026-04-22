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

## 4. Register the new cron

```bash
hermes cron create \
  --name autonomous-worker-staged \
  --schedule 'every 10m' \
  --command "$HOME/workspace/chitin/scripts/hermes/tick.sh"
```

## 5. Dry-run before letting cron fire

```bash
cd ~/workspace/chitin
./scripts/hermes/tick.sh --dry-run
```

Inspect the newest tick dir under `~/chitin-sink/ticks/` and confirm
`plan.json` and `act-log.txt` (containing `WOULD-RUN:` lines) look sane.

## 6. Let one real tick fire

Wait ≤10 minutes, then:

```bash
hermes cron list | grep autonomous-worker-staged
```

`Last run` should show `ok`. Inspect the artifact dir for that tick.

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

## Rollback

If the new worker misbehaves:

```bash
hermes cron pause <id of autonomous-worker-staged>
```

Work reverts to whatever manual `hermes chat` invocations you run. The
retired prompt can be restored from `~/.hermes/scripts/retired-<date>/`
if you want to re-register the v1 cron.
