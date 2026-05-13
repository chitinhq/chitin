# /queue — Operator queue + collaborative triage

Show what's on the operator's kanban plate + which PRs are awaiting
merge, then work through it together. Project-scoped to chitin.

## Operator identity

The operator handle defaults to `$KANBAN_OPERATOR_ID` if set, else
`$USER`. Override per-invocation by supplying as the first positional
arg: `/queue red` (legacy) or `/queue alice`.

## Usage

```
/queue              — Full view: kanban + PRs awaiting merge
/queue kanban       — Only kanban items
/queue prs          — Only PRs awaiting operator merge
/queue stale        — Filter to items past their stale threshold
/queue <ticket-id>  — Deep-dive one item with action plan
```

## What it pulls (in order)

### 1. Kanban tickets assigned to the operator

```bash
OPERATOR="${KANBAN_OPERATOR_ID:-$USER}"
sqlite3 ~/.hermes/kanban/boards/chitin/kanban.db <<SQL
SELECT
  id,
  status,
  COALESCE(priority, 0) AS p,
  ROUND((strftime('%s','now') - COALESCE(started_at, created_at)) / 3600.0, 1) AS age_hours,
  substr(title, 1, 90) AS title
FROM tasks
WHERE assignee = '$OPERATOR'
  AND status NOT IN ('done', 'archived')
ORDER BY status, p DESC, created_at ASC;
SQL
```

Group output by status (`triage → ready → in_progress → blocked`).
Per item, one line: `[P<priority>] [<age>h] <id> <title-truncated>`.

### 2. PRs awaiting operator merge

```bash
gh pr list --repo chitinhq/chitin --state open \
  --json number,title,author,createdAt,mergeStateStatus,reviewDecision,headRefName,statusCheckRollup \
  --limit 30
```

Filter to PRs where any of:

- Author is the operator's GitHub login (operator-local; configure
  via `gh auth status` or pass `--author`)
- Head branch matches `spec/`, `swarm/`, `clawta/` (autonomous swarm
  or spec-writer output that needs an operator merge call)

Surface: `#<num> [<merge-state>] [<age>h] [<ci>] <title>` plus a
`Copilot: N findings` tag if any review comments are unaddressed.

### 3. Stale flags

Per-lane thresholds (override via env if needed):

| Lane | Stale threshold |
|---|---|
| `triage` | 48h since created (operator hasn't groomed it) |
| `ready` | 6h since promoted (clawta-poller should have picked it up) |
| `in_progress` | 24h with no kanban comment activity |
| `blocked` | 12h since blocked (operator decision overdue) |

PRs: stale if > 24h since last commit with green CI and no merge.

### 4. Per-item recommended action

For each kanban item, compose a one-line recommendation tagged:

- `[spec]` — vague triage ticket needs a spec before a worker can act
- `[dispatch]` — ready ticket with a clear terminal-lane reassignment suggestion (codex/copilot/claude-code/gemini)
- `[merge]` — code_review or PR-recorded ticket with green CI + clear reviews
- `[decide]` — blocked ticket waiting on operator call; surface the blocker verbatim
- `[defer]` — low-priority + stale; recommend archive
- `[unstick]` — in_progress past stale threshold with no activity; recommend reset to ready

For each PR, tag:

- `[merge-now]` — clean, CI green, reviews clear
- `[copilot-findings]` — Copilot review has substantive comments; file followup tickets first
- `[ci-fail]` — CI failing; needs fix before merge
- `[adversarial-needed]` — high-risk (touches gov, governance, policy); operator should read in detail

## Output shape

```
=== Kanban queue for <operator> ===

triage (N items)
  [P90] [12h]  t_xxxxxxxx  Title… [spec] "write the spec next?"
  [P70] [8h]   t_yyyyyyyy  Title… [defer] "low-pri + stale; archive?"

ready (M items)
  ...

in_progress (K items)
  ...

blocked (J items)
  ...

=== PRs awaiting your merge (P items) ===
  #545 [CLEAN]    [3h]  [7/7 ok]  spec: Hermes+Clawta amendment [merge-now]
  #513 [UNSTABLE] [16h] [6/7 ok]  hardening: execute_code bypass [copilot-findings]
  ...

=== Stale flags ===
  - <ticket-id> sat in <lane> for <hours>h — recommend <action>
  - PR #<n> open with green CI for <hours>h — recommend <merge-or-defer>

=== Suggested batch ops ===
  - merge all green: #<a>, #<b>, …
  - spec the high-priority triage: t_<x>, t_<y>
  - reassign blocked tickets: t_<z>
```

End with: **"Pick a number / ticket id to deep-dive, or tell me which
batch to run (`merge all green`, `spec the triage`, etc.)."**

## Variants

### `/queue kanban`

Skip the PRs section. Useful when triaging the board only.

### `/queue prs`

Skip the kanban section. Useful at end-of-day to clear merge backlog.

### `/queue stale`

Show ONLY items past their stale threshold. Operator-grooming view.

### `/queue <ticket-id>`

Deep-dive one item:

```bash
sqlite3 ~/.hermes/kanban/boards/chitin/kanban.db \
  "SELECT body FROM tasks WHERE id='<ticket-id>'"

sqlite3 ~/.hermes/kanban/boards/chitin/kanban.db \
  "SELECT datetime(created_at,'unixepoch','localtime'), author, body
   FROM task_comments WHERE task_id='<ticket-id>'
   ORDER BY created_at DESC LIMIT 10"
```

Produce a focused action plan: what the ticket asks, what's been
done, what's blocking, what the concrete next step is. Offer to take
that step.

## Anti-patterns

- **Don't auto-act.** Surface and recommend. Operator decides. The
  merge / reassign / spec-write call is always operator-confirmed.
- **Don't claim merge authority** on `swarm/*` PRs. Per
  `docs/superpowers/specs/2026-05-12-clawta-hermes-architecture.md`
  (the amended spec): operator owns the merge button on autonomous
  swarm output.
- **Don't ingest log files** for chain data. Per
  `docs/superpowers/specs/2026-05-12-swarm-observability-via-chitin-cli.md`:
  chain queries go through `chitin-kernel` CLI, not `~/.openclaw/logs/`
  scraping. This skill stays kanban-side (sqlite + gh) for its data
  source; if it needs chain stats, it calls `chitin-kernel`.

## Related

- `swarm/bin/swarm-audit` — daily operator audit (8am cron). `/queue`
  is the on-demand version of the same data shape.
- `scripts/kanban-flow` — the lifecycle helper; many `/queue`
  recommendations resolve to `kanban-flow start/block/done/demote`.
- `docs/runbooks/swarm-sdlc-status-machine.md` — the canonical state
  machine `/queue` is presenting.
