# Implementation Plan: Production Execution-Governance Kernel

Status: plan - awaiting operator review before implementation.

Date: 2026-05-09

Spec: `docs/superpowers/specs/2026-05-08-production-execution-governance-kernel-design.md`

## Overview

This plan turns the production kernel spec into small, verifiable slices. The work strengthens Chitin's asymmetric core: cross-driver canonical action normalization, deterministic policy, pre-side-effect gate decisions, tamper-evident local records, replay/index rebuilds, bounds enforcement, persistent agent severity, pure-Go signals, and optional post-commit telemetry.

It deliberately avoids orchestration, approvals, dashboards, model routing, SaaS dependencies, and LLM-in-the-loop safety.

## Architecture Decisions

- **Go kernel remains authoritative.** Any implementation task that needs enforcement, state mutation, chain writes, or policy evaluation lands under `go/execution-kernel/`.
- **Driver work starts with conformance fixtures.** Before changing behavior, each supported driver gets fixtures proving what Chitin claims it can intercept and normalize.
- **Decision durability comes before projection.** Decision records and chain/index verification are tightened before OTEL or read-side polish.
- **Bounds and severity stay separate from approvals.** Repeated denial and lockdown are deterministic kernel state, not an operator prompt.
- **Docs are acceptance artifacts.** README, roadmap, architecture, and driver conformance docs must match tested behavior, not aspirational behavior.

## Dependency Graph

```text
Driver conformance inventory
  -> canonical action fixture coverage
    -> policy/gate fail-closed hardening
      -> decision durability and chain linkage
        -> replay/index verification
          -> bounds and severity production checks
            -> signal and OTEL invariant checks
              -> documentation alignment
```

## Task List

### Phase 1: Conformance Baseline

## Task 1: Lock Driver Conformance Matrix

**Description:** Update `docs/driver-conformance.md` so every supported driver has an explicit integration point, real-time gating status, fixture location, known coverage gaps, and production-support claim.

**Acceptance criteria:**
- [x] Claude Code, Codex, Gemini, Copilot, OpenClaw, and Hermes are listed.
- [x] Each row states whether the integration is pre-side-effect and real-time.
- [x] Unknown or partial coverage is called out explicitly instead of implied.

**Verification:**
- [x] Review doc links against `docs/architecture.md` and `docs/roadmap.md`.
- [x] Run `rg "Claude Code|Codex|Gemini|Copilot|OpenClaw|Hermes" docs/driver-conformance.md`.

**Dependencies:** None.

**Files likely touched:**
- `docs/driver-conformance.md`

**Estimated scope:** S: 1 file.

## Task 2: Add Driver Normalization Fixtures

**Description:** Add or complete representative fixture tests for each supported driver so semantically equivalent tool calls normalize to the same canonical `gov.Action` where appropriate.

**Acceptance criteria:**
- [x] Each live driver has known-tool and unknown-tool fixture coverage.
- [x] Shell execution payloads across supported drivers normalize consistently.
- [x] Unknown actions normalize to `ActUnknown` and are covered by tests.

**Verification:**
- [x] `(cd go/execution-kernel && go test ./internal/driver/... ./internal/gov -run Normalize)`

**Dependencies:** Task 1.

**Files likely touched:**
- `go/execution-kernel/internal/driver/*/normalize_test.go`
- `go/execution-kernel/internal/gov/normalize_test.go`

**Estimated scope:** M: 3-5 files per driver batch. Split by driver if needed.

### Checkpoint: Interception Claims

- [x] Driver conformance docs match tested fixture coverage.
- [x] No README or roadmap driver claim exceeds the conformance matrix.

### Phase 2: Gate and Policy Hardening

## Task 3: Fail-Closed Policy Load Paths

**Description:** Audit `chitin-kernel gate evaluate` and hook dispatch paths to ensure required policy load, malformed policy, stale effects, and unsupported predicates fail closed in enforcement paths.

**Acceptance criteria:**
- [x] Malformed policy denies rather than silently allowing.
- [x] Culled effects such as approval/escalate fail parse.
- [x] Monitor mode remains explicit and test-covered.

**Verification:**
- [x] `(cd go/execution-kernel && go test ./internal/gov -run 'Policy|Load|Gate')`

**Dependencies:** Task 2.

**Files likely touched:**
- `go/execution-kernel/internal/gov/policy.go`
- `go/execution-kernel/internal/gov/inherit.go`
- `go/execution-kernel/internal/gov/policy_test.go`
- `go/execution-kernel/internal/gov/inherit_test.go`

**Estimated scope:** M: 4 files.

## Task 4: Normalize Gate Exit Codes and JSON Shape

**Description:** Make `gate evaluate`, hook dispatch, and simulate paths return stable machine-readable decision JSON and documented exit codes for allow, deny/guide/lockdown, and internal error.

