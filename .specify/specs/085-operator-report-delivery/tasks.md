---
description: "Task list for Operator Report Delivery"
---

# Tasks: Operator Report Delivery

**Input**: Design documents from `specs/085-operator-report-delivery/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included — contracts C1–C4 each name a test, and Constitution §1.2
requires spec→test coverage. Tests are written before the implementation they
cover.

**Organization**: Tasks are grouped by user story (US1–US3 from spec.md) so each
story is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel — different files, no dependency on incomplete tasks.
- **[Story]**: US1 / US2 / US3.

---

## Phase 1: Setup

**Purpose**: Confirm a clean baseline before changing kernel/orchestrator code.

- [X] T001 Confirm a green baseline — `go build ./... && go test ./...` in both `go/execution-kernel/` and `go/orchestrator/`; record any pre-existing failures.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The `internal/report` package and the shared message renderer that
both the heartbeat (US1) and the digest (US2) build on.

**⚠️ CRITICAL**: US1 and US2 both depend on this phase.

- [X] T002 Create the `go/execution-kernel/internal/report/` package (package doc) and register a `report` subcommand dispatcher in `go/execution-kernel/cmd/chitin-kernel/main.go` routing `heartbeat`/`digest` to handler stubs in `go/execution-kernel/cmd/chitin-kernel/report.go`.
- [X] T003 Write `go/execution-kernel/internal/report/render_test.go` — the shared renderer: skimmable length-bounded output, link inclusion on detail lines, fixed section/line ordering, degraded-line rendering.
- [X] T004 Implement the shared renderer in `go/execution-kernel/internal/report/render.go` — titled sections of `(text, optional link)` → a skimmable, length-bounded Discord message string (contract C4). Make T003 pass.

**Checkpoint**: `internal/report` exists with a tested renderer — user stories can proceed.

---

## Phase 3: User Story 1 - Heartbeat liveness signal (Priority: P1) 🎯 MVP

**Goal**: A frequent liveness message (gateway / kernel / agents + last
redeploy) is delivered to the operator's Discord on a schedule.

**Independent Test**: Wait one heartbeat interval; a liveness message arrives in
Discord stating per-component status and the last kernel-redeploy outcome.

- [X] T005 [US1] Write `go/execution-kernel/internal/report/heartbeat_test.go` — `ComposeHeartbeat` over healthy / degraded / unreachable-source fixtures; `missed_reports` derivation; the never-healthy-on-an-absent-signal invariant (FR-003).
- [X] T006 [US1] Implement `go/execution-kernel/internal/report/heartbeat.go` — the `Heartbeat` type + `ComposeHeartbeat()` reading `chitin health` (component statuses, kernel staleness, last redeploy) and the delivery log for `missed_reports`. Make T005 pass.
- [X] T007 [US1] Implement `chitin-kernel report heartbeat` in `go/execution-kernel/cmd/chitin-kernel/report.go` — compose → render → print to stdout; exit 0 on a partial report; side-effect-free (contract C1).
- [X] T008 [P] [US1] Write the delivery-script test `go/execution-kernel/cmd/chitin-kernel/operator_report_script_test.go` — a bash harness (mock `chitin-kernel` and `openclaw`) asserting the audit record, exit codes, and the delivery-failure path (contract C2).
- [X] T009 [US1] Implement `swarm/bin/deliver-operator-report.sh` (heartbeat mode) — run `chitin-kernel report heartbeat`, post via `openclaw message send --channel discord`, append one `ReportDeliveryRecord` to `~/.cache/chitin/operator-report.jsonl` (contract C2). Make T008 pass.
- [X] T010 [P] [US1] Add the tracked installer `swarm/bin/install-operator-report.sh` (Constitution §4) — symlink the delivery script to its runtime location.
- [X] T011 [US1] Add the `operator-heartbeat` Temporal Schedule `JobSpec` in `go/orchestrator/schedules/operator_heartbeat.go` (hourly) and register it in `Registry()` in `go/orchestrator/schedules/schedules.go` (contract C3).
- [X] T012 [P] [US1] Test JobSpec registration in `go/orchestrator/schedules/operator_report_test.go` — `operator-heartbeat` is in `Registry()` with the expected name and cron shape.
- [ ] T013 [US1] Verify US1 via `quickstart.md` — `chitin-kernel report heartbeat` composes; `deliver-operator-report.sh heartbeat` posts to Discord; a stale kernel shows `degraded`; the scheduled job fires.

**Checkpoint**: ✅ US1 — heartbeat delivered to Discord, scheduled, side-effect-free.

---

## Phase 4: User Story 2 - Daily telemetry digest (Priority: P2)

**Goal**: A daily + on-demand wrap-up (orchestration, kernel, per-driver
activity, PRs per driver) reaches Discord as a skimmable message with
click-through links into chitin-console.

**Independent Test**: Trigger the digest; a four-section message arrives in
Discord, each detail line linking to a working chitin-console view.

- [X] T014 [P] [US2] Write `go/execution-kernel/internal/report/sources_test.go` — telemetry adapters: gov-decisions grouping by driver, PR-per-driver attribution from `agent/<driver>-*` branches, the source-unavailable path.
- [X] T015 [US2] Implement `go/execution-kernel/internal/report/sources.go` — adapters over `chitin health`, `chitin-kernel decisions recent` (gov-decisions), and `gh pr list`; each returns data-or-unavailable, never an error that aborts the digest (FR-009). Make T014 pass.
- [X] T016 [US2] Write `go/execution-kernel/internal/report/digest_test.go` — `ComposeDigest`: all four sections always present; an unavailable source → section marked unavailable, not dropped; console links on detail lines; bounded rendered length on a large fixture.
- [X] T017 [US2] Implement `go/execution-kernel/internal/report/digest.go` — the `TelemetryDigest` type + `ComposeDigest()` — four sections (orchestration, kernel, drivers, PRs), console deep-links, degradation. Make T016 pass.
- [X] T018 [US2] Add `chitin-kernel report digest` to `go/execution-kernel/cmd/chitin-kernel/report.go` — flags `--board`, `--window-hours`, `--console-base`; compose → render → print (contract C1; Constitution §5 board-aware).
- [X] T019 [US2] Extend `swarm/bin/deliver-operator-report.sh` with the `digest` mode, `--on-demand`, and the rapid-repeat cooldown (contract C2, FR-014).
- [ ] T020 [US2] Wire the on-demand trigger — route an operator Discord command through Clawta to `deliver-operator-report.sh digest --on-demand`.
- [X] T021 [US2] Add the `operator-digest` Temporal Schedule `JobSpec` in `go/orchestrator/schedules/operator_digest.go` (daily, operator TZ), register it in `Registry()`, and extend `operator_report_test.go` to cover it.
- [ ] T022 [P] [US2] Add read-only digest-detail endpoints/routes in `apps/chitin-console-api/src/server.mjs` for any digest link target not already served (per-driver activity, PR rollup).
- [ ] T023 [US2] Verify US2 via `quickstart.md` — the digest composes with four sections and working console links; an on-demand request is delivered within 2 minutes; a degraded source is marked not dropped; the daily job fires.

**Checkpoint**: ✅ US2 — daily + on-demand digest delivered with console links.

---

## Phase 5: User Story 3 - Research reports and Obsidian notes (Priority: P3)

**Goal**: Research reports and Obsidian notes reach the operator through the
same Discord channel.

**Independent Test**: Producing a research report or an Obsidian note delivers a
Discord announcement with a click-through link.

- [ ] T024 [US3] Identify the research-report and Obsidian-note producers in the repo and their completion signal.
- [ ] T025 [US3] Add an `announce` mode to `swarm/bin/deliver-operator-report.sh` — post a "new <artifact>" Discord message with a click-through link, using the same destination and audit log.
- [ ] T026 [US3] Wire research-report and Obsidian-note publication to invoke the `announce` delivery on completion.
- [ ] T027 [US3] Verify US3 via `quickstart.md` — a produced research report / Obsidian note delivers a Discord announcement with a working link.

**Checkpoint**: ✅ US3 — knowledge output delivered through the same channel.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T028 [P] `gofmt`, `go vet`, and lint clean across all changed Go files; `bash -n` on the scripts; `go test ./...` green in both Go modules.
- [ ] T029 [P] Document the operator report-delivery jobs — update the relevant `docs/` and the chitin-console/orchestrator docs.
- [ ] T030 Run `specs/085-operator-report-delivery/quickstart.md` end-to-end as the acceptance pass for all stories.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: after Setup — blocks US1 and US2.
- **US1 (Phase 3)**: after Foundational. The MVP.
- **US2 (Phase 4)**: after Foundational; reuses US1's delivery script and
  installer — sequence US2 after US1.
- **US3 (Phase 5)**: after US1 (reuses the delivery script + destination).
  Independent of US2.
- **Polish (Phase 6)**: after all desired stories.

### User Story Dependencies

- **US1** — depends only on Foundational. Independently shippable.
- **US2** — depends on Foundational; reuses US1's `deliver-operator-report.sh`.
- **US3** — depends on US1's delivery script; independent of US2.

### Within-story ordering

- US1: T005→T006→T007 (compose chain) sequential; T008→T009 (script) sequential;
  T011 before its test T012. T010 is `[P]`.
- US2: T014→T015 then T016→T017 (sources before digest); T018→T019→T020 (CLI →
  script → trigger) sequential; T022 is `[P]`.

### Parallel Opportunities

- `[P]`-marked: T008 (script test) alongside T005–T007 (Go compose);
  T010 (installer) alongside T011–T012 (schedule); T014 (sources test);
  T012 (schedule test); T022 (console endpoints); T028/T029 (polish).

## Implementation Strategy

### MVP

US1 (the heartbeat) is the MVP — it proves the full delivery channel
(compose → openclaw → Discord, scheduled) and is independently valuable.

### Incremental Delivery

1. Setup + Foundational → `internal/report` package + tested renderer.
2. **US1** → heartbeat delivered to Discord on a schedule.
3. **US2** → daily + on-demand telemetry digest with console links.
4. **US3** → research reports + Obsidian notes via the same channel.
5. Polish → lint/test green, docs, quickstart acceptance.

## Notes

- `[P]` = different files, no incomplete dependency.
- The `chitin-kernel report` command is side-effect-free (Constitution §1) — it
  composes and prints; only `deliver-operator-report.sh` posts to Discord.
- spec 083 US4's unified telemetry interface is not yet built — `sources.go`
  reads the sinks directly and is swapped to US4's interface later behind the
  same boundary, with no consumer change.
- Commit after each task or logical group; implement in worktrees (Constitution §2).
