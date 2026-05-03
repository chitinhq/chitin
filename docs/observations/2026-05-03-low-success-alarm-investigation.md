# 2026-05-03 — low-success alarm investigation

## TL;DR

`chitin-alarm-feeder.timer` fired at **2026-05-03T03:45:14Z** with:

> LOW SUCCESS: driver=claude-code-headless 56% (5/9)

Investigation traced the 4 failures to **two distinct root causes**:

| failures | root cause | fix |
|---|---|---|
| 3 of 4 | stale `chitin-kernel` binary — pre-dates PR #171 (closed-enum normalizer); `Task` / `Agent` / `Skill` tool calls fall through to `default-deny-unknown`, accumulate, trigger sticky lockdown | rebuilt + reinstalled kernel; reset claude-code agent state |
| 1 of 4 | governance self-modification trap — `scheduler-gov-rule` entry asks the agent to write to `chitin.yaml`, blocked by `no-governance-self-modification: enforce` rule | needs operator decision (re-tier T5 or new action class) |

The dominant story is **deployment lag, not code defect**: PR #171's normalizer fix was in `main` for ~20 hours before the alarm fired but had not been redeployed to `~/.local/bin/chitin-kernel`. The swarm ran against a 21-hour-old binary that didn't know modern Claude Code tools.

## The alarm window

- Alarm fired: **2026-05-03T03:45:14Z**
- Rollup window: rolling 24h before the alarm
- Rollup file at the time of fire was overwritten by the next tick at 06:00 EDT (10:00 UTC); reconstruction below was done from `~/.cache/chitin/swarm-state/dispatched/` markers cross-joined with `tmp/result-swarm-*.json` envelopes via `analysis.swarm_runs.load_swarm_runs`.

## The 4 failures

All four share the same outcome shape (`exit=0`, `commits_added=0`) — the "no-work-produced" failure class, not crashes.

| entry_id | tier | dur | commits | dispatched_at (UTC) | mode |
|---|---|---|---|---|---|
| `task-validate-command-pre-activity-gate` | T3 | 33s | 0 | 2026-05-02T04:09:26 | lockdown |
| `chitin-install-slice-3-agents` | T2 | 8s | 0 | 2026-05-02T04:14:31 | lockdown |
| `rename-local-cloud-driver-misnomer` | T2 | 9s | 0 | 2026-05-02T04:24:39 | lockdown |
| `scheduler-gov-rule` | T4 | 22s | 0 | 2026-05-03T03:52:42 | gov-self-mod |

## Root cause A — stale kernel → unknown-tool denial cascade → sticky lockdown

### What the chain shows

`/home/red/.chitin/events-*.jsonl` records 33 deny decisions across all agents:

| count | rule_id | what it means |
|---|---|---|
| 22 | `lockdown` | the agent was already locked; rule preempts everything |
| 7 | `default-deny-unknown` | the canon normalizer returned `ActUnknown` |
| 3 | `no-governance-self-modification` | agent tried to write `chitin.yaml` |
| 1 | `no-force-push` | agent tried `git push --force` |

All 7 `default-deny-unknown` events for `claude-code` had `target=Agent`. Reading `go/execution-kernel/internal/driver/claudecode/normalize.go`, the `Task` / `Agent` / `Skill` cases ARE mapped to `gov.ActDelegateTask`. So why are they showing as unknown?

### The deploy-lag finding

```
$ stat ~/.local/bin/chitin-kernel | grep Modify
Modify: 2026-05-01 21:37:03 -0400

$ git log -1 --format='%h %ci' -- go/execution-kernel/internal/driver/claudecode/normalize.go
ba24785 2026-05-02 17:40:51 -0400   # PR #171: closed-enum coverage
```

The deployed binary was built **20 hours before** PR #171's normalizer fix landed. The fix exists in source; it doesn't exist in the running kernel.

