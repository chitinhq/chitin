# Mock-worker dogfood — Slice F lifecycle validation

Captures the end-to-end walk of one real research ticket
(`t_e1d0e815` — "Research: explain why lockdown dominates copilot-cli
denies") by Claude Code in the `researcher` role, to validate that
Slices A–E hang together before clawta-poller starts dispatching
autonomously.

## What we validated

| Slice | Validation                                               |
|-------|----------------------------------------------------------|
| A     | `kanban-flow start` / `done` transitioned + audited      |
| B     | `researcher` role lifecycle (ready→in_progress→done, no PR) |
| C     | (Not validated yet; poller dry-run only — Slice F doesn't dispatch via poller) |
| D     | (Not validated yet; lobster workflow not invoked — manual flow) |
| E     | (Out of scope — separate repo)                           |

Slice F validates the **manual** path through the SDLC. The autonomous
path (clawta-poller → lobster → worker) gets validated when the
operator marks a ticket `ready` and lets the poller pick it up.

## The walk

### 1. Take ownership

The original ticket was assigned to `codex` but had no `task_runs` row
(it was one of the 6 stuck-overnight tickets that triggered this whole
epic). Reassigning to a different worker:

```bash
hermes kanban --board chitin assign t_e1d0e815 claude-code
```

### 2. Flip to in_progress + announce role

```bash
kanban-flow start t_e1d0e815 --author claude-code
hermes kanban --board chitin comment t_e1d0e815 --author claude-code \
  "Role: researcher. Dogfooding the swarm SDLC lifecycle ..."
```

**Audit produced:**
- One comment ("Picking up at <ts>") from `kanban-flow start`
- One role-announcement comment
- One `task_events` row: `{from: ready, to: in_progress, by: claude-code}`

### 3. Do the research

The researcher role's recipe says: investigate → cite specific
sources → synthesize → no PR. Concrete moves:

- Read ~/.chitin/gov-decisions-*.jsonl
- Build a Counter over (day, rule_id, agent) tuples
- Discover the temporal concentration (2090 of 2094 denies on a single day)
- Cross-reference against `feedback_sticky_state_needs_recovery_automation.md` memory
- Validate root-cause hypothesis matches existing follow-up ticket

### 4. Post findings comment

The researcher SKILL.md prescribes the comment structure:

- **Question** (restated)
- **Answer** (1–3 sentences, no hedging)
- **Evidence** (bullets with citations)
- **Confidence** (single-line)
- **Root-cause bucket** (single-line)
- **Follow-ups** (concrete tickets to file, or "None")

The actual comment is the public record on t_e1d0e815. Read it for the
exemplar shape.

### 5. Close

```bash
kanban-flow done t_e1d0e815 --result "<one-line summary>" --author claude-code
```

The researcher lifecycle has no `code_review` state. The output IS the
findings comment. `kanban-flow done` writes the audit event + flips to
done + records the result text.

## What the board shows afterward

```
✓ t_e1d0e815  done      claude-code      Research: explain why lockdown dominates copilot-cli denies
```

Comments are the audit trail:
- Old "dispatched to codex" comments (stale, from before the takeover)
- Take-ownership comment
- Role announcement
- Findings comment
- `kanban-flow done` summary

Anyone reading the ticket cold can reconstruct: who picked it up, what
role they used, what they found, why it closed, and what follow-ups
the closing operator chose (or chose not) to file.

## Validation criteria — what good looks like

- ✅ Every transition has both a kanban comment AND a `task_events`
  row. Confirmed via:
  `sqlite3 ~/.hermes/kanban/boards/chitin/kanban.db "SELECT kind,payload FROM task_events WHERE task_id='t_e1d0e815' ORDER BY id"`
- ✅ The findings comment cites specific evidence (file paths +
  date counts + cross-reference to existing memory).
- ✅ No PR was opened (correct for researcher role).
- ✅ A follow-up ticket was correctly NOT filed (the load-bearing
  follow-up — `t_c8307795` rotator — already exists).
- ✅ The board status reflects reality: the ticket is `done` because
  the research is published, not because a PR landed.

## Anti-patterns observed (and avoided)

- **Filing a hypothesis-only finding.** Tempted at first to claim
  "probably the 5/4 incident" — but the SKILL.md says "no hedging".
  Forced the precise day-level breakdown into the comment, which made
  the answer crisp.
- **Bundling a fix.** Tempted to also propose a `--exclude-incident`
  flag for `analysis.decisions`. The recipe says "file follow-ups;
  don't bundle". The flag idea ended up in the "Follow-ups" section
  as an optional ticket — not opened, but documented as available.

## Next dogfood — autonomous path

The manual walk validates the lifecycle. The autonomous walk
(clawta-poller → lobster → worker with role inlining) needs a
separate ticket: mark a ticket `ready` with `--assignee codex` or
similar, install the poller/runtime guards via
`swarm/bin/install-clawta-poller.sh`, and watch the board state
transitions arrive without operator intervention. The repo-owned
runtime guard thresholds and OpenClaw cron ownership are documented in
`docs/runbooks/swarm-runtime-guards.md`.
