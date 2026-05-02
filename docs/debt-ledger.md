# Debt Ledger

This file tracks known technical, documentation, infrastructure, and governance debt in the project. Each entry follows the schema below:

```yaml
id: <slug>
discovered_at: <ISO-8601>
discovered_by: <swarm | operator | user>
severity: blocking | high | medium | low
category: code-debt | doc-debt | infra-debt | governance-debt
file: <primary file or 'cross-cutting'>
description: |
  What's wrong / why it's debt / what scenario it bites in.
status: open | claimed | shipped
shipped_in: <PR # if shipped>
```

---

## Entries

---

```yaml
id: load-marker-duplication
discovered_at: 2024-04-15T10:00:00Z
discovered_by: operator
severity: medium
category: code-debt
file: python/swarm_health.py
status: open
shipped_in: 
description: |
  The _load_marker_count logic is duplicated between swarm_health.py and swarm_runs.py. This duplication risks divergence and bugs if one is updated without the other. Discovered during adversarial review in PR #127.
```

---

```yaml
id: writeworktree-overwrite
discovered_at: 2024-03-10T14:30:00Z
discovered_by: swarm
severity: high
category: code-debt
file: cross-cutting
status: open
shipped_in: 
description: |
  writeWorktreeClaudeSettings overwrites worktree state by design, but this was undocumented and led to a security-shaped failure (bucket-B incident). Needs explicit documentation and safer handling.
```

---

```yaml
id: missing-debt-ledger
discovered_at: 2024-05-02T12:58:00Z
discovered_by: operator
severity: blocking
category: governance-debt
file: docs/debt-ledger.md
status: open
shipped_in: 
description: |
  Absence of a curated debt-ledger makes technical debt invisible until it causes incidents. This file seeds the ledger and should be referenced by grooming and analysis tools.
```

---

```yaml
id: outdated-docs-api
discovered_at: 2024-04-20T09:45:00Z
discovered_by: user
severity: medium
category: doc-debt
file: docs/api.md
status: open
shipped_in: 
description: |
  API documentation is out of date with recent changes to the event schema. This can mislead implementers and external integrators.
```

---

```yaml
id: infra-ci-flakiness
discovered_at: 2024-03-28T16:20:00Z
discovered_by: operator
severity: high
category: infra-debt
file: .github/workflows/ci.yml
status: open
shipped_in: 
description: |
  CI workflow is flaky due to inconsistent dependency caching and race conditions in parallel jobs. Causes spurious failures and slows down development.
```
