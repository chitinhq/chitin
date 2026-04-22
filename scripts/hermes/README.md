# Hermes Staged Tick v1

Cron-triggered autonomous worker for `chitinhq/chitin`. Three stages,
each with a locked model:

| Stage | Model | Purpose |
|-------|-------|---------|
| PLAN  | `glm-5.1:cloud`      | Read the queue; pick an action |
| CODE  | `qwen3-coder:30b`    | Local on the 3090; write a diff (iff `plan.action=="code"`) |
| ACT   | `glm-5.1:cloud`      | Apply diff, commit, push, open PR, leave comments |

Governance v1 (`go/execution-kernel/internal/gov/`) fires on every tool
call in Stage 3; destructive actions are blocked at the tool boundary,
not in the prompt.

## Running one tick manually

```bash
cd ~/workspace/chitin/scripts/hermes
./tick.sh            # normal — executes tool calls in Stage 3
./tick.sh --dry-run  # Stage 3 prints WOULD-RUN lines, takes no action
```

## Where artifacts land

Each tick writes to `~/chitin-sink/ticks/<YYYY-MM-DD>/<UTC-ISO>/`:

| File | Stage | Contents |
|------|-------|----------|
| `env.txt`          | setup | Scrubbed env (CHITIN/HERMES/PATH) |
| `queue.json`       | setup | `{labeled, unlabeled, in_flight_prs}` |
| `plan.json`        | 1     | Stage 1's plan object |
| `plan-stderr.txt`  | 1     | Stage 1 hermes stderr |
| `ollama-probe.txt` | 2     | `ok` or `unreachable` |
| `diff.patch`       | 2     | Unified diff (iff `action=="code"` + probe ok) |
| `code-stderr.txt`  | 2     | Stage 2 hermes stderr |
| `act-log.txt`      | 3     | Stage 3 tool-call log / WOULD-RUN lines |
| `act-stderr.txt`   | 3     | Stage 3 hermes stderr |
| `tick.log`         | sh    | tick.sh orchestration log |

## Pausing the worker

```bash
hermes cron list                       # find the id of autonomous-worker
hermes cron pause <id>                 # pause
hermes cron resume <id>                # resume
```

## Troubleshooting

- **Empty `plan.json`**: Stage 1 returned non-JSON. See
  `plan-stderr.txt`. Next tick will retry from scratch.
- **`ollama-probe.txt: unreachable`**: local ollama daemon is not
  reachable at `127.0.0.1:11434`. Run `ollama serve` in a terminal. See
  also `~/chitin-sink/ollama-unreachable-streak.txt` — after 3
  consecutive unreachable ticks, the daily-summary WhatsApp cron
  surfaces this.
- **Tool call blocked by governance**: see `act-log.txt` for the block
  dict; also in `~/chitin-sink/gate-log-<date>.jsonl`. This is normal
  behavior — do not disable governance.

## Tests

```bash
bats scripts/hermes/tests/tick.bats         # orchestration tests (6)
scripts/hermes/tests/validate-plans.sh      # schema vs fixtures
```

## Specs and plan

- Spec: `docs/superpowers/specs/2026-04-22-hermes-staged-tick-design.md`
- Plan: `docs/superpowers/plans/2026-04-22-hermes-staged-tick.md`
