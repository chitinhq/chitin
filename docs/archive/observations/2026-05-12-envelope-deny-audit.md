# Envelope closed/exhausted deny audit

Date: 2026-05-12

## Question

Do `envelope-closed` and `envelope-exhausted` denials block normal
browse/status flows that should remain operable, or are they enforcing the
intended operator boundary?

## Data source

Local chain rows from `~/.chitin/gov-decisions-*.jsonl` through
2026-05-12, filtered to:

```sh
jq 'select(.allowed == false and (.rule_id == "envelope-closed" or .rule_id == "envelope-exhausted"))' \
  ~/.chitin/gov-decisions-*.jsonl
```

The local sample contains 68 matching rows. The `driver` JSON field is absent
on these rows; the `agent` field is `claude-code` except for one
`codex` `file.write` row.

## Matrix

Blast level here uses the kernel's stamped tier: `T0` is low-blast local
inspection, and `T2` is elevated/default-cost execution or mutation.

| Envelope state | Blast | Action type | Count | Read/status/browse? | Notes |
| --- | ---: | --- | ---: | --- | --- |
| `envelope-closed` | `T0` | `file.read` | 32 | Yes | Plain Claude `Read`/`Glob`/`Grep`/`LS`-shaped inspection. |
| `envelope-closed` | `T0` | `git.status` | 1 | Mixed | Target was `git add ... && git status`; normalized as status but contains a write-shaped staging operation. |
| `envelope-closed` | `T2` | `shell.exec` | 17 | Mostly | Mostly `ls`, `cat`, `pwd`, status-style directory probes; one `chitin-kernel envelope grant ...` recovery attempt. |
| `envelope-closed` | `T2` | `delegate.task` | 1 | No | Subagent delegation after envelope close. |
| `envelope-closed` | `T2` | `file.write` | 1 | No | Codex write attempt with empty target. |
| `envelope-exhausted` | `T0` | `file.read` | 6 | Yes | Plain file reads. |
| `envelope-exhausted` | `T2` | `shell.exec` | 5 | Mixed | Includes `echo`, `find`, `docker ps/logs`, and `vitest ... | tail`; status-like but subprocess-shaped. |
| `envelope-exhausted` | `T2` | `file.write` | 3 | No | Write attempts after budget exhaustion. |
| `envelope-exhausted` | `T2` | `git.commit` | 2 | No | Real commit attempts after exhaustion. |

Summary:

| Bucket | Count |
| --- | ---: |
| Low-blast `T0` inspection rows | 39 |
| Elevated/default `T2` shell rows | 22 |
| Elevated/default `T2` mutation/delegation rows | 7 |

## Denied read/status/diff samples

No `git.diff`, `git.log`, `git.worktree.list`, GitHub view/list, or `T0`
`shell.cat` rows appear in the sample.

Representative `T0` read/status rows:

| Timestamp | Rule | Action | Target |
| --- | --- | --- | --- |
| 2026-04-29T22:04:06Z | `envelope-exhausted` | `file.read` | `/home/red/workspace/chitin/docs/architecture/layer-contracts.md` |
| 2026-04-30T16:10:53Z | `envelope-exhausted` | `file.read` | `/home/red/.claude/skills/subagent-driven-development/code-quality-reviewer-prompt.md` |
| 2026-04-30T16:11:05Z | `envelope-closed` | `git.status` | `cd /home/red/workspace/chitin-analysis && git add ... && git status` |
| 2026-05-03T23:48:15Z | `envelope-closed` | `file.read` | `apps/temporal-worker/skills/**/*.md` |
| 2026-05-04T01:13:06Z | `envelope-closed` | `file.read` | `docs/decisions/**` |
| 2026-05-04T01:28:11Z | `envelope-closed` | `file.read` | `./go/execution-kernel/internal/router/**` |

Representative shell browse/status rows currently stamped `T2`:

| Timestamp | Rule | Target |
| --- | --- | --- |
| 2026-04-30T16:24:09Z | `envelope-closed` | `ls /home/red/workspace/chitin-analysis/` |
| 2026-05-03T23:48:00Z | `envelope-exhausted` | `find .../apps/temporal-worker/src/skill-loader -type f` |
| 2026-05-04T00:37:24Z | `envelope-closed` | `cat .../scripts/install-kernel.sh 2>&1 || echo "FILE_NOT_FOUND"` |
| 2026-05-04T01:18:00Z | `envelope-closed` | `pwd && ls docs/` |
| 2026-04-30T23:52:10Z | `envelope-exhausted` | `docker ps ... && docker logs ... | tail -10` |

## Interpretation

Most denied `T0` rows are harmless inspection actions. They do block normal
browse flows after an envelope is closed or exhausted.

That blocking is consistent with the current envelope contract. In
`go/execution-kernel/internal/gov/gate.go`, envelope spend runs after policy
allowance and converts an otherwise allowed decision into
`envelope-closed`/`envelope-exhausted`. The comments describe caps as hard
contracts, not monitor-mode advisories. The implementation also exempts
budget denials from the lockdown counter, which means envelope denial is a
boundary on continued work, not a punishment signal.

The strongest reason not to add a broad read-only carveout is that normalized
action type is not a sufficient proof of no side effect. The sample has
`git add ... && git status` logged as `git.status` at `T0`. A carveout keyed
only on `T0` or `file.read`/`git.status` would create a governance recovery
loophole for compound shell commands that contain status/read tokens.

Cost-control also weakens under a broad carveout. PreToolUse can only charge
the proposed tool call and input bytes; it cannot know output size. Allowing
unbounded post-exhaustion reads lets an agent continue generating large tool
outputs and model follow-up context after the operator's envelope says stop.

## Recommendation

Do not add a general read-only bypass for closed or exhausted envelopes.
The observed behavior is noisy for browse flows, but it matches the operator
boundary: closed/exhausted means the current unit of work should stop until
the operator or a trusted supervisor grants, switches, or closes out the
envelope.

Keep the existing chitin admin recovery bypass instead. The current hook path
already sets `spendEnvelope = nil` for `chitin-kernel` admin commands, so
operator recovery commands can run through policy without consuming or being
blocked by the active envelope. Mutation-shaped admin commands are separately
guarded by trusted authority rules.

## Safe scope if this changes later

A carveout would only be defensible if it is narrow, explicit, and
syntax-aware:

1. Apply only to `envelope-exhausted`, not `envelope-closed`. A closed envelope
   is an operator stop signal; exhaustion is a budget edge.
2. Apply only to structured driver tools whose normalizer proves read shape:
   Claude `Read`, `Glob`, `Grep`, `LS`, `TaskGet`, `TaskList`, `TaskOutput`,
   `ToolSearch`, `CronList`, `EnterPlanMode`, and `ExitPlanMode`.
3. Do not apply to `shell.exec`, even when the command looks like `ls`, `cat`,
   `find`, `git status`, or `tail`. Shell syntax needs compound-command
   analysis before it can safely claim read-only.
4. Cap the carveout separately, for example a small post-exhaustion inspection
   counter per envelope, and stamp a distinct rule such as
   `envelope-exhausted-readonly-grace` so analytics can see it.

The current sample does not warrant that change. It shows friction, but also
shows exactly the loophole class a broad bypass would open.
