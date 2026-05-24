# Feature Specification: Retire the Kanban Substrate

**Feature Branch**: `087-retire-kanban-substrate`

**Created**: 2026-05-22

**Status**: Draft

**Input**: User description: "Retire kanban end to end. The platform's dispatch flow has
moved to the Temporal orchestrator (specs 070, 081); the kanban substrate is residual."

The chitin platform's dispatch flow now runs through the Temporal orchestrator (spec 070).
Spec 081 retired the board read-model; PR #908 (landed today) retired the `clawta-poller`
that pulled tickets from the kanban DB. The kanban substrate as a whole — the MCP server
that exposed it to agents, the kernel CLI subcommands that maintained its DBs, the
console-API routes that read it, the UI pages that showed it, and the residual swarm-side
scripts that polled it — no longer drives any platform behavior. It remains in the tree as
zombie infrastructure. This feature removes it end to end so the codebase reflects the
operating model.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The chitin codebase no longer carries a kanban surface (Priority: P1)

After this retirement lands, no active source file under `apps/`, `go/`, `libs/`,
`services/`, or `swarm/` imports, calls, exposes, or maintains the kanban substrate. The
MCP server that exposed kanban to agents is gone. The kernel CLI subcommands that
maintained the kanban DB are gone. The console-API routes that read it are gone. The
operator UI pages that displayed it are gone. The residual swarm-side scripts that polled
it are gone. Spec docs and decision history that reference kanban remain untouched —
those are history, not active surfaces.

**Why this priority**: this IS the feature. Without it, the codebase keeps mis-signalling
its operating model — engineers and agents will keep wiring against kanban as if it's
still real, and zombie code accumulates further. The whole point is to make the code
reflect the architecture as it actually runs today.

**Independent Test**: a repo-wide `grep -rli 'kanban\|hermes.*board'` over the active
source roots (`apps/`, `go/`, `libs/`, `services/`, `swarm/`) returns no matches. The
build (Go modules, TypeScript apps/libs, Python packages) is green. The remaining test
suites pass. The platform's actual flow — Temporal orchestrator dispatching agent work
units — continues to function.

**Acceptance Scenarios**:

1. **Given** the retirement has landed, **When** an engineer searches the active source
   tree for kanban or hermes-board references, **Then** they find none.
2. **Given** the retirement has landed, **When** every module's build and tests run,
   **Then** they pass — no orphaned imports of removed packages, no tests for removed code.
3. **Given** the retirement has landed, **When** the platform's normal dispatch flow runs,
   **Then** the Temporal orchestrator continues to schedule and dispatch work as before;
   no platform capability is lost.

---

### User Story 2 - The operator can still see what the platform is doing (Priority: P2)

The kanban UI pages (queue, tickets, threads if tied to kanban) were the operator's
"what's the platform doing right now" view. After they're gone, the operator's
visibility need is met by the orchestrator-side surfaces that already exist: the sessions
page, the orchestrator-diagram page, and the agent-bus-successor surfaces if any. If any
specific view the operator used daily is irreplaceable, that's a separate UI gap to file,
not a reason to keep kanban alive.

**Why this priority**: P2 because P1 is the substantive retirement; this is the
operator-experience side of "did we leave the operator stranded?" If the answer is yes,
the gap must be named and tracked separately — kanban isn't the answer.

**Independent Test**: an operator walking through their normal "what's running, what
landed, what's stuck" workflow uses only the post-retirement UI surfaces and either
finds what they need, or files concrete gap tickets for the specific views that are
irreplaceable. No gap is filled by un-retiring kanban.

**Acceptance Scenarios**:

1. **Given** the operator's daily "view the platform" routine, **When** they perform it
   against the post-retirement console, **Then** they accomplish the visibility tasks
   they performed via the kanban pages, OR each unsatisfied need is filed as a
   non-blocking UI gap ticket against the orchestrator/sessions surfaces.
2. **Given** a new operator joins, **When** they read the operator runbook for
   "view current work," **Then** the runbook points at orchestrator/sessions surfaces,
   not at the deleted kanban pages.

