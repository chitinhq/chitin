# Retired Contract: chitin-kernel kanban + board-config subcommands

**Surface**: `chitin-kernel kanban`, `chitin-kernel board-config`
**Status**: RETIRED by spec 087
**FR**: FR-003

## What this contract was

Two CLI subcommands of the `chitin-kernel` binary:

### `chitin-kernel kanban`

Wrapped `go/execution-kernel/internal/kanban/{schema,migrate}.go` to manage the on-disk
SQLite kanban DBs. Typical invocations (reconstructed from the subcommand body and the
swarm-side scripts that called it):

| Invocation | Effect |
|---|---|
| `chitin-kernel kanban migrate` | apply pending schema migrations to a board's DB |
| `chitin-kernel kanban show <ticket-id> --board <slug>` | print one ticket as JSON (called by clawta-pr-reviewer, clawta-blocked-escalator) |
| `chitin-kernel kanban list --board <slug> --lane <name>` | list tickets in a lane (called by board-watchdog) |
| `chitin-kernel kanban assign <ticket> <assignee> --board <slug>` | reassign a ticket (called by clawta-blocked-escalator) |

### `chitin-kernel board-config <slug>`

Wrapped `go/execution-kernel/internal/boardconfig/boardconfig.go` to resolve a board slug
to a DB path + lane definitions, used by swarm-side scripts that needed to know "where
does the chitin board live?" before opening the DB.

## What replaces it

**Nothing inside chitin replaces these subcommands.** The Temporal orchestrator carries
the dispatch semantics; the chitin-console UI carries the operator-visibility
semantics. Specific replacement mappings:

| Old invocation | New mechanism |
|---|---|
| `chitin-kernel kanban migrate` | n/a — no kanban DB to migrate |
| `chitin-kernel kanban show <id>` | the orchestrator's session view in the console UI |
| `chitin-kernel kanban list --lane ready` | the orchestrator's queue view (sessions page) |
| `chitin-kernel kanban assign` | n/a — Temporal workflows don't have manual reassignment via CLI |
| `chitin-kernel board-config <slug>` | n/a — no board concept |

## What in-repo callers do post-retirement

The callers that used to invoke these subcommands are themselves retired or partial-edited:

- `swarm/bin/clawta-pr-reviewer:117` — calls `["hermes", "kanban", "--board", BOARD, "show", ticket_id, "--json"]`. Partial-edit: strip this call or replace with the orchestrator-session lookup.
- `swarm/bin/clawta-blocked-escalator:83` — calls `[HERMES_BIN, "kanban", "--board", BOARD, "assign", ticket.id, RED_ASSIGNEE]`. Partial-edit: strip; ticket assignment is no longer the platform's concern.
- `swarm/bin/swarm-audit` — audits kanban DB state; partial-edit: strip kanban-audit sections.

(Note: these scripts invoke `hermes kanban`, where `hermes` is a separate binary
outside the chitin repo that wraps `chitin-kernel kanban`. The chitin retirement
covers `chitin-kernel kanban`; hermes's wrapper layer is out-of-repo and follows on
its own schedule per the spec Assumptions.)

## What external callers see after the retirement

- An operator typing `chitin-kernel kanban <anything>` gets "unknown subcommand" and exit
  code != 0. This is intended — fail-fast surfaces the breakage to whoever's still
  scripting against the removed CLI.
- A cron entry on the operator's box that runs `chitin-kernel board-config` errors
  cleanly; the operator removes the cron on their own schedule.

## Verification at merge

```
chitin-kernel kanban 2>&1 | grep -qi 'unknown\|invalid\|not.recognized'  && echo PASS || echo FAIL
chitin-kernel board-config 2>&1 | grep -qi 'unknown\|invalid\|not.recognized'  && echo PASS || echo FAIL
grep -rln 'chitin-kernel.kanban\|chitin-kernel.board-config' apps/ go/ libs/ services/ swarm/ | wc -l   # expect 0
```
