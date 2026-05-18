# 022 — Dispatch readiness contract

> Operator call 2026-05-17 after spending 15 minutes chasing three
> undocumented gates that all had to be satisfied before a single
> readybench MVP ticket could dispatch:
>
> > *"yet another example of why we need specs for everything..."*

## Ticket refs

- Workspace chitin task (file separately after merge).
- Concrete repro: t_5f18463a (Portal Overview tab) bounced through
  promote-demote loops twice this session before all three gates
  were identified + cleared.

## File-system scope

- `swarm/bin/clawta-poller` (extract + document the readiness gate)
- `swarm/bin/board-watchdog-bounded.py` (delete the hardcoded
  BOARDS dict; route through `board_resolver.spec_dir_for_board`)
- `swarm/bin/board_resolver.py` (no functional change; this is the
  single source of truth)
- `swarm/tests/test_dispatch_readiness_contract.py` (new)
- `docs/governance-setup-extras/dispatch-readiness.md` (operator-
  facing runbook)
- `.specify/specs/022-dispatch-readiness-contract/**`

## Goal

A single, written, code-enforced contract for "when is a ticket
ready to dispatch" — replacing the current set of code-comment-and-
default rules that only surface when someone hits them.

## The three gates we hit today (the audit)

### Gate 1: `owned_orgs` set determines whether the poller looks in
the workspace overlay or the target repo for spec-kit entries.

- **Where it lives now**: `board_resolver.owned_orgs()` defaults to
  `{"chitinhq", "red"}`. Boards under other orgs (e.g.
  `wjcmurphy/bench-devs-platform`) silently fall through to the
  workspace-overlay path — even when the operator is the de facto
  owner.
- **How it surfaced**: poller reported `missing spec-kit entry`
  for tickets whose specs were committed to the target repo. No
  log explained the path-resolution decision.
- **The contract this spec writes**: every board MUST declare its
  spec resolution explicitly. Default-set fallback is removed; the
  board config in `~/.hermes/kanban/boards/<board>/config.json`
  MUST carry one of:
  - `"spec_source": "repo"` (specs live in `<workspace_root>/.specify/specs/`)
  - `"spec_source": "workspace_overlay"` (specs live in
    `~/workspace/.specify/specs/`)
  - `"spec_source": "owned_orgs"` (legacy — keep `owned_orgs` and
    let `board_resolver` derive; deprecated in favor of explicit)

### Gate 2: Watchdog `BOARDS` dict hardcodes spec_root, drifting from `board_resolver`.

