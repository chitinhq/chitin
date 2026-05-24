---
status: open
owner: claude-code
kanban: t_c083fd6d
implementation_pr: null
superseded_by: null
effective_from: '2026-05-13'
effective_to: null
---

# Spec: worktree status report

Date: 2026-05-13
Status: spec — open
Kanban: `t_c083fd6d` (priority 25)
Source: `docs/archive/audits/2026-05-13-architecture-audit.md` — Top finding 5
Author: claude-code (operator-controlled, spec writer)

## Problem

Architecture audit 2026-05-13: 25 local worktrees with mixed
prefixes. Live `git worktree list` on 2026-05-13:

```
~/workspace/chitin                                       [main]
~/.cache/chitin/swarm-worktrees/swarm-admin-clsfr-bug-t_41b18659   [claude-code/admin-classifier-overbroad]
~/.cache/chitin/swarm-worktrees/swarm-claude-code-t_0c95013b       [swarm/claude-code-0c95013b]
~/.cache/chitin/swarm-worktrees/swarm-codex-t_084e79e0             [swarm/codex-084e79e0]
~/.cache/chitin/swarm-worktrees/swarm-copilot-t_2f7fffab           [swarm/copilot-2f7fffab]
~/.cache/chitin/swarm-worktrees/swarm-gov-bypass-tests-t_00306ffc  [codex/gov-bypass-regression-tests]
~/.cache/chitin/swarm-worktrees/swarm-nx-step2-t_cb0311ab          [codex/nx-step2-normalize-tags]
... (29 total)
```

Observed problems from the listing:

1. **Two naming conventions** in use: `swarm-<lane>-t_<id>`
   (most) versus `swarm-<slug>-t_<id>` (e.g.
   `swarm-admin-clsfr-bug-t_41b18659`, `swarm-gov-bypass-tests`,
   `swarm-nx-step2`). The slug variants pre-date the canonical
   `swarm-<lane>-<short>` naming.
2. **Two branch prefixes**: `swarm/<lane>-<short>` (e.g.
   `swarm/codex-084e79e0`) versus `<lane>/<slug>` (e.g.
   `codex/gov-bypass-regression-tests`,
   `claude-code/admin-classifier-overbroad`). Same lane, two
   branch namespaces. Reviewers and CI rules that match on
   `swarm/*` miss the second form.
3. **No mapping back to kanban** other than the embedded `t_<id>`
   in the worktree directory name. An agent or operator inspecting
   the kanban ticket has no way to find the worktree from the
   ticket without `grep` across `~/.cache`.
4. **Stale worktrees**: nothing in the listing tells the operator
   "this worktree's PR merged 6 days ago, you can prune it." 25
   open worktrees include some that almost certainly map to
   already-merged tickets.

This is the AI-navigation smell the audit names: an agent told
"work on `t_d3340a9e`" can't tell whether to grab
`swarm-claude-code-t_d3340a9e` (correct, recent dispatch) or
diff against `main` (wrong branch). One look-up table fixes it.

## Invariant (the claim)

> For every git worktree in `git worktree list` for this repo,
> `chitin-kernel worktree status` produces exactly one row with:
> `path`, `branch`, `kanban_ticket`, `pr_number`, `pr_state`,
> `owner_lane`, `last_commit_ts`, `age_days`. A worktree without a
> derivable `kanban_ticket` is flagged as `orphan`; a worktree
> whose `pr_state` is `MERGED` for more than 7 days is flagged
> `stale`.

A "lane" is one of: `clawta`, `codex`, `copilot`, `claude-code`,
`gemini`, `human` (operator-driven branches), `unknown`.

## Decision

Add a `chitin-kernel worktree status` Go subcommand that produces
the table above. The data sources are:

1. `git worktree list --porcelain` — paths and branches.
2. The branch / directory name — kanban ticket id via
   `t_[0-9a-f]{8}` regex (existing convention).
3. `gh pr list --json` — PR number and state per branch.
4. `git log -1 --format=%ct <branch>` — last activity timestamp.
5. Operator-side cache file
   `~/.cache/chitin/worktree-status.json` — for fast iteration;
   refreshed best-effort by the default text report.

Output is text by default (one row per worktree), JSON with
`--json`, and `--stale` filters to flag rows. Read-only machine
outputs such as `--json` and `--prune-eligible` do not refresh the
operator cache.

The Go subcommand replaces ad-hoc bash that has been informally
attempted in `scripts/` over the past month. Go because the data
joins (worktree × kanban × gh) get unwieldy in bash and because
this surface gets read by automation (poller, audit) where typed
output matters.

## In scope

1. **`chitin-kernel worktree status`** subcommand in Go:
   - `go/execution-kernel/cmd/chitin-kernel/worktree.go`
   - `go/execution-kernel/internal/worktree/` package with the
     table-build logic.
2. **Default output**: aligned text table sorted by `age_days`
   ascending, with bold `[stale]` / `[orphan]` tags.
3. **`--json` flag**: machine-readable for the poller / audit
   scripts.
4. **`--stale` flag**: filter to rows the operator should prune.
5. **`--prune-eligible`** flag: list paths the operator can pass to
   `git worktree remove`. Does NOT remove anything itself.
