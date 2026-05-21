---
description: "Task list for spec 080 — Orchestrator Operational Completion"
---

# Tasks: Orchestrator Operational Completion

**Input**: Design documents from `specs/080-orchestrator-ops-completion/`

**Prerequisites**: plan.md, spec.md

**Tests**: Driver and activity unit tests are included — the spec's success
criteria (SC-001…SC-003) require them.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story the task serves (US1, US2, US3)

## Notes

There is no shared Setup or Foundational phase — the orchestrator's driver,
activity, and workflow infrastructure already exists (spec 070). The three
user stories are independent; each phase ships as its own PR.

---

## Phase 1: User Story 1 — Gemini + Copilot agent drivers (Priority: P1)

**Goal**: The driver registry grows from five to seven; the scheduler can route
to Gemini and Copilot.

**Independent test**: Start the worker host; run driver selection for a
capability each new driver declares — it routes to the new driver.

- [ ] T001 [P] [US1] Create `go/orchestrator/driver/gemini/driver.go` — a Gemini
  `AgentDriver` (id `gemini`): `Ready` probes the `gemini` CLI on PATH,
  `Invoke` shells out in the work unit's worktree, `Card` declares only the
  capabilities Gemini genuinely supports. Mirror `driver/hermes/driver.go`.
- [ ] T002 [P] [US1] Create `go/orchestrator/driver/gemini/driver_test.go` —
  table-driven tests: id, card capabilities in the closed taxonomy, not-ready
  when the binary is absent.
- [ ] T003 [P] [US1] Create `go/orchestrator/driver/copilot/driver.go` — a
  Copilot `AgentDriver` (id `copilot`): same shape; `Card` declares
  `code.review` among its capabilities.
- [ ] T004 [P] [US1] Create `go/orchestrator/driver/copilot/driver_test.go` —
  table-driven tests, as T002.
- [ ] T005 [US1] Register `gemini.New()` and `copilot.New()` in the driver list
  in `go/orchestrator/cmd/chitin-orchestrator/main.go`.
- [ ] T006 [US1] Verify: `go build ./...`, `go test ./driver/...`, and confirm
  the registry reports seven drivers at worker-host startup.

**Checkpoint**: US1 is independently shippable here.

---

## Phase 2: User Story 2 — Discord notification surface (Priority: P2)

**Goal**: The orchestrator posts work events to a Discord channel, write-only;
dispatch stays DAG-driven.

**Independent test**: Run a scheduler tick that completes a work unit — with a
webhook configured a message posts; with none, it degrades to a logged no-op.

- [ ] T007 [US2] Create `go/orchestrator/activities/notify.go` — a
  `DiscordNotify` write-only activity: a typed `NotificationEvent` (kind, work
  unit / run, summary) POSTed to a Discord webhook URL from the environment. A
  missing, malformed, unreachable, or rate-limited webhook degrades to a logged
  no-op; the activity never returns a fatal error.
- [ ] T008 [US2] Create `go/orchestrator/activities/notify_test.go` — cases:
  webhook configured (posts), webhook unset (logged no-op), endpoint
  unreachable (logged, no fault), oversized payload (truncated).
- [ ] T009 [US2] Emit notification events from
  `go/orchestrator/workflows/work_unit.go` — on work-unit settled (done/failed)
  and on PR opened (from the delivery result).
- [ ] T010 [US2] Emit notification events from
  `go/orchestrator/workflows/scheduler.go` — on a node going
  blocked-unroutable and on the run reaching a terminal state (complete or
  stalled).
- [ ] T011 [US2] Register the `DiscordNotify` activity and wire the webhook
  configuration in `go/orchestrator/cmd/chitin-orchestrator/main.go`.
- [ ] T012 [US2] Update
  `apps/chitin-console/src/app/pages/orchestrator-diagram.page.ts` — add a
  human-surfaces lane (Discord notifications, console, GitHub PRs) distinct
  from the dispatch path.
- [ ] T013 [US2] Verify: `go build ./...`, `go test ./activities/... ./workflows/...`,
  `nx build chitin-console`; confirm a run with no webhook configured faults
  nothing.

**Checkpoint**: US2 is independently shippable here.

---

## Phase 3: User Story 3 — chitin-console as a first-class service (Priority: P3)

**Goal**: The console runs as a persistent systemd user service — always-on,
survives reboot.

**Independent test**: Install the unit, start it, curl the console (HTTP 200);
restart it and confirm it returns with no human action.

- [ ] T014 [P] [US3] Create `swarm/systemd/chitin-console.service` — a systemd
  user unit serving the built console bundle on a single declared port,
  `Restart=always`, `WantedBy=default.target`.
- [ ] T015 [P] [US3] Create `swarm/bin/install-chitin-console.sh` — an
  idempotent installer: build the console bundle, install the unit,
  `daemon-reload`, enable + start; with `--verify` and `--remove` modes. Mirror
  `swarm/bin/install-chitin-orchestrator.sh`.
- [ ] T016 [US3] Verify: run the installer, confirm `systemctl --user` reports
  the service active, the console answers HTTP 200, and it returns after a
  service restart.

**Checkpoint**: US3 is independently shippable here.

---

## Dependencies

- US1, US2, US3 are mutually independent — any order, any parallelism across
  stories.
- Within US1: T001–T004 are all `[P]` (distinct files); T005 depends on
  T001+T003; T006 depends on T005.
- Within US2: T007 before T009–T011; T008 `[P]` with T009–T012; T013 last.
- Within US3: T014 and T015 are `[P]`; T016 depends on both.

## Out of scope

Phase 3–5 of the spec-070 rollout (migrating the remaining crons, watchdogs,
and the Icarus bench into workflows) — that is the rollout of spec 070, tracked
there, not here.
