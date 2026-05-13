# Worktree Conventions

Chitin swarm worktrees use one canonical branch pattern:

```text
swarm/<lane>-<ticket-short>
```

`<lane>` is one of `clawta`, `codex`, `copilot`, `claude-code`,
`gemini`, or `human`. `<ticket-short>` is the eight hex characters from
the kanban ticket id without the `t_` prefix.

Example:

```text
worktree path: ~/.cache/chitin/swarm-worktrees/swarm-codex-t_c083fd6d
branch:        swarm/codex-c083fd6d
ticket:        t_c083fd6d
```

The older `<lane>/<slug>` branch pattern, such as
`codex/gov-bypass-regression-tests`, is legacy naming. Existing
worktrees may drain naturally, but new swarm dispatches should use the
canonical `swarm/<lane>-<ticket-short>` branch namespace so CI,
reviewers, and automation can find them consistently.

Operator-local worktrees may use any path or branch name, but embedding
the `t_<id>` token in the worktree path keeps `chitin-kernel worktree
status` able to link the worktree back to kanban.

## Status Report

Use the kernel report before pruning or picking up work:

```bash
chitin-kernel worktree status
```

The default output is an aligned table sorted by `age_days` ascending.
Rows include the worktree path, branch, kanban ticket, PR number, PR
state, owner lane, last commit timestamp, age in days, and tags.

Machine-readable output is newline-delimited JSON:

```bash
chitin-kernel worktree status --json
```

To inspect stale candidates only:

```bash
chitin-kernel worktree status --stale
```

To produce paths suitable for an operator-reviewed prune loop:

```bash
chitin-kernel worktree status --prune-eligible
```

`--prune-eligible` only prints paths. It never removes worktrees.