6. **Naming-convention linter**
   (`scripts/check-worktree-naming.sh`): warns (does not fail) on
   new worktrees created with the legacy `<lane>/<slug>` branch
   pattern. Warn-only so existing worktrees don't break CI; once
   they're drained, the linter is upgraded to fail.
7. **Documentation**: `docs/runbooks/worktree-conventions.md`
   stating the canonical pattern (`swarm/<lane>-<short>` for swarm
   work, `<lane>/<slug>` deprecated, operator-local worktrees may
   use any name but should embed `t_<id>` for ticket linkage).

## Out of scope (followups)

- Auto-pruning of stale worktrees — destructive; operator-driven.
  The command tells you what to prune; you run `git worktree remove`.
- Migrating existing `<lane>/<slug>` worktrees to the canonical
  pattern — rename-in-place is risky for in-flight worktrees.
  Lifecycle takes care of it: as those branches merge, new ones
  use the canonical pattern.
- Kanban-side updates to embed worktree path in the ticket row —
  the `t_<id>` extraction is one direction; the reverse mapping
  is the new `worktree status` table.
- Cross-repo worktree status (multiple chitin clones on the
  operator's machine) — single-repo for now.

## Approach detail

### Column derivations

| Column          | Source                                                             |
|-----------------|--------------------------------------------------------------------|
| `path`          | `git worktree list --porcelain` → `worktree` field                 |
| `branch`        | same → `branch` field                                              |
| `kanban_ticket` | regex `t_[0-9a-f]{8}` matched against branch then path; else null  |
| `pr_number`     | `gh pr list --search "head:<branch>" --json number` first match    |
| `pr_state`      | same call → `state` (OPEN/MERGED/CLOSED), `NONE`, or `UNKNOWN` when GitHub enrichment is unavailable |
| `owner_lane`    | branch prefix: `swarm/<lane>-<short>` → `<lane>`; else heuristic   |
| `last_commit_ts`| `git log -1 --format=%ct <branch>`                                 |
| `age_days`      | now - last_commit_ts                                               |

Tags applied to the row:

- `[stale]` — `pr_state == MERGED` and `now - merge_ts > 7d`. Or
  `pr_state == NONE` and `age_days > 14`.
- `[orphan]` — `kanban_ticket == null`.
- `[in-progress]` — `pr_state == OPEN`.
- `[github-unavailable]` — `gh pr list` failed or timed out; PR state
  is reported as `UNKNOWN` and rows are not marked stale from missing
  PR data alone.
- `[ready]` — `kanban_ticket != null` and `pr_state == NONE` and
  `age_days < 1` (just-created worktree).

### Example output

```
$ chitin-kernel worktree status

PATH                                                                BRANCH                              TICKET       PR      AGE  TAGS
~/workspace/chitin                                                  main                                -            -       0d   [primary]
~/.cache/chitin/swarm-worktrees/swarm-claude-code-t_d3340a9e        swarm/claude-code-d3340a9e          t_d3340a9e   #576    1d   [stale: merged 1d ago; PR #576]
~/.cache/chitin/swarm-worktrees/swarm-codex-t_084e79e0              swarm/codex-084e79e0                t_084e79e0   #575    1d   [stale: closed 1d ago; PR #575]
~/.cache/chitin/swarm-worktrees/swarm-gov-bypass-tests-t_00306ffc   codex/gov-bypass-regression-tests   t_00306ffc   #560    8d   [stale; legacy-naming]
~/.cache/chitin/swarm-worktrees/swarm-nx-step2-t_cb0311ab           codex/nx-step2-normalize-tags       t_cb0311ab   -       12d  [orphan-pr; legacy-naming]
...

Summary: 25 worktrees | 4 in-progress | 13 stale | 2 orphan | 8 legacy-naming
Prune candidates: 13 — run `chitin-kernel worktree status --prune-eligible | xargs -I{} git worktree remove {}`.
```

### Operator workflow

```bash
# Morning routine
chitin-kernel worktree status --stale

# Prune cleanly merged worktrees
chitin-kernel worktree status --prune-eligible | while read p; do
  git worktree remove "$p"
done
```

The `/queue` skill is updated in a followup to include the
worktree-status summary line so the operator sees pruning
opportunities alongside kanban state.

## Verification

- **Determinism**: two consecutive runs on the same tree produce
  the same JSON (modulo timestamps).
- **PR join correctness**: a worktree on a branch with an open PR
  reports the correct `pr_number` and `pr_state == OPEN`.
- **Stale detection**: a worktree on a branch whose PR merged 8
  days ago reports `[stale]`.
- **Orphan detection**: a worktree whose branch contains no
  `t_<id>` token reports `[orphan]`.
- **Performance**: `chitin-kernel worktree status` finishes in
  under 3 seconds on a tree with 30 worktrees (real-world budget
  given gh API latency).

## Done-condition

- `chitin-kernel worktree status` exists, default + `--json` +
  `--stale` + `--prune-eligible` all functional.
- `scripts/check-worktree-naming.sh` exists, warns on legacy
  branch namespace, CI-wired (warn-only).
- `docs/runbooks/worktree-conventions.md` exists and is linked
  from the operator runbook index.
- The `/queue` skill output references the worktree status
  command (followup PR).

## Effort

S. The Go subcommand is the bulk: ~1 day including tests. Linter
+ docs is half a day. Total ~1.5 days.
