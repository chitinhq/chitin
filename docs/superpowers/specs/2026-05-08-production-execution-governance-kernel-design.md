# Production Execution-Governance Kernel - Design Spec

**Date:** 2026-05-08
**Status:** Spec - awaiting operator review before planning or implementation
**Positioning:** Chitin is the `iptables + flight recorder` for AI coding agents.
**Supersedes in direction:** Any spec or plan that treats Chitin as an orchestrator, approval system, workflow runner, swarm manager, generic dashboard, SaaS product, or LLM-in-the-loop safety monitor.

## Assumptions

1. This spec defines the production-grade target for the existing Chitin repository, not a new product or repo.
2. The Go kernel in `go/execution-kernel/` remains the only authoritative side-effect path.
3. Driver adapters normalize tool calls into the kernel's canonical action vocabulary, but they do not make governance decisions.
4. SQLite and JSONL remain the local authoritative storage surfaces; OTEL remains an optional projection.
5. The first production milestone is correctness and boundary hardening, not UI, SaaS, orchestration, or approval flows.

Correct these assumptions before implementation planning if any are wrong.

## Objective

Turn Chitin into a production-grade deterministic execution-governance kernel for heterogeneous AI coding agents.

The kernel intercepts proposed tool calls before side effects occur, normalizes each call into a canonical action, evaluates that action against deterministic local policy, records the resulting decision into a tamper-evident audit chain, updates local derived indexes, and optionally projects telemetry after the local record is durable.

The user is the local operator or downstream substrate that needs a trustworthy execution boundary beneath Claude Code, Codex CLI, Gemini CLI, Copilot CLI, OpenClaw, Hermes, and future coding-agent surfaces.

Success means Chitin can credibly be described as a kernel-level execution exoskeleton: fast, local-first, fail-closed for governance, replayable, cross-driver, policy-driven, and boring enough to trust.

## Product Boundary

Chitin must be:

- Deterministic.
- Local-first.
- Kernel-like.
- Fast enough for the tool-call path.
- Fail-closed for governance decisions.
- Tamper-evident.
- Replayable.
- Cross-driver.
- Policy-driven.
- Boring enough to trust.

Chitin must not become:

- An agent framework.
- An orchestrator.
- A swarm manager.
- A model router.
- A human approval system.
- A kanban or backlog system.
- A workflow DAG runner.
- A generic observability dashboard.
- A SaaS-first product.
- An LLM-in-the-loop safety monitor.

Scope relapse is the top product risk. If a feature does not deepen canonical action normalization, deterministic policy, tamper-evident chain, replay/indexing, cross-driver coverage, bounds enforcement, agent severity state, or optional chain-derived telemetry, it does not belong in this repo.

## Target Architecture

```text
Agent runtime / CLI
  -> driver adapter
  -> canonical action normalization
  -> gov.Gate.Evaluate
  -> allow / deny / guide / lockdown
  -> hash-linked event and decision chain
  -> SQLite-derived indexes
  -> optional OTEL projection after local commit
```

The Go kernel owns the hot path:

```text
tool-call payload
  -> internal/driver/<driver>.Normalize
  -> internal/gov.Action
  -> internal/gov.Gate.Evaluate(action, agent)
  -> internal/gov.Decision
  -> gov-decisions-YYYY-MM-DD.jsonl
  -> chain_index.sqlite / gov.db derived state
  -> optional OTEL emit
```

OTEL must never be authoritative. Telemetry failure must not compromise the local chain, and policy decisions must never depend on OTEL data.

## Tech Stack

- Go: canonical execution kernel, governance, normalization, chain writes, policy evaluation, replay, router signals, and driver-specific kernel code under `go/execution-kernel/`.
- TypeScript: contracts, read-side telemetry libraries, thin adapters, and operator CLI surfaces that shell into the kernel rather than reimplementing authority.
- Nx + pnpm: monorepo task orchestration for builds, tests, lint, and type checks.
- SQLite: local derived indexes, agent state, budgets, and severity counters.
- JSONL: local append-only chain and decision log records.
- YAML: `chitin.yaml` deterministic policy input.
- OTLP/HTTP JSON: optional post-commit telemetry projection.

## Commands

Install dependencies:

```bash
pnpm install
```

Build the Go kernel:

```bash
pnpm exec nx run execution-kernel:build
```

Run all Go tests:

```bash
(cd go/execution-kernel && go test ./...)
```

Run a focused Go package test:

```bash
(cd go/execution-kernel && go test ./internal/gov -run TestGate)
```

Run TypeScript tests:

```bash
pnpm exec vitest run
```

Run project-scoped TypeScript tests:

```bash
pnpm exec nx run @chitin/cli:test
pnpm exec nx run @chitin/contracts:test
pnpm exec nx run @chitin/telemetry:test
```

Lint Go:

```bash
pnpm exec nx run execution-kernel:lint
```

Lint TypeScript:

```bash
pnpm exec oxlint .
pnpm exec eslint .
```