Other PRs merged between binary-build and now that the swarm is also running without:
- #170 — three OTEL emit bugs from PR #71 adversarial review
- #171 — closed-enum normalizer (the fix above)
- #172 — copilot driver hygiene cluster
- #173 — canon-driven bypass detection (closes #58–62)
- #175 — F4 timeout + single-callsite OnDecision
- #177 — session_end-is-last + idempotent dedup
- #178 — shared TS↔Go fixtures + schema-drift gate
- #180 — chain_id in error JSON + chain-verify stub
- #182 — per-action bounds + invariantModes
- #186 — SubagentStop routes to subagent's chain

### How this caused the lockdown chain

Timeline reconstructed from chain events for the `claude-code` agent identity:

| ts (UTC) | rule | tool | what happened |
|---|---|---|---|
| 2026-05-01T20:56 | `default-allow-shell` (allow) | shell.exec | (normal early activity) |
| 2026-05-02T01:39 | `no-governance-self-modification` (deny) | file.write `chitin.yaml` | smoke test deny |
| 2026-05-02T03:44 | `default-deny-unknown` (deny) | unknown `Agent` | **Task tool falls through** |
| 2026-05-02T03:54 | `default-deny-unknown` (deny) | unknown `Agent` | (same) |
| 2026-05-02T04:09:42 | `default-deny-unknown` (deny) | unknown `Agent` | (same) |
| 2026-05-02T04:09:48 | **`lockdown` (deny)** | file.read `*.go` | **threshold crossed; sticky lock fires** |
| 2026-05-02T04:09 → 11:52 | 22× `lockdown` denials | various | every subsequent tool call denied |

The `claude-code` agent identity is **shared across all c-c-h swarm dispatches**. So one dispatch's denial accumulation poisons the next dispatch's run. Once total denials cross 10 (`internal/gov/escalation.go:86`), `locked=1` is sticky — survives all sessions until operator runs `chitin-kernel gate reset --agent=<id>`.

The agent in failure #1 actually tried to self-reset:

```json
"tool_input": {"command": "chitin-kernel gate reset --agent=claude-code 2>&1"}
```

That `shell.exec` was also denied by the lockdown rule. Closed loop.

### How dispatches die under lockdown

Once the gate returns `RuleID: "lockdown"` for the very first tool call, claude-code reports the call as a permission denial in its final stream-json summary, rolls onto the next planned tool call, hits the same lockdown denial, gives up, and exits. Total dispatch duration: 8–33 seconds. No commits. The dispatcher's `result-swarm-*.json` envelope records `exit_code: 0` (clean exit), `commits_added: 0`, and `permission_denials: [...]` in the stdout tail.

## Root cause B — governance self-modification trap

`scheduler-gov-rule` (the 4th failure) ran AFTER PR #171 would have helped if deployed, but the failure mode here is different. The entry's task is to add a new chitin governance rule by editing `chitin.yaml`. Chitin's `no-governance-self-modification: enforce` rule blocks the write:

```
2026-05-03T03:48:23Z  rule=no-governance-self-modification
                      tool=file.write
                      target=/home/red/.cache/chitin/swarm-worktrees/swarm-scheduler-gov-rule-…/chitin.yaml
```

The agent has no path to complete the work as specified by the entry. Exits clean with 0 commits. This isn't a swarm bug — it's a backlog-entry / policy mismatch the groomer should have caught before dispatch.

The entry needs one of:

1. **Re-tier to T5 (human action).** Match the existing pattern for governance changes — a human operator authors the chitin.yaml edit; swarm dispatchers don't touch governance config.
2. **New action class `chitin.gov.proposed-rule.add`** that routes through review rather than a direct file write. Lets the swarm draft a rule proposal that an operator approves before the file is written. Bigger lift; same protective property; preserves swarm-author capability for governance-evolution work.

## Operational fix applied (this report)

Two commands, ~2 minutes:

