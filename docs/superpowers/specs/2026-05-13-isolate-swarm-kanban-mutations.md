---
status: open
owner: claude-code
kanban: t_77f5b407
implementation_pr: null
superseded_by: null
effective_from: '2026-05-13'
effective_to: null
---

# Spec: route all swarm kanban mutations through one wrapper

Date: 2026-05-13
Status: spec — open
Kanban: `t_77f5b407` (priority 25)
Source: `docs/audits/2026-05-13-architecture-audit.md` — Top finding 2
Author: claude-code (operator-controlled, spec writer)

## Problem

Architecture audit 2026-05-13: `swarm/` is 21 files but produces 16
`kanban` hits, with heavy overlap on `clawta`, `hermes`, and
`openclaw`. Dispatch and kanban-flow commits are still landing every
few days. A small, volatile surface with N independent paths into
the same SQLite database is the textbook setup for a state-mutation
incident: a poller comments under one author, an audit script under
another, a Lobster workflow writes a row directly, and the operator
loses the ability to reason about *who did what* without diffing
the audit table.

`scripts/kanban-flow` already exists and is the de-facto canonical
wrapper for operator-side and clawta-side transitions. Its preamble
documents the invariant: "every status change emits both a
task_comment AND a task_events row." That invariant is currently
maintained by social convention, not enforced. Anyone holding the
DB path can `sqlite3` directly and skip the audit row.

Observed callers today (from `grep -ln kanban swarm/`):

- `swarm/bin/clawta-poller` — uses `kanban-flow` ✓
- `swarm/bin/swarm-audit` — read-only queries on `kanban.db` ✓
- `swarm/bin/architecture-audit` — filed 5 tickets today via
  `chitin-kernel` (we think). Need to verify the path.
- `swarm/bin/clawta-pr-lifecycle` — unclear; likely transitions on
  PR merge events. Needs audit.
- `swarm/bin/clawta-invariants` — unclear.
- `swarm/workflows/kanban-dispatch.lobster` — Lobster spec.
  Probably the call site for the chain dispatcher.
- `swarm/workflows/clawta_decisions.py` — needs audit.

