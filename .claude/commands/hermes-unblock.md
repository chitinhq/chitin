# /hermes-unblock — Diagnose and unblock the Hermes swarm

Operational reference for the operator (red/JP) when the Hermes
autonomous swarm is stuck: dispatches crashing, tickets glued to a
status, crons silent, branches missing, spec queue jammed. Surfaces
the right query, the right log, and the right command — no guessing.

Authored by Hermes (GLM 5.1) as the operator handoff doc; lives in
the workspace so any box that pulls + runs `scripts/sync-skills.sh`
gets `/hermes-unblock`.

## Usage

```
/hermes-unblock                     — Triage view: stuck tickets + cron health + recent dispatch failures
/hermes-unblock <board>             — Same, scoped to one board (chitin | readybench)
/hermes-unblock ticket <id>         — Inspect one ticket: state, block_reason, last dispatch log
/hermes-unblock dispatch <id> <drv> — Manual re-dispatch (codex | claude-code | copilot)
/hermes-unblock crons               — `hermes cron list` with last_status per job
/hermes-unblock sign                — Re-sign chitin.yaml after a policy edit
```

## The stack (who does what)

| Component | Role |
|---|---|
| Hermes (GLM 5.1) | Dispatcher, critic, ceremony lead |
| Clawta (OpenClaw) | Dispatch pipeline via lobster workflows |
| Codex (GPT 5.4) | Implementation worker |
| chitin-kernel | Execution governance — gate, chain, signals |

## Boards

- **DB:** `~/.hermes/kanban/boards/<board>/kanban.db`
- **Boards:** `chitin`, `readybench`
- **Config:** `~/.hermes/kanban/boards/<board>/config.json`
- **Status flow:** `triage → ready → in_progress → (blocked) → review → done`
- **Critical:** tickets in `triage` need an `invariants_and_boundaries`
  block before promotion. Never promote without one.

### Board config (read via CLI, never hand-edit)

```bash
chitin-kernel board-config <board> workspace_root    # git repo path
chitin-kernel board-config <board> default_branch    # integration branch
chitin-kernel board-config <board> repo              # github repo slug
```

### Direct DB access (only when CLI won't do it)

```bash
DB=~/.hermes/kanban/boards/<board>/kanban.db

# Reset a stuck ticket back to ready/unassigned
sqlite3 "$DB" "UPDATE tasks SET status='ready', assignee=NULL WHERE id='<id>'"

# Bounce a ticket back to operator with a block reason
sqlite3 "$DB" "UPDATE tasks SET status='triage', assignee='red' WHERE id='<id>'"

# Inspect one ticket
sqlite3 "$DB" "SELECT id, status, assignee, block_reason FROM tasks WHERE id='<id>'"

# All in_progress tickets (often the source of glued state)
sqlite3 "$DB" "SELECT id, title, status, assignee FROM tasks WHERE status='in_progress'"

# Operator's spec queue
sqlite3 "$DB" "SELECT id, title FROM tasks WHERE status='triage' AND assignee='red'"
```

## Dispatch pipeline

`poller → classify → pick_driver → routing_record → approval → spawn_worker → finalize`

### Manual dispatch

```bash
cd ~/workspace/chitin
export KANBAN_BOARD=<board>
clawta dispatch ticket <id> to codex          # or: to claude-code, to copilot
```

### Dispatch logs

```bash
# Latest log for a ticket
cat ~/.openclaw/logs/dispatch-<id>.log

# Recent dispatches (newest first)
ls -lt ~/.openclaw/logs/dispatch-*.log | head -10

# Success vs failure tally
grep -l '"ok": true'  ~/.openclaw/logs/dispatch-*.log | wc -l
grep -l '"ok": false' ~/.openclaw/logs/dispatch-*.log | wc -l
```

### Common dispatch failures