```bash
# 1. Rebuild kernel from main (pulls in PRs #170-#186)
cd ~/workspace/chitin/go/execution-kernel
go build -o ~/.local/bin/chitin-kernel ./cmd/chitin-kernel

# 2. Reset accumulated state for claude-code (currently 7/10, two more
#    Task denials from a stale binary would re-lock; reset is precaution)
~/.local/bin/chitin-kernel gate reset --agent=claude-code
```

Verified post-fix:

```bash
$ echo '{"hook_event_name":"PreToolUse","tool_name":"Task",
        "tool_input":{"description":"smoke test"},
        "cwd":"/home/red/workspace/chitin",
        "session_id":"manual-smoke"}' \
  | ~/.local/bin/chitin-kernel gate evaluate --hook-stdin --agent=manual-smoke
$ echo "exit=$?"
exit=0   # Task → delegate.task → default-allow-delegate → allowed
```

`agent_state` and `denials` rows for `claude-code` confirmed empty after reset.

## Structural gap and follow-up entries

The deploy-lag pattern is not specific to this incident. Any PR landing in `go/` or `chitin.yaml` only takes effect when an operator manually rebuilds. The swarm runs unattended; nobody redeploys; policy fixes sit dark.

Two follow-up entries filed alongside this report (in the same PR):

1. **`auto-rebuild-redeploy-chitin-kernel`** (T2, ready) — systemd-timer or post-merge GitHub Action that rebuilds + reinstalls `~/.local/bin/chitin-kernel` after any merge to `main` touching `go/` or `chitin.yaml`. Closes the deploy-lag gap.

2. **`scheduler-gov-rule-retier-or-action-class`** (T5, in_design) — operator decision on the gov-self-mod trap. Either re-tier the entry to T5 or add the `chitin.gov.proposed-rule.add` action class.

## Appendix — queries

### Which dispatches failed in the alarm window

```python
from datetime import datetime, timedelta, timezone
from pathlib import Path
from analysis.swarm_runs import load_swarm_runs
from analysis.loaders import Window

state_dir = Path.home() / '.cache/chitin/swarm-state/dispatched'
tmp_dir = Path('/home/red/workspace/chitin/tmp')
end = datetime(2026, 5, 3, 4, 0, tzinfo=timezone.utc)
window = Window(end - timedelta(hours=24), end)

runs = load_swarm_runs(state_dir, tmp_dir, window)
cch = [r for r in runs if r.driver == 'claude-code-headless']
fails = [r for r in cch if r.exit_code != 0 or r.commits_added == 0]
# → 4 failures (matches alarm "5/9" = 4 fails of 9 c-c-h runs)
```

### Why each dispatch failed

```python
import json
for f in [
    'task-validate-command-pre-activity-gate-1777694966071',
    'chitin-install-slice-3-agents-1777695271138',
    'rename-local-cloud-driver-misnomer-1777695879490',
    'scheduler-gov-rule-1777780362211',
]:
    env = json.loads(open(f'tmp/result-swarm-{f}.json').read())
    tail = env['result']['stdout_tail']
    if 'permission_denials' in tail:
        # extract permission_denials block — surfaces lockdown / unknown denies
```

### Agent state and denial accumulation

```python
import sqlite3
db = sqlite3.connect('/home/red/.chitin/gov.db')
list(db.execute("""
    SELECT agent, total, locked, locked_ts
    FROM agent_state ORDER BY total DESC
"""))
# Pre-fix: claude-code total=7 locked=0 (residual from before reset)
# Post-fix: claude-code (no row — reset cleared it)
```

### Chain events surfacing the rule cascade

```python
import glob, json
for path in sorted(glob.glob('/home/red/.chitin/events-*.jsonl')):
    for line in open(path):
        ev = json.loads(line)
        if ev.get('event_type') != 'decision': continue
        p = ev.get('payload', {})
        if p.get('decision') != 'deny': continue
        if ev.get('agent_instance_id') != 'claude-code': continue
        # → 33 chain entries; 22 lockdown, 7 default-deny-unknown, 3
        #   no-gov-self-mod, 1 no-force-push
```