- **Where it lives now**: `board-watchdog-bounded.py:37-46`
  hardcodes `BOARDS["readybench"]["spec_root"] = WORKSPACE_ROOT /
  ".specify" / "specs"`. Today (before PR #743 hot-fixed it) this
  diverged from the poller's `board_resolver.spec_dir_for_board`
  resolution; result: poller said spec is found, watchdog said
  spec is missing, ticket bounced.
- **The contract this spec writes**: watchdog MUST consume
  `board_resolver.spec_dir_for_board(board)` like the poller does.
  The hardcoded `BOARDS` dict goes away. PR #743 was the surgical
  patch; this spec lands the structural one.

### Gate 3: Poller refuses to dispatch tickets without `assignee` set to a terminal driver or routing lane.

- **Where it lives now**: poller emits "missing assignee — grooming
  step must route to a terminal lane (codex/copilot/claude-code/
  gemini) or the routing lane (clawta) before clawta-poller can
  dispatch" and demotes. The "grooming step" is unnamed; the
  operator's repro of "set to ready" doesn't trigger it.
- **How it surfaced**: every time I `UPDATE tasks SET status='ready',
  assignee=NULL` the next poller run demoted. Took two rounds to
  see the error in the direct-run log.
- **The contract this spec writes**: every ticket that promotes to
  `ready` MUST carry an `assignee`. Three valid values:
  - a terminal driver: `codex`, `copilot`, `claude-code`, `gemini`
  - the routing lane: `clawta` (poller will choose the driver)
  - the operator: `red` (means "needs operator hands")
  - NULL is rejected at the `kanban-flow ready` boundary, not at
    poller time. Promotion fails loud; no silent demote.

## Requirements

- **R1**: Single source of truth for board→spec-root resolution.
  `board_resolver.spec_dir_for_board(board)` is called by the
  poller AND the watchdog AND any future consumer. No code outside
  `board_resolver.py` constructs a spec_root path.
- **R2**: Board config schema gains an explicit `spec_source`
  field. `board_resolver` reads it. The legacy `owned_orgs`
  default-set is removed (still readable from config for explicit
  opt-in; no implicit fallback).
- **R3**: `kanban-flow ready <id>` rejects tickets with no
  assignee. Error message names the valid set + the routing-lane
  alternative.
- **R4**: A new operator-facing runbook (`docs/governance-setup-
  extras/dispatch-readiness.md`) documents the full checklist a
  ticket must satisfy before dispatch:
  1. Has `invariants_and_boundaries` block in body
  2. Has spec-kit entry under board-appropriate spec root
  3. Has `assignee` set (terminal driver or routing lane)
  4. Has no unresolved `Blocked until:` in the bound spec
  5. Is not a tracking-epic
- **R5**: The watchdog's `### readybench board` section in its
  Discord post MUST include resolution-decision telemetry: "spec
  root: <path> (source: repo|workspace_overlay|owned_orgs)" so
  future drift is visible without source-diving.

## Test coverage

### Why integration + static-analysis (not browser-e2e) for this spec

The end-to-end surface here is **the poller + watchdog code path
on a real kanban DB**. There's no browser or HTTP boundary. The
authentic tests are:
1. Static-analysis against the lobster/Python source (matching the
   existing test_dispatch_base_freshness_regression.py pattern)
2. Integration: seed a fake board config + tickets in a temp DB,
   run the poller against it, assert the right tickets dispatch/
   demote with the right reason strings.

A browser-e2e would be ceremonial — there's no user-observable
boundary to drive.

| Spec 022 AC | Test case (in `swarm/tests/test_dispatch_readiness_contract.py`) | What breaks if removed |
|-------------|------------------------------------------------------------------|------------------------|
| R1 single source of truth | `test_watchdog_consumes_spec_dir_for_board` (greps `board-watchdog-bounded.py` for the import + call; asserts no hardcoded `WORKSPACE_ROOT / ".specify" / "specs"` literal remains) | Drift recurs |
| R2 explicit spec_source | `test_board_config_requires_spec_source` (loads each board config, asserts `spec_source` key present) | Implicit defaults sneak back in |
| R3 assignee gate at promotion | `test_ready_rejects_null_assignee` (integration: `kanban-flow ready t_xxx` with NULL assignee returns nonzero + named error) | Tickets silently demote at poller time |
| R3 valid-set list | `test_ready_accepts_valid_assignees` (parametrized over codex, copilot, claude-code, gemini, clawta, red) | Valid sets get rejected |
| R4 runbook exists | `test_readiness_runbook_exists_with_all_5_checks` (greps the markdown file for each numbered check) | Operator-facing doc rots |
| R5 watchdog telemetry | `test_watchdog_post_includes_spec_root_decision` (run watchdog, assert Discord-bound output contains "spec root:" + "source:" lines) | Future drift goes unflagged |

## Invariants

- **inv-1: configuration is explicit.** No defaults that hide
  resolution decisions. The cost of "the operator must declare
  spec_source per board" is one line of JSON; the benefit is
  every future debugging session starts from a written contract.
- **inv-2: a single function owns resolution.** `spec_dir_for_board`
  in `board_resolver`. Every consumer calls it. Any other path
  construction is a bug.
- **inv-3: readiness fails fast at the boundary.** `kanban-flow
  ready` is where validation lives. By the time the poller sees
  the ticket, it's known-ready or known-not.

## Out of scope

- Migrating the kanban DB out of `~/.hermes/` (separately tracked
  as Plan 4, operator PR #682).
- Splitting `board-watchdog-bounded.py` into a library + cron entry
  (cleanup, but orthogonal).
- Adding new gates (e.g. CI-passed-on-base, no-conflicts-with-base).
  Each future gate gets its own spec.

## Why this spec exists

Today, in 15 minutes of dispatching one readybench MVP ticket, I
hit three undocumented gates back-to-back. None had a spec. Each
cost a round-trip:

1. owned_orgs default-set excluded `wjcmurphy` → unset → cost: 1 round-trip
2. watchdog hardcoded BOARDS dict drifted from poller → cost: 1 round-trip
3. assignee-required-before-dispatch → silently demoted → cost: 1 round-trip

The operator's response was direct: *"yet another example of why
we need specs for everything."* This spec is the answer. Every
governance rule that lives only in code is a tax on every future
engineer. Pay it down here.