The risk is not theoretical: the recent dispatch refactor (PR #576)
and the EPIPE fix (PR #579) both touched paths that mutate kanban
state. A surface this volatile needs one chokepoint.

## Invariant (the claim)

> Every kanban *mutation* (insert, update, delete on `tasks`,
> `task_comments`, `task_events`) performed by any `swarm/*` code
> path goes through `scripts/kanban-flow` (or its Go successor),
> which guarantees: (1) a `task_events` row is written, (2) the
> author is recorded, (3) the transition is legal under the SDLC
> state machine.

A violation is any swarm code path that:

1. Opens `~/.hermes/kanban/boards/chitin/kanban.db` with
   write/insert intent, OR
2. Shells out to `sqlite3 ... UPDATE ...` against the kanban DB, OR
3. Mutates kanban state via a non-`kanban-flow` API.

Read-only access (SELECT) is unrestricted — `swarm-audit` and
`/queue` both need it.

## Decision

Two-part: (1) declare `kanban-flow` (or its Go successor) the only
sanctioned mutation surface; (2) add a CI check + a runtime guard
that catch direct mutations.

The CI check is static — it greps `swarm/` for write SQL against
the kanban DB and for direct calls to a non-`kanban-flow` mutation
API. The runtime guard is a `BEGIN` trigger on the DB that records
caller stack into a "mutations" audit table, so a bypass leaves
forensic evidence even if static analysis misses it. Bypass is
*recordable*, not preventable, because we can't sandbox a script
that the operator runs.

### Forward direction: Go reimplementation

`scripts/kanban-flow` is 400+ lines of bash with subprocess
escaping that gets harder every release. The medium-term target is
`chitin-kernel kanban <verb>` — a Go subcommand that does the same
thing with proper type-checking and shared DB transaction handling.
This spec deliberately keeps the bash version as the canonical
chokepoint for now; the Go migration is a followup ticket
(`t_77f5b407-followup-1`, to be filed on merge).

## In scope

1. **`scripts/check-swarm-kanban-isolation.sh`** — static linter
   that greps `swarm/` for the violation patterns and exits non-zero
   on hit. CI-wired.
2. **`swarm/workflows/clawta_decisions.py` + `clawta-pr-lifecycle`
   + `clawta-invariants` audit** — read each, identify mutation
   paths, route them through `kanban-flow`. If a transition isn't
   supported by `kanban-flow`, add the subcommand.
3. **`kanban-flow` extensions for swarm-only verbs** — if the audit
   surfaces a transition the current wrapper doesn't support (e.g.
   `pr-merged`, `invariant-flag`), add it with the same
   "comment + event + state change in one transaction" contract.
4. **Audit-trigger** — a SQLite trigger that fires on any
   `INSERT|UPDATE|DELETE` to `tasks`, `task_comments`, `task_events`
   and records the connection's `application_id` into a new
   `kanban_mutations_log` table. `kanban-flow` sets a known
   `application_id`; anything else is recorded as `unknown`.
5. **Documentation update** — `docs/runbooks/swarm-sdlc-status-machine.md`
   already exists; add a §"Mutation channel" naming `kanban-flow`
   as the only sanctioned writer.

## Out of scope (followups)

- Go rewrite of `kanban-flow` (`chitin-kernel kanban <verb>`) —
  filed as separate ticket on merge of this spec.
- Hardening against the operator running `sqlite3` by hand —
  trust boundary, not a code boundary. Audit table catches it
  forensically; that's enough.
- Reformulating the kanban DB as an event-sourced log — out of scope
  here; this spec only ensures the *current* schema's mutations are
  chokepointed.

## Approach detail

### Detection patterns (CI linter)

```
sqlite3[[:space:]].*kanban(\.db)?[[:space:]].*\b(INSERT|UPDATE|DELETE)\b
\.execute\(\s*['"](INSERT|UPDATE|DELETE)[^'"]*FROM\s+task
import\s+sqlite3.*\n.*kanban\.db
```

Restricted to `swarm/` paths, excluding tests (`swarm/tests/`) and
`scripts/kanban-flow` itself.

### SQLite mutation audit trigger

Installed into the chitin kanban DB once, idempotent. Schema:

```sql
CREATE TABLE IF NOT EXISTS kanban_mutations_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL DEFAULT (strftime('%s','now')),
  table_name TEXT NOT NULL,
  op TEXT NOT NULL,           -- INSERT/UPDATE/DELETE
  task_id TEXT,               -- best-effort extraction
  application_id TEXT,        -- set by kanban-flow; null/unknown otherwise
  pid INTEGER                 -- caller pid
);

CREATE TRIGGER IF NOT EXISTS audit_tasks_write
AFTER INSERT OR UPDATE OR DELETE ON tasks
BEGIN
  INSERT INTO kanban_mutations_log (table_name, op, task_id, application_id)
  VALUES ('tasks', CASE
    WHEN OLD.id IS NULL THEN 'INSERT'
    WHEN NEW.id IS NULL THEN 'DELETE'
    ELSE 'UPDATE' END,
    COALESCE(NEW.id, OLD.id),
    (SELECT value FROM pragma_application_id())
  );
END;
-- repeat for task_comments, task_events
```

`kanban-flow` sets `PRAGMA application_id = <hash of 'kanban-flow'>`
on every connection. The trigger captures it; a direct `sqlite3`
invocation leaves it null, so the audit row is recognizable. Triggers
on `task_comments` and `task_events` are analogous.

### Operator query

```bash
sqlite3 ~/.hermes/kanban/boards/chitin/kanban.db <<'SQL'
SELECT datetime(ts,'unixepoch','localtime'), table_name, op, task_id, application_id
FROM kanban_mutations_log
WHERE application_id IS NULL OR application_id != <kanban-flow id>
ORDER BY ts DESC LIMIT 20;
SQL
```

This is the bypass canary: any row in the result is a non-sanctioned
write that needs investigation.

## Verification

- **Static linter passes** on a clean tree after the audit step.
- **Fixture PR** that adds `swarm/bin/bad.py` with
  `sqlite3.connect(...).execute("UPDATE tasks SET ...")` — CI rejects.
- **Trigger forensics:** run a manual `sqlite3 kanban.db "UPDATE tasks SET priority=99 WHERE id='t_x'"`,
  confirm `kanban_mutations_log` has a row with `application_id IS NULL`.
- **Round-trip:** run `kanban-flow start t_x`; confirm the audit row
  has the sanctioned `application_id`, and the existing
  `task_events` + `task_comments` rows are present (regression
  check on `kanban-flow`'s own contract).

## Done-condition

- `scripts/check-swarm-kanban-isolation.sh` exists, CI-wired, green
  on `main`.
- Every mutation path in `swarm/` either uses `kanban-flow` or
  appears as a justified allowlist entry.
- The audit trigger is installed on the production kanban DB;
  `kanban_mutations_log` exists; `kanban-flow` sets
  `application_id` on connect.
- `docs/runbooks/swarm-sdlc-status-machine.md` names `kanban-flow`
  as the canonical mutation channel.
- Followup ticket filed for the Go rewrite of `kanban-flow`.

## Effort

M. Linter + audit trigger + `kanban-flow` `application_id` plumbing
is ~1 day. The `swarm/workflows/*` audit is the variable — depends
on how many direct-mutation paths exist. Estimated 1–2 days total.