Type-check TypeScript projects:

```bash
pnpm exec nx run @chitin/contracts:typecheck
pnpm exec nx run @chitin/telemetry:typecheck
```

Run the operator CLI from source:

```bash
pnpm exec nx run @chitin/cli:run -- --help
```

## Project Structure

```text
go/execution-kernel/
  cmd/chitin-kernel/          Go CLI entrypoint and subcommands
  internal/gov/               Gate, policy, decisions, bounds, agent state
  internal/driver/            Driver payload normalization
  internal/chain/             Hash-linked chain write/read primitives
  internal/emit/              Canonical emit path and optional OTEL projection
  internal/replay/            Replay, summaries, stats, related-chain lookup
  internal/router/            Pure-Go chain-derived signals only

libs/contracts/
  src/                        Canonical TypeScript schemas and shared helpers

libs/telemetry/
  src/                        Read-side indexing and query utilities

libs/adapters/
  */                          Thin driver-side adapters; no authority

apps/cli/
  src/                        Operator CLI wrapping kernel subcommands

python/analysis/
  */                          Chain-derived analytics readers

docs/decisions/
  *.md                        Durable boundary decisions

docs/superpowers/specs/
  *.md                        Reviewed specs before implementation
```

## Code Style

Kernel code should keep authority explicit, deterministic, and easy to audit. Prefer small functions, table-driven normalization tests, closed enums, and plain error propagation over clever control flow.

Example style for a hot-path decision helper:

```go
func (g *Gate) Evaluate(action Action, agent string) Decision {
	if g.locked(agent) {
		return g.deny(action, agent, "agent_lockdown", "agent is in lockdown")
	}

	decision := g.policy.Evaluate(action)
	if decision.Allowed {
		return g.record(decision)
	}

	decision.Escalation = g.counter.RecordDenial(agent, action.Fingerprint())
	return g.record(decision)
}
```

Conventions:

- Unknown action types fail closed.
- Policy parsing validates stale or unsupported effects loudly.
- Normalizers preserve raw payloads for audit while evaluating only canonical actions.
- The kernel does not call LLMs in the hot path.
- Chain and decision writes happen before non-authoritative projections.
- TypeScript surfaces do not duplicate kernel authority.

## Testing Strategy

Use unit tests for deterministic kernels and integration tests for on-disk invariants.

Required test levels:

- Normalizer tests: every supported driver maps representative tool payloads to the same canonical action where semantics match.
- Policy tests: rule matching, inheritance, malformed policy rejection, stale effect rejection, branch/path/bounds predicates, and unknown-action denial.
- Gate tests: allow, deny, guide, lockdown, counter persistence, decision logging, and fail-closed error paths.
- Chain tests: hash continuity, replayability, SQLite index derivation, and tamper detection.
- OTEL tests: projection happens after commit and failure does not affect the canonical write.
- Driver conformance tests: each supported driver has fixture payloads for known tools and unknown-tool behavior.
- CLI smoke tests: kernel subcommands return stable exit codes and machine-readable JSON.

Acceptance test for production readiness:

```bash
pnpm exec nx run execution-kernel:build
(cd go/execution-kernel && go test ./...)
pnpm exec vitest run
pnpm exec oxlint .
pnpm exec eslint .
```

## Kernel Requirements

### 1. Interception

Every supported driver must have a documented pre-side-effect integration point. The integration can be a native hook, plugin callback, SDK wrapper, or substrate-owned adapter, but the tool call must reach `chitin-kernel gate evaluate` or equivalent kernel path before side effects occur.

Acceptance:

- `docs/driver-conformance.md` lists each live driver, integration point, real-time gating status, fixture location, and known coverage gaps.
- Each live driver has tests proving representative tool calls normalize and evaluate.
- Missing or malformed payloads deny by default unless explicitly marked monitor-only in policy.

### 2. Canonical Action Vocabulary

The action vocabulary is closed and owned by the Go kernel. Driver payloads normalize into `gov.Action` values. Unknown or unsupported actions become `ActUnknown` and are denied by default.

Acceptance:

- `internal/gov/action.go` is the canonical enum.
- `internal/driver/*/normalize.go` produces only known actions or `ActUnknown`.
- Cross-driver equivalents normalize consistently. Example: shell command execution from Claude Code, Codex, Gemini, Hermes, and OpenClaw maps to the same semantic action where the proposed effect is the same.
- Action vocabulary expansion requires tests and policy examples.

### 3. Deterministic Policy

`chitin.yaml` is the deterministic policy input. Evaluation must be pure with respect to the normalized action, policy, agent state, and local repository state needed for declared predicates.

Acceptance:

- Policy load validates all actions, effects, predicates, and bounds.
- Unsupported effects such as culled approval/escalate effects fail parse.
- Policy predicates are structured (`path_under`, `bounds`, `branches`) rather than regex-only shell matching.
- Failures to load required policy fail closed for enforcement paths.

### 4. Decisions

