# Dispatch Readiness Checklist

> Operator-facing runbook. Before a ticket can dispatch, **all five checks must pass.**
> Enforced by `kanban-flow ready` (check 3) and `clawta-poller` (all checks at dispatch time).

## 1. Invariants and boundaries block in ticket body

The ticket body must contain an `invariants_and_boundaries` or equivalent structured block that defines what "done" means. Without it, the poller cannot validate completion criteria.

**How to verify**: Open the ticket body and search for `invariants` or a structured `## Invariants` heading.

**If missing**: Groom the ticket — write the invariants before promoting to `ready`.

## 2. Spec-kit entry under board-appropriate spec root

The ticket must be bound to a spec under the board's spec root. The spec root is determined by the board's `spec_source` config:

- `spec_source: "repo"` → specs live in `<workspace_root>/.specify/specs/`
- `spec_source: "workspace_overlay"` → specs live in `~/workspace/.specify/specs/`
- `spec_source: "owned_orgs"` → deprecated; resolves via `owned_orgs` heuristic

**How to verify**: Run `board_resolver spec-dir --board <board>` and confirm the spec directory exists and contains a `<NNN-<slug>/spec.md` matching the ticket.

**If missing**: Create the spec or bind the ticket to an existing one via a forward reference in the ticket body.

## 3. Assignee set to a terminal driver, routing lane, or operator

The ticket must have an `assignee` value. `kanban-flow ready` rejects tickets with NULL assignee.

**Valid values**:
| Value | Meaning |
|---|---|
| `codex` | Dispatch to Codex CLI |
| `copilot` | Dispatch to GitHub Copilot |
| `claude-code` | Dispatch to Claude Code |
| `gemini` | Dispatch to Gemini CLI |
| `clawta` | Routing lane — poller chooses the driver |
| `red` | Operator handles directly |

**How to verify**: `hermes kanban --board <board> show <ticket-id>` — check the `assignee` field.

**If missing**: `kanban-flow ready <id> --assignee <value>` or `hermes kanban --board <board> assign <id> <value>`.

## 4. No unresolved "Blocked until:" in bound spec

If the bound spec contains `Blocked until:` or `depends on:` markers with unresolved conditions, the ticket cannot dispatch.

**How to verify**: Read the bound spec and search for `Blocked until:` or `depends on:`. Confirm each condition is met.

**If blocked**: Resolve the dependency or keep the ticket in `blocked` status until the condition clears.

## 5. Not a tracking epic

Tickets marked `Tracking-epic: true` are containers for child tickets and should not dispatch. `kanban-flow ready` blocks them unless `--force` is passed.

**How to verify**: Check the ticket body for `Tracking-epic: true`.

**If it is a tracking epic**: Don't promote it. Promote the child tickets instead. Use `--force` only if you are the operator and understand the consequences.

---

## Quick check (one-liner)

```bash
# For a ticket t_XXXX, verify readiness:
board_resolver spec-dir --board chitin   # confirm spec root
hermes kanban --board chitin show t_XXXX  # check assignee + body
kanban-flow status t_XXXX                 # check current status
```

## Error messages reference

| Error | Cause | Fix |
|---|---|---|
| `has no assignee — set one before marking ready` | `kanban-flow ready` rejected NULL assignee | `kanban-flow ready <id> --assignee <driver>` |
| `is marked 'Tracking-epic: true'` | Auto-promotion blocked | Don't promote tracking epics; promote children |
| `spec_source` deprecation warning | Board config missing `spec_source` | Add `spec_source` to board config JSON |