| Error | Cause | Fix |
|---|---|---|
| `debug: not found` | `$pick_driver.json.shape_bucket` unquoted; `\|` becomes shell OR | PR #671 (merged) — verify workflow has quoted refs |
| `invalid reference: origin/swarm` | Branch not on remote | `git push -u origin swarm && git fetch origin` |
| `cannot lock ref refs/heads/swarm/…` | `swarm/` prefix collides with `swarm` branch | Use `agent/` prefix (deployed fix) |
| `pnpm: command not found` | PATH not propagated to subprocess | Check clawta wrapper L33 PATH fix |
| `returncode=-1` | Codex CLI auth/avail failure | Verify `codex` auth + model access |
| `JSONDecodeError` in `_pick_driver.py` | classify step returned bad JSON | Check classify PROMPT quoting (single-quote fix) |

## Branch conventions

```
main / develop      → protected, agents never touch
swarm               → integration branch (agent PRs target this)
agent/<driver>-<id> → individual agent worktree branches
```

- Board config `default_branch` MUST match the integration branch.
- `origin/<default_branch>` must exist on remote before dispatch works.
- Worktrees live in `~/.cache/chitin/swarm-worktrees/`.

## Cron jobs

```bash
hermes cron list                  # all jobs + last_status
hermes cron pause   <job_id>      # soft kill
hermes cron resume  <job_id>      # re-enable
hermes cron run     <job_id>      # trigger now
```

Active jobs to know:

| Job | Period | Purpose |
|---|---|---|
| `board-watchdog` | 10m | Grooms tickets; escalates spec-less to operator |
| `hermes-clawta-bridge` | 15m | Failure escalation, auto-unblock |
| `autonomous-board-engine` | 30m | Claims P0/P1 for hermes |
| `readybench-poller` | 15m | Dispatches readybench tickets |
| `swarm-standup` | 9am wkd | Daily standup |
| `swarm-retro` | 10am Mon | Weekly retro |

## Policy signing

After editing `chitin.yaml`, re-sign or governance loaders refuse it:

```bash
CHITIN_POLICY_PRIVATE_KEY_FILE=~/.chitin/trust/chitin-policy-ed25519 \
  chitin-kernel policy sign --policy-file ~/workspace/chitin/chitin.yaml
```

## The invariants_and_boundaries gate

Every ticket promoted to `ready` must carry:

```markdown
invariants_and_boundaries
boundary: <concrete constraint 1>
boundary: <concrete constraint 2>
```

Without it: poller demotes to `triage`, bridge won't auto-unblock,
watchdog blocks and assigns to operator. New tickets always start in
`triage`; the operator promotes once the spec is in.

## Key files

| File | Purpose |
|---|---|
| `~/.local/bin/clawta` | Dispatch wrapper (`--board`, PATH fix, `LOBSTER_REPO`) |
| `~/workspace/chitin/swarm/bin/clawta-poller` | Autonomous ticket dispatcher |
| `~/.hermes/scripts/hermes-clawta-bridge.py` | Failure-escalation bridge |
| `~/.openclaw/workflows/kanban-dispatch.lobster` | Lobster dispatch pipeline |
| `~/.openclaw/workflows/_pick_driver.py` | Driver/model selection |
| `~/workspace/chitin/chitin.yaml` | Governance policy |
| `~/.chitin/trust/chitin-policy-ed25519` | Policy signing key |

## Unblock playbook

1. **Stuck in `in_progress`** → reset to `ready` via SQLite, then re-dispatch.
2. **Promote-demote loop** → check bridge logs, verify `invariants_and_boundaries`, drop auto-unblock if needed.
3. **Dispatch crash** → read the dispatch log, match the failure table, fix workflow, re-run manually.
4. **Branch missing** → `git push -u origin <branch>`, then update board config if needed.
5. **Cron failure** → `hermes cron list`, inspect `last_status`, pause if broken.
6. **Spec queue full** → operator writes specs and promotes to `ready`.

## Anti-patterns

- Never merge to `main` directly — flow is `swarm → develop → main`.
- Never auto-unblock tickets assigned to `red`.
- Never promote a ticket without `invariants_and_boundaries`.
- Never modify the eval harness for an agent being evaluated.
- Discord is notification only — never source of truth.

## Chains with

- **`/queue`** surfaces the operator's plate; this skill is the
  unblocker for items it flags.
- **`/rollout`** for kernel version churn that breaks dispatch.
- **`/verdict`** to record the disposition once a stuck ticket lands.
