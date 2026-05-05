# Decision: No GitHub Issues for Swarm Work-Tracking (2026-05-03)

## Decision
GitHub issues are reserved for external, human-filed bug reports. The Chitin swarm does not create issues for work-tracking or kanban. Instead, all swarm work is tracked in `docs/swarm-backlog.md`, which serves as the authoritative kanban surface.

## Why
- **Machine-readable backlog:** The markdown backlog supports dependencies (`blocks`), status, and role fields, enabling structured, automatable workflows that GitHub issues cannot match.
- **Workflow alignment:** PRs reference backlog entry IDs, and the lessons extractor reads from PRs, not issues. The dispatcher and audit flows already route around issues.
- **Operator clarity:** There is no operator-side gain to duplicating work-tracking in issues.

## Forward Direction
The flat-file backlog is an interim solution. The upcoming `libs/scheduler` library will subsume the backlog, with backlog entries becoming scheduler items. These will retain fields like `status`, `tier`, `role`, and `blocks`, and add scheduling metadata (e.g., deadlines, preferred windows). The planned `apps/scheduler-dashboard` Angular UI will provide a unified kanban view for both life-scheduling and swarm work, eliminating the need for a separate swarm-backlog-kanban app.

## Exception
This policy applies only to swarm work-tracking. Real bug reports filed by humans should continue to use GitHub issues as normal.

## When to Revisit
Revisit this decision if:
- Swarm scaling or operator visibility suffers.
- External contributors require an issues-based entrypoint.
- Scheduler absorption of the backlog stalls or fails.