---

### User Story 3 - Operator data is not autonomously destroyed (Priority: P3)

The on-disk SQLite databases (`~/.chitin/kanban/<board>/kanban.db`,
`~/.hermes/kanban/boards/<board>/kanban.db`) are *operator-owned data*. The retirement
removes the *code* that read and wrote them, but does not delete the data itself. The
operator decides when (and whether) to archive or remove those files on their own
schedule.

**Why this priority**: P3 because it's a "don't destroy user data without consent"
discipline, not a feature. It's a constraint the retirement honors, not a deliverable
unto itself.

**Independent Test**: after the retirement lands, the on-disk `kanban.db` files still
exist on operator boxes; nothing in the platform reads or writes them. The retirement's
diff contains no shell command, install hook, or migration step that deletes those files.

**Acceptance Scenarios**:

1. **Given** the retirement has landed on an operator box, **When** the operator inspects
   `~/.chitin/kanban/` and `~/.hermes/kanban/`, **Then** the SQLite files are unchanged
   from before the change.
2. **Given** the operator runs the new chitin version, **When** any platform component
   starts, **Then** no component opens those SQLite files for read or write.

---

### Edge Cases

- A spec doc under `.specify/specs/` references a deleted file or module — these are
  historical, not active; the retirement does not edit spec history.
- A decision doc under `docs/decisions/` references kanban — same: history, untouched.
- An external caller (outside the chitin repo) opens a stdio connection to the deleted
  swarm-kanban-mcp server — the connection fails cleanly (binary not found); the caller
  is responsible for its own update.
- A systemd unit or cron on an operator's box still tries to invoke a removed
  `chitin-kernel kanban migrate` or `chitin-kernel board-config` — the subcommand returns
  cleanly with an "unknown subcommand" error; the operator removes the cron entry on their
  own schedule.
- An operator runs the new chitin version while their `kanban.db` files exist — nothing
  opens them; they sit unread.
- A test file `*_test.go` / `*.test.ts` / `test_*.py` exercises removed code — the test
  file is deleted with the code it exercised; the build and remaining tests stay green.
- README or runbook still references the kanban pages — README/active-runbook docs are
  updated to point at the replacement surfaces; archived runbooks remain as history.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every active source file under `apps/`, `go/`, `libs/`, `services/`,
  `swarm/` that imports, calls, depends on, or otherwise exposes the kanban substrate
  MUST be removed or repointed.
- **FR-002**: The MCP server that exposed kanban tools to agents
  (`services/swarm-kanban-mcp/`) MUST be removed in its entirety.
- **FR-003**: The kernel kanban package, the boardconfig package, and the kanban /
  board-config CLI subcommands MUST be removed; nothing in the kernel binary remains that
  reads or writes a kanban DB.
- **FR-004**: The console-API HTTP routes that read the kanban DB MUST be removed. The
  console-API itself remains (it serves non-kanban surfaces — argus, ELO, gov-decisions,
  etc.).
- **FR-005**: The console-UI pages that displayed kanban state (queue, tickets, and any
  threads page tied to the kanban substrate) MUST be removed. The console-UI app itself
  remains.
- **FR-006**: The residual swarm-side kanban-pull scripts (the ones that survived spec 081
  + PR #908) MUST be removed. The Temporal orchestrator's dispatch path remains intact.
- **FR-007**: Tests that exercise removed code MUST be removed together with the code;
  no orphaned tests for missing modules.
- **FR-008**: The on-disk kanban SQLite files on operator boxes MUST NOT be autonomously
  deleted by this change. The retirement removes code, not user data.
- **FR-009**: The kernel's MCP-call recognition in driver normalizers (the
  `mcp__<server>__<tool>` action-name shape mapping to `gov.ActMCPCall`) MUST be
  unchanged — it is destination-agnostic and applies to any MCP server.
- **FR-010**: Active operator documentation (README, current runbooks) MUST be updated to
  remove references to retired kanban surfaces and point at the orchestrator-side
  replacements where applicable. Historical spec and decision documents MUST remain
  untouched.