**Acceptance criteria:**
- [x] Allow exits 0 with decision JSON.
- [x] Deny, guide, and lockdown exits are stable and documented.
- [x] Internal errors are distinguishable from policy denials.

**Verification:**
- [x] `(cd go/execution-kernel && go test ./cmd/chitin-kernel ./internal/hookdispatch ./internal/gov)`

**Dependencies:** Task 3.

**Files likely touched:**
- `go/execution-kernel/cmd/chitin-kernel/*`
- `go/execution-kernel/internal/hookdispatch/dispatch.go`
- `go/execution-kernel/internal/hookdispatch/dispatch_test.go`

**Estimated scope:** M: 3-5 files.

### Checkpoint: Gate Contract

- [x] Gate behavior is deterministic under malformed policy, unknown actions, and normal allow/deny paths.
- [x] CLI and hook paths agree on decision semantics.

### Phase 3: Decision Chain and Replay

## Task 5: Decision Record Durability Audit

**Description:** Ensure every gate decision has a durable local record before any non-authoritative projection, and that decision logging failures fail closed where they affect governance.

**Acceptance criteria:**
- [x] All `Gate.Evaluate` paths record or return an explicit failure.
- [x] Lockdown short-circuit decisions are recorded.
- [x] Decision records include action type, target, agent, rule id, decision, reason, timestamp, and stable identifiers.

**Verification:**
- [x] `(cd go/execution-kernel && go test ./internal/gov -run 'Decision|Gate|Lockdown')`

**Dependencies:** Task 4.

**Files likely touched:**
- `go/execution-kernel/internal/gov/gate.go`
- `go/execution-kernel/internal/gov/decision.go`
- `go/execution-kernel/internal/gov/decision_test.go`
- `go/execution-kernel/internal/gov/gate_test.go`

**Estimated scope:** M: 4 files.

## Task 6: Chain Verification and Index Rebuild Smoke

**Description:** Add a kernel-level verification path or test coverage proving hash continuity, tamper detection, and rebuildability of the derived SQLite index from local canonical records.

**Acceptance criteria:**
- [ ] Modified, missing, or reordered records are detected in tests.
- [ ] Derived index rebuild from JSONL is covered.
- [ ] Tests distinguish canonical record failure from derived-cache failure.

**Verification:**
- [ ] `(cd go/execution-kernel && go test ./internal/chain ./internal/replay ./internal/emit)`

**Dependencies:** Task 5.

**Files likely touched:**
- `go/execution-kernel/internal/chain/*`
- `go/execution-kernel/internal/replay/*`
- `go/execution-kernel/internal/emit/*_test.go`

**Estimated scope:** M: 3-5 files.

### Checkpoint: Flight Recorder

- [ ] Local decision and event records are durable and verifiable.
- [ ] Derived indexes are rebuildable and not treated as authoritative.

### Phase 4: Bounds, Severity, and Signals

## Task 7: Bounds Enforcement Production Cases

**Description:** Expand bounds tests for push-shaped and large-change actions, including protected paths, over-limit diffs, normal diffs, and unable-to-compute diffs.

**Acceptance criteria:**
- [ ] Bounds failures deny regardless of earlier allowed edits.
- [ ] Unable-to-compute bounds fails closed for enforcement paths.
- [ ] Protected-path behavior is covered by policy fixtures.

**Verification:**
- [ ] `(cd go/execution-kernel && go test ./internal/gov -run Bounds)`

**Dependencies:** Task 5.

**Files likely touched:**
- `go/execution-kernel/internal/gov/bounds.go`
- `go/execution-kernel/internal/gov/bounds_test.go`
- `go/execution-kernel/internal/gov/testdata/*`

**Estimated scope:** S-M: 2-4 files.

## Task 8: Agent Severity and Lockdown Persistence

**Description:** Tighten persistent agent state tests so repeated denied fingerprints raise severity, enter lockdown, survive process boundaries, and reset only through explicit operator commands.

**Acceptance criteria:**
- [ ] Counters key on stable action fingerprints.
- [ ] Lockdown survives a new `Counter` or gate instance.
- [ ] Reset and status are explicit and test-covered.

**Verification:**
- [ ] `(cd go/execution-kernel && go test ./internal/gov -run 'Escalation|Lockdown|Status|Reset')`

**Dependencies:** Task 5.

**Files likely touched:**
- `go/execution-kernel/internal/gov/escalation.go`
- `go/execution-kernel/internal/gov/escalation_test.go`
- `go/execution-kernel/cmd/chitin-kernel/*`

**Estimated scope:** M: 3-5 files.

## Task 9: Pure-Go Signal Invariants

**Description:** Verify blast-radius, floundering, and drift signals remain deterministic chain-derived annotations and cannot spawn agents, call models, prompt operators, or schedule workflows.

**Acceptance criteria:**
- [ ] Signal tests prove deterministic output for fixed chain tails.
- [ ] No router signal path shells out to LLMs or delegates work.
- [ ] Advisory fields are recorded without changing kernel authority.