The gate returns one of `allow`, `deny`, `guide`, or `lockdown`-equivalent denial. `guide` is deterministic corrective feedback, not human approval and not LLM consultation.

Acceptance:

- Decision JSON includes action type, target, agent, rule id, decision, reason, timestamp, and chain linkage fields where available.
- Decision logging errors are propagated or fail closed on the governance path.
- Lockdown is persisted in local state and survives sessions.

### 5. Tamper-Evident Chain

Every canonical event and governance decision must be recoverable from local append-only records and verifiable by hash linkage.

Acceptance:

- `gov-decisions-YYYY-MM-DD.jsonl` and event JSONL files are append-only from the kernel perspective.
- `chain_index.sqlite` is derived and rebuildable.
- Replay detects missing, reordered, or modified records.
- The canonical local record exists before optional telemetry projection.

### 6. Bounds Enforcement

Push-shaped and large-change actions must be bounded by policy. Bounds are a Chitin moat feature and should remain kernel-owned.

Acceptance:

- File-count, line-count, and protected-path checks run before push-shaped side effects.
- Bounds failure denies even if individual edits were allowed.
- Bounds checks have tests for normal, over-limit, protected-path, and unable-to-compute cases.

### 7. Agent Severity State

Agent behavior across sessions is tracked locally. Repeated denied patterns raise severity and can enter lockdown.

Acceptance:

- `gov.db` persists per-agent counters and lockdown state.
- Counters are keyed on stable action fingerprints, not raw formatting.
- Reset and status commands are explicit operator actions.
- Severity state is not a human approval workflow.

### 8. Signals

Blast-radius, floundering, and drift signals remain pure-Go chain-derived heuristics. They may stamp advisory fields onto decisions or route records, but they do not spawn LLMs and do not orchestrate work.

Acceptance:

- Signal computation is deterministic and test-covered.
- Signals are emitted as local chain data or decision annotations.
- No signal path performs model calls, agent delegation, workflow scheduling, or operator approval prompting.

### 9. Telemetry Projection

OTEL is optional and post-commit. Chitin exports telemetry without allowing telemetry failure to compromise the canonical local record.

Acceptance:

- OTEL is disabled unless explicitly configured.
- OTEL projection is derivable from chain records.
- OTEL failure does not fail an already-committed canonical write.
- No policy decision depends on OTEL.

## Boundaries

Always:

- Preserve the kernel boundary.
- Keep authority in Go kernel code.
- Deny unknown action types by default.
- Write canonical local records before telemetry.
- Use structured policy predicates.
- Add tests for every action vocabulary, policy, chain, or driver behavior change.
- Update decision docs when a boundary changes.

Ask first:

- Adding dependencies to the kernel hot path.
- Adding a new app surface.
- Changing `chitin.yaml` schema compatibility.
- Changing chain hash format or event schema.
- Changing driver support claims in README or roadmap.
- Adding any network behavior outside optional telemetry.

Never:

- Add orchestration, kanban, scheduling, workflow DAGs, or backlog ownership to Chitin.
- Add human approval prompts to Chitin.
- Spawn or consult an LLM from the kernel hot path.
- Make OTEL authoritative.
- Let TypeScript libraries duplicate governance authority.
- Add SaaS-first assumptions or remote mandatory services.
- Reintroduce MCP server hosting in Chitin.
- Treat dashboards as production readiness.

## Success Criteria

Production-grade means all of the following are true:

- A representative tool call from every supported driver is intercepted before side effects, normalized, evaluated, logged, and replayable.
- Unknown or malformed actions fail closed in enforcement paths.
- Policy evaluation is deterministic and covered by tests.
- Decision logs and event chains can be verified for hash continuity.
- `chain_index.sqlite` can be rebuilt from local canonical records.
- Bounds enforcement blocks oversized or protected changes even when the edit sequence was individually allowed.
- Agent severity and lockdown persist across sessions.
- OTEL projection can fail without corrupting or preventing the local canonical record.
- The README, roadmap, architecture docs, and driver conformance docs make the same production claims as the tests prove.
- No production-readiness task adds orchestration, approvals, workflow management, SaaS dependency, or LLM-in-loop safety behavior.

## Open Questions

1. Should production readiness require a formal latency budget for `gate evaluate` per tool call, such as p95 under 50 ms excluding bounds checks?
2. Should `gov-decisions-YYYY-MM-DD.jsonl` be folded into the same event chain as `decision` events immediately, or remain a parallel canonical decision log with replay linkage?
3. What is the minimum driver conformance fixture set for declaring a driver production-supported?
4. Should `guide` remain a decision value, or should it be represented as `deny` plus a deterministic `guidance` field to keep the verdict enum smaller?
5. Should protected-path and bounds checks live exclusively in `internal/gov`, or should some repository-state readers move to `internal/chain` or a new kernel-internal package?

## Review Gate

Do not proceed to implementation planning until this spec is reviewed. The next phase should produce an ordered implementation plan with small tasks, each touching no more than about five files and each with an explicit verification command.
