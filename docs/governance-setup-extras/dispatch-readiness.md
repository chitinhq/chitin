# Dispatch Readiness — Operator Runbook

> Spec 022 enforcement: a ticket is **dispatch-ready** only when all
> five gates below are satisfied. The `kanban-flow ready` command
> rejects tickets that fail gate 3. The watchdog reports the
> resolution path for gates 1–2.

## Checklist

A ticket must satisfy **all five** checks before the poller can
dispatch it:

1. **Invariants block present.** The ticket body contains an
   `invariants_and_boundaries` (or `Invariants`) section. Tracking
   epics are excluded by convention — they carry `Tracking-epic: true`
   and are never dispatched.

2. **Spec-kit entry exists.** A spec directory matching the ticket
   exists under the board's spec root. The root is resolved by
   `board_resolver.spec_dir_for_board(board)`, which reads the
   `spec_source` field from `~/.hermes/kanban/boards/<board>/config.json`:

   | `spec_source` value | Spec root path |
   |---|---|
   | `repo` | `<workspace_root>/.specify/specs/` |
   | `workspace_overlay` | `~/workspace/.specify/specs/` |
   | `owned_orgs` | Derived from `owned_orgs` set (legacy) |

   If `spec_source` is absent, the default is `workspace_overlay`.

3. **Assignee is set.** Every `ready` ticket must carry an assignee.
   Valid values:

   - **Terminal drivers**: `codex`, `copilot`, `claude-code`, `gemini`
     — the poller dispatches these directly.
   - **Routing lane**: `clawta` — the poller picks the driver.
   - **Operator**: `red` — requires human hands.

   `kanban-flow ready <id>` rejects tickets with a NULL/empty assignee.
   Use `--assignee <lane>` to set one on promotion.

4. **No unresolved `Blocked until:` in the bound spec.** If the spec
   contains `Blocked until:` lines that reference unresolved
   dependencies, the watchdog classifies the ticket as
   `dependency-blocked` and keeps it blocked.

5. **Not a tracking epic.** Tickets marked `Tracking-epic: true` are
   containers for child work and are never promoted to `ready`.

## Diagnostics

- **Watchdog report**: check the `spec root: <path> (source: <tag>)`
  line at the top of each board section. If the path looks wrong,
  check `config.json` → `spec_source`.
- **`kanban-flow ready` rejection**: the error message names the valid
  assignee set. Set one and re-run.
- **`board_resolver spec-source --board <slug>`**: prints the source
  tag (`repo`, `workspace_overlay`, `owned_orgs`, or `default`).

## See also

- Spec 022: `.specify/specs/022-dispatch-readiness-contract/spec.md`
- `swarm/bin/board_resolver.py` — single source of truth for
  board→spec-root resolution.
- `swarm/bin/board-watchdog-bounded.py` — consumes `board_resolver`,
  emits telemetry.