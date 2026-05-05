# Debt Ledger

This file tracks known technical, documentation, infrastructure, and
governance debt in the project. Each entry follows the schema below:

```yaml
id: <slug>
discovered_at: <ISO-8601>
discovered_by: <swarm | operator | user>
severity: blocking | high | medium | low
category: code-debt | doc-debt | infra-debt | governance-debt
file: <primary file or 'cross-cutting'>
status: open | claimed | shipped
shipped_in: <PR # if shipped>
description: |
  What's wrong / why it's debt / what scenario it bites in.
```

> **Scope note:** This PR ships the debt-ledger document. The Python
> analysis-lib pieces from the original `tech-debt-ledger` backlog
> entry (`python/analysis/debt.py` extension + tests +
> `parse-backlog.ts` integration) are split into a follow-up entry
> (`debt-ledger-analysis-loader`) so the doc can land independently
> for the GROOM and analyst roles to consume immediately. See
> `docs/swarm-backlog.md`.

## Entries

---

```yaml
id: load-marker-duplication
discovered_at: 2026-05-02T16:35:00Z
discovered_by: operator
severity: medium
category: code-debt
file: python/analysis/swarm_health.py
status: open
shipped_in:
description: |
  `_load_marker_count` in swarm_health.py and `_load_markers` in
  swarm_runs.py iterate the same marker directory with the same
  parse logic. swarm_runs.py is envelope-driven (returns SwarmRun
  records); swarm_health.py needs marker count alone (for the
  in-flight-or-lost calculation). Refactor: expose a marker iterator
  helper in swarm_runs.py and have swarm_health.py call it. Risk if
  not done: a marker-format change updates one path and not the
  other. Discovered during PR #127's adversarial review (finding C1).
```

---

```yaml
id: writeworktree-overwrite-undocumented
discovered_at: 2026-05-02T14:30:00Z
discovered_by: swarm
severity: high
category: doc-debt
file: apps/runner/src/activity.ts
status: open
shipped_in:
description: |
  `writeWorktreeClaudeSettings` overwrites whatever main has at
  `worktree/.claude/settings.json` to install the chitin gate hook
  before spawning claude-code-headless. This was an undocumented
  side effect that became the bucket-B contamination root cause on
  2026-05-02 (4 PRs shipped only the settings-overwrite as their
  diff). PR #123 reverted the artifact at apply-time. Remaining
  debt: the worker's behavior is still undocumented in the function
  signature / interface — a future contributor changing the apply
  step's diff heuristic could reintroduce the failure mode without
  realizing the contract. Add a comment block on
  writeWorktreeClaudeSettings naming the bucket-B incident as the
  reason for the apply-step revert.
```

---

```yaml
id: dispatcher-ignores-blocks-field
discovered_at: 2026-05-02T16:50:00Z
discovered_by: operator
severity: high
category: code-debt
file: apps/runner/src/dispatcher.ts
status: claimed
shipped_in:
description: |
  `pickEntryToDispatch` ignores the `blocks:` YAML field, so any
  entry with `blocks: [other-entry]` is claimable while
  other-entry is in-flight. Produced PR #133 + PR #135 today as
  redundant swarm dispatches that had to be closed. Backlog entry
  filed: `dispatcher-respect-blocks-field` (PR #139).
```

---

```yaml
id: stub-role-prompt-templates
discovered_at: 2026-05-02T16:00:00Z
discovered_by: operator
severity: medium
category: code-debt
file: apps/runner/src/role-prompts.ts
status: open
shipped_in:
description: |
  10 of the 12 roles in the role registry (researcher, product,
  groomer, architect, qa, gatekeeper, tech-writer, analyst,
  refactorer, debt-curator) use `buildStubPrompt` — a placeholder
  that names the role + entry but doesn't have a dedicated prompt
  template. The agent is told to "treat the entry below as a
  generic programmer task" which defeats the role-typing's purpose.
  Backlog entries to ship the real templates one at a time
  (researcher first via `external-signal-collector`; reviewer
  shipped in PR #134 via `agent-adversarial-review-pass`). Track
  here so progress is visible across the role taxonomy.
```