- **FR-011**: After the retirement, a repo-wide grep over the active source roots
  (`apps/`, `go/`, `libs/`, `services/`, `swarm/`) for kanban / hermes-board MUST return
  no matches.

### Key Entities

- **Kanban substrate**: the union of in-tree active code, build configuration, and UI
  surfaces that exposed kanban as a dispatch or state-view layer. The plan phase fixes
  the precise file list; the spec defines the boundary (active source dirs above; spec
  history and decision history excluded).
- **Operator kanban data**: the SQLite files on operator boxes (`~/.chitin/kanban/`,
  `~/.hermes/kanban/`). Frozen by this change; not deleted.
- **Visibility replacement**: the post-retirement UI/CLI surfaces (the orchestrator
  pages, sessions, the agent-bus-successor surfaces if applicable) the operator uses
  in place of the kanban pages.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Zero kanban / hermes-board references remain in active source — a repo-wide
  grep over `apps/`, `go/`, `libs/`, `services/`, `swarm/` returns no hits.
- **SC-002**: Every module's build is green (Go, TypeScript, Python). Every remaining
  test suite passes.
- **SC-003**: The Temporal orchestrator's dispatch flow continues to schedule and run
  agent work units exactly as before; no platform capability is lost.
- **SC-004**: An operator walking through their daily "view the platform" routine
  completes it via the post-retirement surfaces, OR each unsatisfied view is filed as a
  separate UI-gap ticket. No gap is filled by un-retiring kanban.
- **SC-005**: Zero on-disk kanban SQLite files are deleted by the retirement — the
  operator's data is preserved on the box.
- **SC-006**: The chitin kernel's existing recognition of MCP-shaped tool names
  (`mcp__server__tool`) is unchanged and continues to govern MCP calls from any agent
  to any MCP server.

## Assumptions

- **Temporal is the dispatch substrate.** Spec 070 established the orchestrator; spec
  081 retired the board read-model; PR #908 retired `clawta-poller`. This retirement
  completes the direction those changes set.
- **Operator visibility is met by the orchestrator-side UI.** The sessions and
  orchestrator-diagram pages cover the post-retirement "what's running" view. If any
  specific kanban view turns out to be irreplaceable in daily operator use, that is a
  separate (non-blocking) UI gap — not a reason to keep the substrate alive.
- **Kanban data is operator-owned.** The on-disk SQLite files are not autonomously
  deleted. Operators archive or remove them on their own schedule.
- **Protocol-agnostic kernel governance is preserved.** The kernel's mapping of
  `mcp__server__tool` action names to `gov.ActMCPCall` applies to any MCP server and is
  not coupled to kanban; it stays.
- **External MCP callers are out of scope.** If anything outside the chitin repo connects
  to the deleted swarm-kanban-mcp via stdio, that caller is responsible for its own
  update; the chitin repo's job is to delete the in-tree code.
- **Decommissioned siblings stay decommissioned.** `services/agent-bus/` and
  `services/mini-mcp/` were retired by spec 069; this retirement does not revisit them.
- **Scope — in**: active source files under `apps/`, `go/`, `libs/`, `services/`,
  `swarm/`; build configuration that lists removed modules (Nx project graph, `go.mod`
  references); active operator documentation (README, current runbooks).
- **Scope — out**: spec documents under `.specify/specs/` (history); decision documents
  under `docs/decisions/` (history); operator-local data (`~/.chitin/kanban/`,
  `~/.hermes/kanban/`); external MCP callers; the kernel's general MCP-call action
  vocabulary.
- **Dependencies**: this retirement assumes the Temporal orchestrator (spec 070), board
  retirement (spec 081), and clawta-poller retirement (PR #908) — all landed — are
  sufficient to absorb the kanban substrate's responsibilities. If a gap is discovered
  during planning, it surfaces as a separate ticket against the orchestrator/UI, not as a
  reason to roll back this retirement.
