# Implementation Plan: Driver Governance & Telemetry Integrity

**Branch**: `main` (worktrees per work unit, constitution §2) | **Date**: 2026-05-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/083-driver-governance-telemetry/spec.md`

## Summary

Make driver governance **provable**. A 2026-05-21 audit
(`docs/2026-05-21-orchestrator-driver-telemetry-audit.md`) found only 3 of 6
dispatchable drivers proven-governed. This plan restores the regressed
hermes/clawta telemetry (US1), hardens the kernel-redeploy pipeline so merged
fixes cannot strand (US2), brings copilot/codex/gemini and orchestrator
worktrees to provably-governed (US3), and unifies the three telemetry sinks
behind one queryable interface with a `chitin doctor` that validates against a
live probe (US4). The work is governance plumbing and observability — no new
agent capability.

## Technical Context

**Language/Version**: Polyglot (spec 074). Go 1.23+ (`go/execution-kernel` —
the kernel, gate, telemetry), TypeScript (`apps/openclaw-plugin-governance` —
the OpenClaw bridge; `apps/chitin-console`), Python 3 (the Hermes governance
plugin `docs/governance-setup-extras/hermes-plugin.py`; `python/analysis`),
Bash (`scripts/install-kernel.sh` and the hook installers).

**Primary Dependencies**: the chitin kernel and its `gate evaluate` surface;
the agent CLIs (claude, codex, copilot, gemini) and their hook mechanisms; the
Hermes and OpenClaw runtimes and their governance plugins; the
`github.com/github/copilot-sdk/go` SDK (the copilot-shim dependency at the
centre of the copilot defect).

**Storage**: append-only JSONL telemetry — `~/.chitin/gov-decisions-YYYY-MM-DD.jsonl`
(central), `~/.chitin/codex-events-<session>.jsonl` (per-session),
`~/.chitin/events-openclaw-clawta-*.jsonl` (per-runtime); plus
`~/.chitin/chain_index.sqlite` / `gov.db`. The fragmentation across these is
US4's problem.

**Testing**: `go test ./...` in `go/execution-kernel`; the audit's probe
method (one-shot driver invocation → assert a `gov-decision` row) as the
acceptance test for US1/US3; `scripts/smoke-hermes-clawta-chain.sh` for US1.

**Target Platform**: the operator's Linux box — systemd `--user` services
(kernel redeploy timer, orchestrator, Temporal) plus the long-running Hermes
and OpenClaw processes.

**Project Type**: governance kernel + multi-agent orchestrator (not web/mobile).

**Performance Goals**: telemetry is a write-only side effect off the
scheduling/gating critical path (spec 070 FR-008); it MUST NOT add latency to a
tool call. The unified query interface (US4) is read-side, no hot-path budget.

**Constraints**: a redeploy MUST leave a working kernel in place on failure
(no governance outage); enforcement changes roll out observe-before-deny
(constitution-aligned, spec 028 pattern); the kernel is the only chain writer
(constitution §1).

**Scale/Scope**: 6 drivers; 3 telemetry sinks to unify; ~15-min redeploy
cadence; one kernel binary + two agent-runtime plugins (Hermes, OpenClaw).

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

| Rule | Assessment |
|---|---|
| §1 Side-effect boundary — only the kernel gates tool calls / writes the chain | ✅ All governance + telemetry work lands in `go/execution-kernel`. US4's unified interface is a **reader** over existing sinks — it writes nothing. |
| §2 Workers + worktrees — primary checkout is never a work surface | ✅ Implementation runs as orchestrator work units in dedicated worktrees. **Process note:** this spec's authoring happened in the primary checkout — a §2 deviation to correct going forward (and exactly what the proposed spec-084 SDD gate would catch). |
| §3 Spec-kit promotion gate | ✅ This feature has `specs/083-driver-governance-telemetry/spec.md`. |
| §4 Tracked installers | ✅ Changes to `install-kernel.sh` and the hook installers stay in the repo; no new runtime artifact ships without its installer. |
| §6 Kernel-local logic belongs in `cmd/`/`internal/`/`libs/`, not `swarm/` | ✅ Gate, telemetry, and doctor changes are kernel-internal. |

**No violations.** Complexity Tracking not required.

## Project Structure

### Documentation (this feature)

```text
specs/083-driver-governance-telemetry/
├── plan.md              # This file
├── spec.md              # Feature spec
├── research.md          # Phase 0 — decisions on the 7 audit defects
├── data-model.md        # Phase 1 — gov-decision record, driver status, kernel build
├── quickstart.md        # Phase 1 — the per-driver governance-proof runbook
├── contracts/           # Phase 1 — gov-decision schema, doctor verdict, query interface
└── tasks.md             # Phase 2 — /speckit-tasks (not produced here)
```

### Source Code (repository root)

```text
go/execution-kernel/
├── cmd/chitin-kernel/        # gate_hook.go, drive_copilot.go, doctor — kernel + shim entrypoints
├── internal/gov/             # gate.go, decision.go, policy.go — decision record + verdict (US1, US3)
├── internal/driver/copilot/  # client.go, driver.go — the copilot shim (US3: SDK timestamp fix)
├── internal/telemetry/       # sinks; US4 unified query interface lands here
└── internal/health/          # US2: kernel-staleness detection surfaced via `chitin health`

scripts/
├── install-kernel.sh         # US2: replace `git pull` with fetch + `merge --ff-only`
└── install-*-hook.sh         # US3: hook installers (codex/copilot/gemini/hermes)

docs/governance-setup-extras/hermes-plugin.py   # US1: Hermes plugin (already #861-fixed in main)
apps/openclaw-plugin-governance/src/            # US1: OpenClaw bridge (already #861-fixed in main)
```

**Structure Decision**: All governed-telemetry logic is kernel-internal under
`go/execution-kernel` (constitution §1, §6). US1's plugin fixes already exist
in `main` (PR #861) — US1 is a deploy + process-restart, not new code. The
redeploy script and hook installers are the only `scripts/` changes.

## Phasing — mapped to the user stories

The four user stories are the implementation phases; each is independently
shippable (spec §"Independent Test").

- **US1 (P1) — Restore hermes/clawta.** Deploy the #861 kernel (done this
  session via `install-kernel.sh`); restart the Hermes gateway + agent so they
  load the #861 plugins; verify with `smoke-hermes-clawta-chain.sh`.
- **US2 (P2) — Redeploy can't strand.** Harden `install-kernel.sh` (fetch +
  `merge --ff-only`); add kernel-staleness detection to `chitin health`; make
  redeploy failure a surfaced alert, not a silent log line.
- **US3 (P3) — All drivers proven-governed.** Fix the copilot shim's
  `copilot-sdk` timestamp mismatch; route codex governance to the central
  sink; teach the system the *unverified* driver state; ensure dispatched
  worktrees are governed.
- **US4 (P4) — Trustworthy observability.** A unified read interface over the
  three sinks; rebuild `chitin doctor` to validate via a live probe and credit
  global hooks.

## Complexity Tracking

No constitution violations — section intentionally empty.
