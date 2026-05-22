---
description: "Task list for Driver Governance & Telemetry Integrity"
---

# Tasks: Driver Governance & Telemetry Integrity

**Input**: Design documents from `specs/083-driver-governance-telemetry/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included where the plan calls for them (`go test` for kernel logic;
the quickstart probe as the acceptance test). Tests are written before the
implementation they cover.

**Organization**: Tasks are grouped by user story (US1–US4 from spec.md) so each
story is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel — different files, no dependency on incomplete tasks.
- **[Story]**: US1 / US2 / US3 / US4.
- `[X]` checkbox = already completed this session.

---

## Phase 1: Setup

**Purpose**: Confirm a clean baseline before changing kernel/orchestrator code.

- [ ] T001 Confirm a green baseline — `go build ./... && go test ./...` in both `go/execution-kernel/` and `go/orchestrator/`; record any pre-existing failures.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The decision-record contract every story reads/writes.

**⚠️ CRITICAL**: US3 (codex routing) and US4 (unified reader) depend on this.

- [ ] T002 Ratify the Governance Decision attribution contract (C1 / INV-001) — confirm `go/execution-kernel/internal/gov/decision.go` carries a non-empty `driver` or `agent` on every decision, and add a unit test enforcing it in `go/execution-kernel/internal/gov/decision_test.go`.

**Checkpoint**: Decision contract fixed — user stories can proceed.

---

## Phase 3: User Story 1 - Restore the regressed hermes/clawta telemetry (Priority: P1) 🎯 MVP

**Goal**: Hermes and Clawta emit attributed governance decisions again.

**Independent Test**: Trigger a Hermes/Clawta unit of work; an attributed
`gov-decision` appears in the central log within the run.

**Status**: ✅ Completed 2026-05-22 (this session).

- [X] T003 [US1] Redeploy `chitin-kernel` from post-#861 `main` via `scripts/install-kernel.sh` (smoke-tested, installed).
- [X] T004 [US1] Restart the Hermes gateway (`hermes gateway restart`) to load the #861 Hermes plugin.
- [X] T005 [US1] Restart the OpenClaw gateway (`systemctl --user restart openclaw-gateway.service`) to load the #861 `chitin-bridge.mjs`.
- [X] T006 [US1] Verify — clawta emits attributed `gov-decisions` (121 rows post-restart); hermes verified by live `hermes chat` probe (`shell.exec`, `allowed`).

**Checkpoint**: ✅ US1 done — hermes/clawta governance telemetry restored and verified.

---

## Phase 4: User Story 2 - Kernel-fix delivery cannot strand (Priority: P2)

**Goal**: A merged kernel fix reliably reaches the running kernel; staleness and
redeploy failures are surfaced.

**Independent Test**: Merge a trivial kernel change; the running kernel reflects
it within the redeploy cadence, with no manual step.

- [ ] T007 [US2] Replace `git pull --ff-only origin main` in `scripts/install-kernel.sh` with `git fetch origin main` + `git merge --ff-only origin/main`, preserving the autostash behaviour via an explicit stash/pop around the merge (research Decision 5).
- [ ] T008 [P] [US2] Implement kernel-staleness detection in `go/execution-kernel/internal/health/` — compare the running binary's build revision against the merged kernel source HEAD.
- [ ] T009 [US2] Surface kernel staleness through the `chitin health` command in `go/execution-kernel/cmd/chitin-kernel/` — report `stale` vs `current` with the revision delta.
- [ ] T010 [US2] Make a redeploy failure operator-visible — emit an alert/health signal beyond the line in `~/.cache/chitin/install-kernel.jsonl` (FR-010).
- [ ] T011 [P] [US2] Unit-test the staleness detector in `go/execution-kernel/internal/health/health_test.go`.
- [ ] T012 [US2] Verify — merge a trivial kernel-relevant change; confirm the running kernel reflects it within one redeploy cadence and `chitin health` reports `current`.

**Checkpoint**: Kernel fixes can no longer strand silently.

---

## Phase 5: User Story 3 - Every dispatched driver is provably governed (Priority: P3)

**Goal**: copilot, codex, gemini, and orchestrator-dispatched worktrees are all
provably governed (or correctly reported `unverified`).

**Independent Test**: Per driver, run the quickstart probe; confirm an attributed
`gov-decision` (or, for an unrunnable CLI, an `unverified` report).

- [X] T013 [US3] ✅ Resolved the copilot CLI↔SDK `timestamp` mismatch — forked `copilot-sdk` into `go/execution-kernel/third_party/copilot-sdk-go/` via a `replace` directive; added `flexInt64` (a string-or-number `timestamp` decoder, `flexint.go`) and applied it to all 7 `timestamp` fields in `types.go`.
- [X] T014 [P] [US3] ✅ Verified `drive_copilot.go` exit codes — it already returns 1/2 on failure correctly; the earlier "exit 0" observation was a `$?`-after-pipe test artifact, not a real bug. No change needed.
- [X] T015 [US3] ✅ Rebuilt `chitin-kernel` with the fix, installed to `~/.local/bin/chitin-kernel` (rollback at `~/.local/bin/chitin-kernel.prev-precopilot`), smoke-tested; `chitin-kernel drive copilot` ran a governed tool call and emitted an `agent:copilot-cli` gov-decision.
- [X] T016 [US3] ✅ Rewired `go/orchestrator/driver/copilot/driver.go` to invoke `chitin-kernel drive copilot --cwd <worktree> <prompt>` and corrected the false "kernel governs regardless" comment; orchestrator builds and the copilot driver tests pass.
- [ ] T017 [P] [US3] Route codex governance decisions to the central `gov-decisions` sink (not only `codex-events-<session>.jsonl`) in the codex hook path / `go/execution-kernel/internal/gov/` (research Decision 2, FR-005).
- [ ] T018 [P] [US3] Implement the `unverified` driver-governance state in `go/execution-kernel/internal/gov/` — a driver whose CLI cannot run is reported `unverified`, never `governed` (FR-014, research Decision 3).
- [ ] T019 [US3] Ensure orchestrator work-unit worktrees are governed — confirm `CreateWorktree` yields a worktree the global hooks reach, or provision governance into it; a work unit with no resolvable governance path is flagged, not run silently (FR-008).
- [ ] T020 [US3] Verify via the quickstart probe — copilot produces an attributed `gov-decision`; codex decisions appear in the central sink; gemini reports `unverified` (gemini descoped for live auth — Antigravity migration pending).

**Checkpoint**: All runnable drivers proven governed; copilot closed.

---

## Phase 6: User Story 4 - Trustworthy, unified observability (Priority: P4)

**Goal**: One queryable interface over all governance telemetry; `chitin doctor`
validates against a live probe.

**Independent Test**: One query returns decisions for every driver; `chitin
doctor`'s verdict matches a real probe for every driver.

- [ ] T021 [US4] Implement the unified, read-only telemetry query interface over `gov-decisions-*.jsonl`, `codex-events-*.jsonl`, and `events-openclaw-clawta-*.jsonl` in `go/execution-kernel/internal/telemetry/` (C3 — read-only, no chain writes).
- [ ] T022 [US4] Rebuild `chitin doctor` in `go/execution-kernel/cmd/chitin-kernel/` to validate each driver by a live probe that produces a `gov-decision` — not a hook-file marker check (FR-012, research Decision 7).
- [ ] T023 [P] [US4] Make `chitin doctor` credit a driver governed by a global (non-project) hook as passing (FR-013).
- [ ] T024 [P] [US4] Unit + integration tests for the unified query interface and the rebuilt `chitin doctor` in `go/execution-kernel/internal/telemetry/*_test.go` and `go/execution-kernel/cmd/chitin-kernel/*_test.go`.
- [ ] T025 [US4] Verify — one query returns all drivers' decisions; `chitin doctor` verdict matches a real probe for every driver, zero false positives/negatives (SC-005).

**Checkpoint**: Observability is unified and trustworthy.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T026 [P] Update `docs/2026-05-21-orchestrator-driver-telemetry-audit.md` to the final post-implementation state.
- [ ] T027 Run `specs/083-driver-governance-telemetry/quickstart.md` end-to-end as the acceptance pass for all stories.
- [ ] T028 [P] `gofmt`, `go vet`, and lint clean across all changed files; `go test ./...` green in both Go modules.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (P1)**: no dependencies.
- **Foundational (P2)**: after Setup — blocks US3 and US4.
- **US1 (P3)**: ✅ already complete (independent of P2).
- **US2 (P4)**: after Setup — independent of US3/US4.
- **US3 (P5)**: after Foundational (T002).
- **US4 (P6)**: after Foundational (T002); US4's doctor (T022) is most useful after US3's driver fixes but is independently testable.
- **Polish (P7)**: after all desired stories.

### User Story Dependencies

- **US1** — done.
- **US2** — independent; can start immediately after Setup.
- **US3** — depends on Foundational; T013→T014→T015→T016 are sequential (copilot chain); T017, T018 are `[P]` (distinct paths).
- **US4** — depends on Foundational; T021–T023 largely parallel.

### Parallel Opportunities

- US2 and US3 and US4 can proceed in parallel once Foundational (T002) is done.
- Within US3: T017 (codex) and T018 (gemini state) run parallel to the copilot chain (T013–T016).
- `[P]`-marked tasks across T008/T011, T014, T017/T018, T023/T024, T026/T028 are parallelizable.

## Parallel Example: User Story 3

```bash
# After T002, the copilot chain and the independent driver fixes run in parallel:
Task: "T013 — resolve copilot CLI/SDK timestamp mismatch"
Task: "T017 [P] — route codex decisions to the central sink"
Task: "T018 [P] — implement the unverified driver-governance state"
```

## Implementation Strategy

### MVP

US1 (the regression) is already the delivered MVP — hermes/clawta restored.

### Incremental Delivery

1. Setup + Foundational → baseline + decision contract.
2. **US2** → kernel fixes can't strand again (protects US1's fix).
3. **US3** → copilot closed; codex centralised; gemini correctly `unverified` → all runnable drivers proven.
4. **US4** → unified observability + trustworthy `chitin doctor`.
5. Polish → docs, quickstart acceptance, lint/test green.

### Recommended order

US2 first (it protects the US1 fix already shipped), then US3 (the visible
copilot gap), then US4 (makes the whole thing durable and visible).

## Notes

- `[P]` = different files, no incomplete dependency.
- US1 tasks are marked `[X]` — completed this session (kernel redeploy + gateway restarts).
- Gemini live-auth verification is descoped (Antigravity migration pending); T018/T020 cover only the correct `unverified` reporting.
- Commit after each task or logical group; implement in worktrees (constitution §2).
