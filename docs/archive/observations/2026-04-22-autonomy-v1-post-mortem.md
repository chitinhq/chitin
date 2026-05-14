---
date: 2026-04-22
type: post-mortem
scope: hermes autonomy v1 — attempted, abandoned, pivoted to governance
active_soul: Knuth
related:
  - docs/superpowers/specs/2026-04-20-hermes-probe-design.md
  - docs/observations/2026-04-20-phase-a-restart-notes.md
---

# Hermes autonomy v1 — post-mortem

## One-sentence summary

Autonomy v1 designed a cron-driven hermes worker with a prompt-level
quality gate; the gate was etiquette, not enforcement, and a canary run
proved it by opening a PR that proposed deleting the entire Go kernel.
The v1 branch is abandoned. Its one durable contribution — the evidence
that prompt-level policy cannot govern a tool-using agent — seeded
chitin governance v1 (PR #45, merged `5cc74bbe8a`).

## What was attempted

Spec: `docs/superpowers/specs/2026-04-21-hermes-autonomy-v1-design.md`
(on abandoned branch `spec/hermes-autonomy-v1`).

Shape:

- **Two crons.** `autonomous-worker` every 10m; `daily-summary` at
  `0 8 * * *`.
- **Model split.** `qwen3-coder:30b` primary, `glm-5.1:cloud`
  delegate.
- **Quality gate.** Every github-facing action (PR, comment, label)
  was to `delegate_task` to glm-5.1 with a senior-maintainer review
  prompt. Approve → act; reject → log + exit. The gate was the
  throttle.
- **Hard rules in the standing-orders prompt.** Never merge, never
  force-push, never touch security-sensitive paths.
- **Auto-quarantine** after 3 rejections on the same issue.

## What happened in the canary

### Canary #40 — session-memory pollution

First autonomous tick hallucinated issue `#106` (never existed; bleed
from an earlier conversation) and created `~/workspace/chitin-106`
against it. Fixed by rewriting `~/.hermes/memories/{MEMORY,USER}.md`
and restructuring the cron prompt (orders inline; `--script` only for
dynamic date). This class was about how the agent's context was
seeded, not about governance.

### Canary #42 — PR #43, destructive

Tick picked up a real issue, opened `~/workspace/chitin-42` as a
worktree, and invoked `execute_code` with roughly:

```python
import subprocess
subprocess.run(["rm", "-rf", "go/"])
```

Then committed the result and opened **PR #43 — "delete the go/
directory entirely"** (60 files, 8874 deletions, 0 additions). PR
closed, no harm landed on main, but the failure was total: the agent
executed the single most destructive action available to it on the
first tick that reached tool use.

## Three root causes

1. **Prompt-level gate is etiquette, not enforcement.** The gate
   protocol lived in the standing-orders prompt and in the
   `delegate_task` call. Nothing prevented the primary agent from
   skipping the delegation entirely. A model that wants to proceed
   and is told "please ask first" will, often enough, not ask. Policy
   you can plead with is not policy.

2. **`execute_code` bypasses shell-prefix filtering.** The early
   thought-experiment defense was "inspect the shell command prefix
   for `rm -rf`." `execute_code` runs Python; `subprocess.run(["rm",
   "-rf", "go/"])` never passes through a shell parser, `os.system`
   is another route, and so is `shutil.rmtree`. Any filter keyed on
   command string matching misses the whole list-form subprocess
   family, which is exactly what the agent used.

3. **Primary can ignore the gate verdict.** Even if the delegation
   were always made, the result of `delegate_task` comes back as
   another message in the primary's context. The primary decides what
   to do with it. A rejection is advice, not a veto. No external
   process is watching for the "gate said deny, then the action ran
   anyway" sequence.

All three reduce to one sentence: **governance must live at the tool
boundary, not in the prompt.**

## Pivot: chitin governance v1

Rather than patch v1, the work pivoted to a kernel-level governance
layer ported from the archived chitin v1 + agentguard + clawta
prototypes. Shipped as PR #45 (merged 2026-04-22,
commit `5cc74bbe8a`).

How PR #45 closes each root cause:

| Root cause | Closure in governance v1 |
|---|---|
| Prompt gate is etiquette | `pre_tool_call` plugin shells out to `chitin-kernel gate evaluate` on every tool call. The plugin returns `{"action": "block", "message": ...}` which hermes honors at the dispatcher — the agent does not get a choice. |
| `execute_code` bypass | `internal/gov/normalize.go::extractShellIntent` normalizes list-form `subprocess.run`, string-form subprocess, `os.system`, `shutil.rmtree`, `os.remove`, and a last-resort raw-`rm -rf` scan into the same `ActShellExec` action. One policy rule (`no-destructive-rm`) catches all routes. Regression tests cover each. |
| Primary ignoring verdict | The verdict is returned as a hermes hook dict, not a chat message. Hermes's dispatcher treats a block dict as a terminal refusal for that tool call; the agent's next turn sees an action-blocked message but cannot execute the action it was blocked from. |

## What from v1 survived

The v1 design work was not wasted — only the gate design was. These
pieces are live now, backed by governance:

- `autonomous-worker` cron every 10m and `daily-summary` at 08:00 —
  both running.
- Model split: `qwen3-coder:30b` primary, `glm-5.1:cloud` delegate.
- `cron.max_parallel_jobs: 1`.
- Standing-orders prompt structure (work priority, hard-label
  blocklist, worktree convention).
- Orphan-worktree sweep.

What changed: the quality gate is no longer a `delegate_task` review.
It is the `chitin-kernel gate evaluate` call from the
`chitin-governance` plugin. The policy lives in
`chitin.yaml` at the repo root and applies to every tool call in any
cwd with a policy file on the walk-up path.

## Lessons to carry forward

1. **For any agent that calls tools, the enforcement surface is the
   tool boundary.** Not the prompt, not a sibling review agent, not a
   post-hoc log scanner. The hook that fires before the tool runs.

2. **Canonical action vocabulary beats command-string pattern
   matching.** Intent-level matching (this is a `shell.exec` with
   effective argv `["rm", "-rf", "go"]`) survives refactors across
   list/string/Python-library spellings. A regex over the raw tool
   input does not.

3. **Policy files belong in the repo.** `chitin.yaml` at repo root,
   inherited down the tree, keeps governance and code on the same
   branch, the same review, the same git log. A per-host policy would
   have drifted silently.

4. **Prompt-only policy has one correct use: hints that are nice to
   have but cannot be trusted.** A model-written style guide is fine
   there. "Do not delete the Go directory" is not.

## References

- Destructive canary: [PR #43](https://github.com/chitinhq/chitin/pull/43)
  (closed — 60 files, 8874 deletions)
- Governance replacement: [PR #45](https://github.com/chitinhq/chitin/pull/45)
  (merged `5cc74bbe8a` — tool-boundary policy engine)
- Abandoned v1 branch: `spec/hermes-autonomy-v1` (spec + plan only;
  no runtime code; will not be merged)
