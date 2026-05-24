# 095 — Continue Checks Pilot

> Operator request 2026-05-23 after cron inventory cleanup: pilot Continue Checks as the highest-leverage governance experiment, after papers/Continue/Claude Harness/AgentWall/SafeAgent/SDOF review context.

## Ticket refs

- None yet — operator-attended spec-first pilot. Any implementation ticket MUST reference this spec after the spec PR is ratified.

## File-system scope

Pilot scope is intentionally narrow and reversible:

- `.continue/checks/` (new) — small Continue Checks rules that evaluate PR/governance artifacts.
- `.continue/config.yaml` or `.continue/config.json` (new/modify, only if required by Continue's local check runner) — minimal local configuration pointing at the pilot checks.
- `docs/continue-checks-pilot.md` (new) — operator runbook: what the pilot checks, how to run it, how to interpret failure, and how to disable it.
- `.github/workflows/continue-checks.yml` (optional, gated by operator approval) — CI-only pilot invocation if local dry-run proves useful.
- `.specify/specs/095-continue-checks-pilot/` — this spec and follow-on plan/tasks artifacts.

Out-of-scope paths for the pilot:

- No changes to `go/execution-kernel/` hot path.
- No Hermes replacement or orchestration behavior.
- No broad LLM review bot rollout.
- No exposed webhooks, tunnels, or externally-triggered inbound surface.

## Goal

Evaluate whether Continue Checks can serve as a lightweight PR governance gate for Chitin by encoding a few narrow, deterministic-ish review rules around spec linkage, e2e justification, and no-driver-bypass invariants. The pilot should answer: "Does Continue add useful, low-noise policy review signal at PR time without duplicating Chitin's kernel, Hermes approvals, or the Temporal orchestrator?"

The pilot treats Continue as a **PR governance check surface**, not as a new agent substrate.

## Acceptance criteria

AC1. **Narrow check set.** The pilot defines no more than three initial Continue Checks, each tied to an existing Chitin governance invariant:

- spec-first linkage: implementation PRs mention a ratified `.specify/specs/NNN-<slug>/spec.md`;
- e2e-default discipline: non-e2e test plans include a named justification subsection per spec 020 / 024;
- no-driver-bypass: driver/orchestrator changes do not introduce direct executable dispatch outside the approved orchestrator/driver boundary.

AC2. **Local dry-run before CI.** The first implementation slice MUST provide a local dry-run command and sample output before any GitHub required-check or branch-protection integration is proposed.

AC3. **Fail-closed only after evidence.** Initial pilot checks MAY report warnings or non-required CI failures. They MUST NOT become required merge gates until at least five PRs have been evaluated and false-positive/false-negative notes are recorded in `docs/continue-checks-pilot.md`.

AC4. **No inbound exposure.** The pilot MUST NOT add webhooks, tunnels, public callbacks, or any external inbound surface. GitHub Actions, if used, runs on repository PR events only.

AC5. **No substrate replacement.** The pilot MUST NOT route work, spawn agents, approve commands, or mutate PRs. It only evaluates PR artifacts and emits check output.

AC6. **Evidence record.** The runbook records every evaluated PR with: PR number, checks run, pass/fail/warn result, operator decision, and whether the result was useful/noisy.

AC7. **Spec-kit entry.** This file exists at `.specify/specs/095-continue-checks-pilot/spec.md` and is registered in `.specify/specs/INDEX.md`.

AC8. **Governance gates pass.** The spec PR introducing this pilot passes existing Chitin governance/spec-kit gates.

## Test coverage

Per spec 024 §1.2, e2e is the default. Because this is a policy-check pilot, the implementation test plan MUST include:

- **E2E dry-run fixture:** run Continue Checks against at least one synthetic PR/diff fixture that should pass and one that should fail spec-first linkage.
- **Regression fixture:** a driver/orchestrator diff that attempts direct dispatch outside the allowed path is flagged by the no-driver-bypass check.
- **Non-e2e justification:** unit tests for rule parsing are allowed only under a named `Non-e2e justification` subsection in the implementation plan/tasks, because parser-level tests cannot prove PR-surface behavior.

## Invariants

I1. **Chitin kernel remains the execution-governance authority.** Continue Checks may advise on PR artifacts; they do not replace `chitin-kernel gate evaluate` or chain/audit enforcement.

I2. **The orchestrator remains the executable swarm boundary.** Any check that reasons about dispatch must reinforce, not weaken, the rule that executable swarm work reaches drivers only through the approved orchestrator/driver path.

I3. **Operator approval remains separate.** Continue Checks cannot approve commands, merge PRs, override governance lockdown, or bypass spec-first gating.

I4. **Pilot is reversible.** Removing `.continue/` and the optional workflow restores pre-pilot behavior without data migration or service teardown.

## Dependencies

- Spec 020 — SDD+TDD enforcement and e2e-default discipline.
- Spec 024 — active-repo doc bundle and spec-kit convention.
- Spec 070 — Chitin orchestrator boundary and worktree isolation.
- Spec 075 — agent-driver contract and driver registry boundary.
- Spec 092 — no-driver-bypass invariant pattern, if ratified before implementation.
- Spec 094 — PR review mechanism; this pilot should complement, not duplicate, dialectic review.

## Out of scope

- Required branch protection changes.
- Automatic PR comments beyond check output.
- Any agent execution, task routing, or code mutation by Continue.
- Any non-local secret handling or webhook exposure.
- Full replacement of human/operator review.

## Open questions

O1. Continue config format: YAML vs JSON in this repo. Proposed: choose the simplest format supported by the installed Continue Checks runner during implementation discovery.

O2. First check severity. Proposed: warnings/non-required status for the first five PR observations, then revisit whether any check deserves required-gate status.

O3. Fixture location. Proposed: keep synthetic fixtures under `.specify/specs/095-continue-checks-pilot/fixtures/` unless Continue requires a different path.
