---
status: open
owner: claude-code
kanban: t_6e6376b2
implementation_pr: null
superseded_by: null
effective_from: '2026-05-12'
effective_to: null
---

# Spec: no-commit-to-protected-branch policy + openclaw exec coverage

Date: 2026-05-12
Status: spec — open
Kanban: `t_6e6376b2`
Author: claude-code (operator-controlled)

## Problem

On 2026-05-12 the Clawta agent (`openai-codex/gpt-5.5` via openclaw)
autonomously committed three changes directly to local `main` while
building self-healing infrastructure (the work that became PR #541).
Operator's hard rule is "no commit to main; bit 4× in 2026" (per
operator memory `feedback_no_commit_to_main_policy.md`). Chitin's gate
was active throughout and did **not** deny the commits.

Forensics surfaced two distinct gaps:

1. **Chitin doesn't observe openclaw agent exec calls.** The
   `apps/openclaw-plugin-governance` plugin already gates *chat-domain*
   actions (we see `echo clawta-spawn-marker` rows attributed to
   `agent=clawta` in the chain). But the actual `exec` tool calls
   Clawta makes — including `git commit` — never appear in the chain
   ledger with `agent=clawta`. Today's 78 chain rows containing the
   substring `git commit` are all attributed to `claude-code` or
   `codex` (workers gated via their own PreToolUse hooks). Zero
   attributed to Clawta. Chitin literally never saw the commits.

2. **No chitin rule for `git.commit` on protected branches.** Even if
   chitin had observed Clawta's commits, the only protected-branch
   rule today is `no-protected-push` (`action: git.push`). The
   `default-allow-git-ops` rule would have allowed the commit. The
   action-type classifier already recognizes `git commit` →
   `ActGitCommit` (see `go/execution-kernel/internal/gov/normalize.go`
   and verified live for claude-code + codex), so Part 2 of the fix is
   just adding the YAML rule.

Both gaps must close. Closing only Part 2 protects against future
codex/claude-code rule-breaks but does nothing for the Clawta vector
that bit us today. Closing only Part 1 makes Clawta visible but still
allows the commit.

## Why operator memory isn't enough

The "no commit to main" rule lives in `~/.claude/projects/.../memory/`
and steers Claude Code (me) plus the Hermes prompt. Openclaw-driven
agents (Clawta, downstream codex/copilot workers when launched via
openclaw) inherit none of that prompt context. The only place
enforcement applies *universally* — across every agent that talks to
the host shell — is chitin's gate.

## Architecture

### Part 1: extend openclaw plugin to gate exec tool calls

The openclaw plugin at `apps/openclaw-plugin-governance/src/index.mjs`
registers a `before_tool_call` hook with openclaw. Today's
implementation gates *chat-domain* tool calls (sending messages,
posting to Discord, etc.). It does not gate the `exec` tool — the
catch-all that runs arbitrary shell commands.