**Verification:**
- [ ] `(cd go/execution-kernel && go test ./internal/router)`
- [ ] `rg "delegate|approval|claude|codex|gemini|openai|anthropic" go/execution-kernel/internal/router`

**Dependencies:** Task 6.

**Files likely touched:**
- `go/execution-kernel/internal/router/*_test.go`
- `go/execution-kernel/internal/router/*.go`

**Estimated scope:** S-M: 2-5 files.

### Checkpoint: Governance Kernel

- [ ] Bounds, severity, lockdown, and signals are deterministic kernel features.
- [ ] No approvals, orchestration, or LLM calls have entered the kernel path.

### Phase 5: Projection and Documentation Alignment

## Task 10: OTEL Post-Commit Invariant

**Description:** Reconfirm and extend OTEL tests so projection is optional, derivable from chain records, and unable to fail an already-committed canonical write.

**Acceptance criteria:**
- [ ] Disabled OTEL does nothing.
- [ ] Failed OTEL endpoint does not corrupt or prevent the local record.
- [ ] No policy or gate logic reads OTEL data.

**Verification:**
- [ ] `(cd go/execution-kernel && go test ./internal/emit -run OTEL)`
- [ ] `rg "OTEL|otel" go/execution-kernel/internal/gov go/execution-kernel/internal/driver` returns no authority-path dependency.

**Dependencies:** Task 6.

**Files likely touched:**
- `go/execution-kernel/internal/emit/otel.go`
- `go/execution-kernel/internal/emit/otel_test.go`

**Estimated scope:** S: 1-2 files.

## Task 11: Production Claim Documentation Pass

**Description:** Align README, roadmap, architecture, event model, governance setup, and driver conformance docs with the tested production behavior.

**Acceptance criteria:**
- [ ] Driver support claims match fixtures and conformance matrix.
- [ ] Production-readiness language maps to passing tests.
- [ ] Boundary exclusions remain visible: no orchestration, approvals, SaaS, model routing, or LLM-in-loop safety.

**Verification:**
- [ ] `rg "approval|orchestr|scheduler|swarm|dashboard|SaaS|LLM" README.md docs`
- [ ] Manual review of README, roadmap, architecture, event model, and driver conformance docs.

**Dependencies:** Tasks 1-10.

**Files likely touched:**
- `README.md`
- `docs/roadmap.md`
- `docs/architecture.md`
- `docs/event-model.md`
- `docs/driver-conformance.md`
- `docs/governance-setup.md`

**Estimated scope:** M: 5-6 docs files; split if the docs drift is larger.

## Final Verification

Run the full production-readiness check:

```bash
pnpm exec nx run execution-kernel:build
(cd go/execution-kernel && go test ./...)
pnpm exec vitest run
pnpm exec oxlint .
pnpm exec eslint .
```

Then run focused claim checks:

```bash
rg "escalate|approval|delegate_task|kanban|scheduler|workflow" go/execution-kernel
rg "OTEL|otel" go/execution-kernel/internal/gov go/execution-kernel/internal/driver
```

Expected result: no authority-path approval/orchestration/model-call regressions, and no OTEL dependency in gate or driver authority code.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Driver support claims exceed actual hooks | High | Start with conformance matrix and fixtures before behavior changes. |
| Fail-closed paths break monitor-mode dogfooding | Medium | Keep monitor mode explicit and covered by tests. |
| Decision log and event chain semantics diverge | High | Make durability and replay tasks early; document whether decision JSONL remains parallel or folds into event chain. |
| Bounds checks become slow or flaky | Medium | Keep bounds fixtures local and deterministic; isolate git-state readers. |
| Router signals re-grow orchestration behavior | High | Add grep checks and pure-Go tests; keep signals advisory. |
| Documentation overclaims production support | Medium | Make docs the final task after tests define the truth. |

## Parallelization Opportunities

Safe to parallelize after Task 1:

- Driver fixture batches by driver, if each worker owns disjoint `internal/driver/<driver>/` files.
- OTEL post-commit tests and router signal invariant tests after chain verification assumptions are clear.
- Documentation wording drafts after behavior tasks settle, with one final owner reconciling claims.

Must be sequential:

- Policy/gate hardening before decision durability.
- Decision durability before replay/index verification.
- Final documentation alignment after tested behavior is known.

## Open Questions

1. Should the latency budget be explicit now, or deferred until the correctness pass is complete?
2. Should decision JSONL remain a parallel canonical log, or should this plan include folding it into the event chain as first-class `decision` events?
3. What fixture count is enough for a driver to be called production-supported: one known write, one read, one shell/exec, one unknown, and one denial?
4. Should `guide` remain a top-level verdict, or become `deny` plus deterministic guidance metadata?

## Review Gate

Do not begin implementation until this plan is reviewed. After approval, execute one task at a time, keep each task in a working state, and run the task-specific verification before moving to the next task.
