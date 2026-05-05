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
file: apps/temporal-worker/src/activity.ts
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
file: apps/temporal-worker/src/dispatcher.ts
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
file: apps/temporal-worker/src/role-prompts.ts
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

---

```yaml
id: stale-doc-docs-archive-map-go-execution-kernel-internal-g-a01c3fad
discovered_at: 2026-05-05T04:00:08.751Z
discovered_by: swarm
severity: low
category: doc-debt
file: docs/archive-map.md
status: open
shipped_in:
description: |
  Stale doc reference detected at docs/archive-map.md:19.
  Reference: go/execution-kernel/internal/governance/ (no longer exists in the working tree).
  Context: `internal/policy`, `internal/invariant`, `internal/drift`, `internal/gate`, `in…
  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.
```

---

```yaml
id: stale-doc-docs-decisions-2026-05-03-no-g-apps-scheduler-dashboard-20199f38
discovered_at: 2026-05-05T04:00:08.751Z
discovered_by: swarm
severity: low
category: doc-debt
file: docs/decisions/2026-05-03-no-github-issues.md
status: open
shipped_in:
description: |
  Stale doc reference detected at docs/decisions/2026-05-03-no-github-issues.md:12.
  Reference: apps/scheduler-dashboard (no longer exists in the working tree).
  Context: The flat-file backlog is an interim solution. The upcoming `libs/scheduler` lib…
  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.
```

---

```yaml
id: stale-doc-docs-design-2026-05-04-bounded-go-execution-kernel-internal-b-909dc9f5
discovered_at: 2026-05-05T04:00:08.751Z
discovered_by: swarm
severity: low
category: doc-debt
file: docs/design/2026-05-04-bounded-context-v1.md
status: open
shipped_in:
description: |
  Stale doc reference detected at docs/design/2026-05-04-bounded-context-v1.md:413.
  Reference: go/execution-kernel/internal/blobs/ (no longer exists in the working tree).
  Context: - [ ] `go/execution-kernel/internal/blobs/` package: `Write(payload)
  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.
```

---

```yaml
id: stale-doc-docs-observations-2026-04-20-p-libs-contracts-src-chitindir-r-71cb6ab4
discovered_at: 2026-05-05T04:00:08.751Z
discovered_by: swarm
severity: low
category: doc-debt
file: docs/observations/2026-04-20-phase-a-restart-notes.md
status: open
shipped_in:
description: |
  Stale doc reference detected at docs/observations/2026-04-20-phase-a-restart-notes.md:36.
  Reference: libs/contracts/src/chitindir-resolve.test.ts (no longer exists in the working tree).
  Context: The plan writes `libs/contracts/src/chitindir-resolve.test.ts`.
  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.
```

---

```yaml
id: stale-doc-docs-observations-2026-04-22-a-docs-superpowers-specs-2026-04-416d4c68
discovered_at: 2026-05-05T04:00:08.751Z
discovered_by: swarm
severity: low
category: doc-debt
file: docs/observations/2026-04-22-autonomy-v1-post-mortem.md
status: open
shipped_in:
description: |
  Stale doc reference detected at docs/observations/2026-04-22-autonomy-v1-post-mortem.md:24.
  Reference: docs/superpowers/specs/2026-04-21-hermes-autonomy-v1-design.md (no longer exists in the working tree).
  Context: Spec: `docs/superpowers/specs/2026-04-21-hermes-autonomy-v1-design.md`
  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.
```

---

```yaml
id: stale-doc-docs-observations-2026-05-03-s-tests-backlog-entry-shape-test-6ec96526
discovered_at: 2026-05-05T04:00:08.751Z
discovered_by: swarm
severity: low
category: doc-debt
file: docs/observations/2026-05-03-skill-mining-report.md
status: open
shipped_in:
description: |
  Stale doc reference detected at docs/observations/2026-05-03-skill-mining-report.md:148.
  Reference: tests/backlog-entry-shape.test.ts (no longer exists in the working tree).
  Context: - cat /home/red/workspace/chitin/tools/lint/tests/backlog-entry-shape.test.ts |…
  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.
```

---

```yaml
id: stale-doc-docs-observations-2026-05-03-s-apps-tempor-67d957e4
discovered_at: 2026-05-05T04:00:08.751Z
discovered_by: swarm
severity: low
category: doc-debt
file: docs/observations/2026-05-03-skill-mining-report.md
status: open
shipped_in:
description: |
  Stale doc reference detected at docs/observations/2026-05-03-skill-mining-report.md:180.
  Reference: apps/tempor (no longer exists in the working tree).
  Context: - /home/red/workspace/chitin/.claude/worktrees/agent-af63538e4b495669b/apps/tem…
  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.
```

---

```yaml
id: stale-doc-docs-observations-2026-05-03-s-docs-observations-2026-05-02-o-c492a4d0
discovered_at: 2026-05-05T04:00:08.751Z
discovered_by: swarm
severity: low
category: doc-debt
file: docs/observations/2026-05-03-skill-mining-report.md
status: open
shipped_in:
description: |
  Stale doc reference detected at docs/observations/2026-05-03-skill-mining-report.md:180.
  Reference: docs/observations/2026-05-02-openclaw-usage- (no longer exists in the working tree).
  Context: - /home/red/workspace/chitin/.claude/worktrees/agent-af63538e4b495669b/apps/tem…
  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.
```

---

```yaml
id: stale-doc-docs-observations-2026-05-03-s-apps-tempor-8df11f23
discovered_at: 2026-05-05T04:00:08.751Z
discovered_by: swarm
severity: low
category: doc-debt
file: docs/observations/2026-05-03-skill-mining-report.md
status: open
shipped_in:
description: |
  Stale doc reference detected at docs/observations/2026-05-03-skill-mining-report.md:181.
  Reference: apps/tempor (no longer exists in the working tree).
  Context: - /home/red/workspace/chitin/.claude/worktrees/agent-af76f3397321eabbc/apps/tem…
  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.
```

---

```yaml
id: stale-doc-docs-observations-research-202-docs-reference-templates-SOUL--c68668fb
discovered_at: 2026-05-05T04:00:08.751Z
discovered_by: swarm
severity: low
category: doc-debt
file: docs/observations/research/2026-04-19-openclaw-soul-verification-suntzu.md
status: open
shipped_in:
description: |
  Stale doc reference detected at docs/observations/research/2026-04-19-openclaw-soul-verification-suntzu.md:46.
  Reference: docs/reference/templates/SOUL.md (no longer exists in the working tree).
  Context: The canonical bootstrap template, **`docs/reference/templates/SOUL.md`** in `op…
  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.
```