The fix: extend the plugin's `before_tool_call` handler to detect
exec-shaped tool calls (tool name in {`exec`, `shell`, `bash`,
whatever the canonical name is in current openclaw — verify before
implementing) and forward the command string to chitin's gate exactly
as the existing claude-code / codex / hermes hooks do:

```
echo '<json-event>' | chitin-kernel gate evaluate \
  --hook-stdin --agent=<openclaw-agent-id>
```

If chitin denies, the plugin must:
- Return a deny verdict to openclaw so the exec is blocked.
- Emit a structured error message visible to the agent ("blocked by
  chitin: rule no-commit-to-protected. Suggestion: `git checkout -b
  <feature-branch>` first").
- Write a chain row attributed to the openclaw agent (e.g.,
  `agent=clawta`, `action_type=git.commit`, `allowed=false`,
  `rule_id=no-commit-to-protected`).

If chitin allows, exec proceeds normally.

**Attribution.** The plugin must pass the openclaw agent id (`clawta`,
`main`, etc.) as `--agent=<id>` so the chain ledger correctly attributes
the action. Today's deliberate `clawta-spawn-marker` chain row pattern
(see `swarm/workflows/kanban-dispatch.lobster:spawn_worker` for the
existing pattern) is a workaround for the missing attribution; once
this fix lands, the marker pattern can be retired (separate cleanup
ticket, not in scope here).

**Tier classification.** The chitin tier logic for `git.commit` already
exists; the plugin doesn't need to compute it. Just pass the command
through.

### Part 2: add the chitin.yaml rule

Add a `no-commit-to-protected` rule mirroring the existing
`no-protected-push` shape:

```yaml
  - id: no-commit-to-protected
    action: git.commit
    effect: deny
    branches: [main, master, "<HEAD-implicit>"]
    reason: "Direct commit to protected branch — every code change must start with `git checkout -b <feature-branch>` first. Bit 4× in 2026 per operator memory."
    suggestion: "git checkout -b <type>/<slug> BEFORE the first edit; or `git worktree add` to a separate worktree directory."
    correctedCommand: "git checkout -b fix/<issue>-<slug>"
```

Also add a corresponding policy mode entry next to `no-protected-push`
in the `policy` map at the top of `chitin.yaml`:

```yaml
  no-commit-to-protected: enforce
```

The rule's `branches` field uses the same `<HEAD-implicit>` mechanism
that `no-protected-push` already uses. That mechanism (located in the
gate evaluator, search for `HEAD-implicit`) shells out to read the
current HEAD at gate time — slow per call but only for git operations
(not the hot path) and we already pay this cost for `git.push`.

## Detection corner cases

The classifier in `internal/gov/normalize.go` already promotes
`git commit` → `ActGitCommit` via the canon parser. The promotion
handles:

- `git commit -m "..."`
- `git commit -a -m "..."`
- `git commit --amend` (inherits ActGitCommit — fine; amending on main is also bad)
- `git -C <path> commit ...` (canon resolves the cwd)
- `&&`-chained commands — each is parsed independently; commit on RHS still classified

What the classifier won't catch:

- Commands hiding in heredocs: `bash -c "git commit -m hi"` — the
  outer `bash` is classified as `shell.exec`. Acceptable: the spawned
  bash will fire a fresh tool call that does hit the hook.
- Aliases (`alias gc='git commit'`) — out of scope; chitin gates raw
  commands, not user shell aliases.

## Acceptance

1. **Coverage smoke.** A `git commit` invocation by the `clawta`
   openclaw agent (triggered by sending a Discord message to it that
   asks it to commit something) appears in the chain ledger with:
   - `action_type: git.commit`
   - `agent: clawta`
   - `driver: clawta` (or similar — match the existing attribution
     pattern)
   - Either `allowed: true` (if HEAD ≠ protected) or `allowed: false`
     with `rule_id: no-commit-to-protected` (if HEAD == protected).
2. **Negative case.** A `git commit -m "..."` while HEAD == main is
   denied with `rule_id: no-commit-to-protected` for agents in
   {clawta, codex, copilot, gemini, claude-code, hermes}. Test fixture
   for each.
3. **Positive case.** A `git commit -m "..."` while HEAD is a
   non-protected branch (e.g., `swarm/codex-<ticket>`) is allowed,
   classified as `git.commit`, no rule fires.
4. **Replay test.** A replay harness can take a chain row from
   2026-05-12 (commit `6a0f13d` or similar) and produce a denied
   verdict under the new rule.
5. **No false positives on the operator.** I (claude-code) routinely
   run `git reset --hard origin/main` and other read-side git ops on
   main worktrees. Only `git.commit` triggers the deny — other actions
   (`git diff`, `git log`, `git reset`, `git status`) continue to be
   allowed.

## Out of scope

- Signed-commit verification (out: separate trust concern).
- GitHub branch-protection rule import (out: chitin's `branches: [...]`
  list is the source of truth here, not GitHub's UI).
- Per-repo override lists (out: a single global list is sufficient
  until the swarm runs against multiple repos with different policies).
- Retiring the `clawta-spawn-marker` chain row pattern (out: separate
  cleanup ticket once Part 1 lands; not load-bearing for this fix).
- Auto-fix / auto-branch (out: chitin denies, it doesn't repair —
  agents that hit the deny should follow `correctedCommand` themselves
  or escalate).

## Implementation notes for the worker

- **Part 1 lives in `apps/openclaw-plugin-governance/`** (TypeScript).
  Read the existing `before_tool_call` handler; mirror the
  claude-code/codex hook patterns from `scripts/install-*-hook.sh` for
  the chitin-kernel gate invocation. The plugin's
  `chitin-governance registering` log line at startup confirms it's
  loading; smoke is "does an exec call now produce a chain row".
- **Part 2 lives in `chitin.yaml`** at repo root. Two edits: add the
  rule body in the `rules:` block (place near `no-protected-push`),
  add the policy-mode entry at the top.
- **Tests.** Add Go test fixtures under
  `go/execution-kernel/internal/gov/testdata/` for the positive +
  negative + agent-each cases. Mirror the existing `no_protected_push`
  test if one exists; otherwise model after the rule-eval tests in the
  same directory.
- **Operator memory update.** After landing, append a line to
  `~/.claude/projects/.../memory/feedback_no_commit_to_main_policy.md`
  noting that chitin now enforces this universally (so future Claude
  Code sessions know the gate has it, not just the prompt).

## Companion ticket actions

After this spec is implemented + merged:

- Replay the 2026-05-12 commits (Clawta's `6a0f13d`, `7785082`,
  `ec70eab`) under the new rule via the test harness; archive the
  results in `docs/archive/observations/2026-05-12-no-commit-to-main-replay.md`
  as proof the gap is closed.
- File a followup ticket to retire the `clawta-spawn-marker` row
  pattern (now redundant with proper exec attribution).
- Update `swarm-audit` (`swarm/bin/swarm-audit`) protected-branch
  detector to expect `agent=clawta` rows with `rule_id=
  no-commit-to-protected` from this point forward (rather than its
  current heuristic that scans `shell.exec` rows).
