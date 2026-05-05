# Chitin as the agent-systems conformance substrate

Status: design. Supersedes `2026-05-05-tier-driver-matrix.md` (which
made the wrong-shape mistake of trying to curate a static matrix).

Date: 2026-05-05

## What this is, in one sentence

Chitin is the execution-normalization + scoring layer that turns
generic agent benchmark corpora (SWE-bench, Terminal-Bench, OpenHands,
Aider) into **continuously-updated, vectorized capability profiles per
(driver, model)** — joined to the operator's actual repo outcomes.

## Why this exists

Three failure modes the agent ecosystem keeps hitting:

1. **Static matrices drift.** Manually curated tier×driver×model
   tables go stale within a release cycle (model names rename, providers
   change pricing, drivers mutate behavior). Operators lose hours
   re-deriving them.
2. **Single-scalar scores collapse the wrong dimensions.** SWE-bench
   tells you "Opus solves 78%." It does not tell you that Opus + Aider
   solves 78% but Opus + Copilot CLI loops on tool calls 30% of the
   time. The collapse is `model → score`; the truth is
   `(driver, model, orchestrator, tool_layer, repo_topology,
   context_depth, runtime_constraints, task_class) → vectorized_score`.
3. **Demo benchmarks beat operational benchmarks.** Most published
   numbers come from optimized demos. The numbers operators care about
   — patch integrity, recovery behavior, governance compliance, CI
   survivability — get measured nowhere. So routing decisions are made
   on irrelevant data.

Chitin uniquely sits at four layers no one else combines:
**execution boundary + normalized telemetry + driver abstraction +
policy gate.** That gives it the substrate to **observe** capability
empirically per operator's actual workloads — not just declare it from
docs.

## What chitin is NOT

- **Not a new benchmark dataset.** That's the trap. The corpora exist;
  the value is in normalized execution across them.
- **Not a hardcoded driver/model registry.** Driver and model are
  separate identities; their compatibility is observed, not declared.
- **Not a single-scalar capability score.** Vectorized profiles only.

## The 12-dimension conformance ontology

Every (driver, model) cell gets scores in these 12 dimensions. Some
are observable from chitin's existing telemetry today (✅), some need
new joins (⚠️), some need new instrumentation (❌).

| # | Dimension | Definition | Status today |
|---|---|---|---|
| 1 | tool_call_validity | malformed-args rate (action_type=unknown / argparse rejects) | ⚠️ partial — gov-decisions has the unknown signal |
| 2 | patch_integrity | post-edit compile/test success rate | ❌ needs CI-result join with dispatch fingerprint |
| 3 | execution_stability | crash + loop frequency per session | ✅ floundering heuristic + ActivityResult.exit_code |
| 4 | long_context_survivability | task success degradation past N context tokens | ❌ needs per-attempt context-length tagging |
| 5 | routing_effectiveness | escalation_requested rate per attempt | ✅ runner loop telemetry (Phase 1 wired) |
| 6 | cost_efficiency | tokens or $ per solved task | ⚠️ partial — gov-decisions has cost_usd; need solved-task signal |
| 7 | governance_compliance | recovery rate from elevated/high escalation states | ✅ escalation Counter in `gov/escalation.go` |
| 8 | recovery_behavior | retry-success vs deadlock rate | ✅ runner attempt-end telemetry, aggregated |
| 9 | repo_mutation_quality | PR-merged rate (vs closed-without-merge) per dispatch | ⚠️ partial — `gh pr list` exists; needs join with dispatch fingerprint |
| 10 | ci_survivability | post-merge CI pass rate per dispatch | ⚠️ partial — `gh pr checks` exists; needs join |
| 11 | latency | wall-clock duration p50/p95 per attempt | ✅ ActivityResult.duration_ms |
| 12 | determinism | same-prompt replay consistency rate | ❌ needs deterministic-replay harness |

**6 of 12 measurable today, 4 with joins, 2 with new instrumentation.**
A roadmap of small commits gets us to all 12.

## The vectorized capability profile

The atomic record:

```yaml
profile:
  driver: copilot
  model: gpt-5.4
  scores:                        # 0.0 – 1.0 per dimension
    tool_call_validity: 0.96
    patch_integrity: 0.91
    execution_stability: 0.88
    long_context_survivability: 0.83
    routing_effectiveness: 0.74  # 0.74 = needed escalation 26% of attempts
    cost_efficiency: 0.68
    governance_compliance: 0.92
    recovery_behavior: 0.79
    repo_mutation_quality: 0.84
    ci_survivability: 0.89
    latency: 0.71
    determinism: 0.82
  context:
    last_observed: 2026-05-05T12:34:56Z
    n_observations: 1247
    repo: chitinhq/chitin
    task_class_breakdown: { refactor: 612, bug_fix: 278, exploration: 357 }
  provenance:
    corpus_seed: terminal-bench-v0.42  # what corpus contributed observations
    operator_traces: 1119               # observations from real operator dispatches
```

Stored as sqlite (joins cleanly with `chain_index.sqlite`) + exported
as JSON for portability.

## The bench substrate (existing corpora — DO NOT INVENT NEW ONES)

The hardest mistake to avoid: rolling our own benchmark dataset.
Existing corpora are sufficient:

| Corpus | What it tests | License | Integration status |
|---|---|---|---|
| **SWE-bench** | Real GitHub issue → patch tasks across 12 repos | MIT | not yet wired |
| **Terminal-Bench** | Terminal-agent execution (file ops, shell, git, build) | Apache-2.0 | already integrated via `clawta/bench/` |
| **OpenHands eval** | Multi-step coding agent harnesses | MIT | not yet wired |
| **Aider corpus** | Edit-task benchmarks | Apache-2.0 | not yet wired |
| **Operator traces** | Real chitin dispatches replayed | n/a | already in `~/.chitin/events-*.jsonl` |

Each corpus contributes observations across SOME of the 12 dimensions.
No corpus alone is sufficient; the union is.

## The execution pipeline

```
[Corpus: SWE-bench task]
        ↓
[Driver adapter — converts the corpus's task shape into ExecutionRequest]
        ↓
[chitin-execute-request --from-corpus-task <id>]
        ↓
[runAgentTurn spawns the (driver, model) under test]
        ↓
[chitin-kernel gates every tool call, records to gov-decisions chain]
        ↓
[ActivityResult + chain events + worktree diff captured]
        ↓
[scoring pipeline — projects raw telemetry into the 12 dimensions]
        ↓
[capability profile updated for (driver, model)]
```

Each box is small + composable. The driver adapters are tiny
glue (~50 LOC per corpus); the chitin substrate (kernel + chain +
analysis) does the heavy lifting.

## The routing query API

Replaces the previous matrix doc's hardcoded TIER_DRIVER tables.

```typescript
type OptimizationTarget =
  | 'cheap+stable'           // T0 swarm — minimize cost while staying above stability floor
  | 'reasoning_depth'        // T4 architect — maximize reasoning quality, cost is fine
  | 'patch_quality'          // CI remediation — deterministic, compile-clean diffs
  | 'recovery'               // autonomous PR agents — survives gov denials, finishes the task
  | 'latency'                // user-facing — p95 wall-clock dominates
  | (...);                   // operator-extensible

type Constraint = {
  budget_per_call_usd?: number;
  required_dimensions?: { [dim: string]: number };  // floors
  exclude_drivers?: DriverId[];
  exclude_models?: string[];
};

routeFor(task_signature, optimization, constraints) → Ranked<{driver, model, score, profile}>
```

Operator declares the optimization target per workload. Routing
returns a ranked list (top candidate is "primary"; rest are
fallbacks). Runner tries primary; on failure, falls through with
escalation context (which itself becomes new observed data).

## Cold-start

For (driver, model) cells with `n_observations < THRESHOLD`, fall
back to **operator-declared seed config** (the matrix from the
superseded `tier-driver-matrix.md` doc — kept as bootstrap default).
As observations accumulate, seeds are overridden by data.

`THRESHOLD = 30` per (driver, model, task_class) bucket is a
reasonable default; tunable.

## Optimization targets, by example workload

The operator's tier definitions become **named optimization targets**:

| Workload | Optimization | Routing prefers |
|---|---|---|
| T0 backlog swarm | `cheap+stable` | high `cost_efficiency` + floor on `execution_stability` |
| T4 architect | `reasoning_depth` | high `routing_effectiveness` (low escalation rate) + `patch_integrity` |
| CI remediation | `patch_quality` | high `patch_integrity` + `determinism` + `ci_survivability` |
| Autonomous PR | `recovery` | high `governance_compliance` + `recovery_behavior` |
| User-facing chat | `latency` | high `latency` (= low p95) + reasonable `tool_call_validity` |

The routing layer is the same for all; the optimization target is
the dial.

## Roadmap to GA — incremental, each step shippable

1. **Storage** — `compatibility_profiles.sqlite` (mirror of
   `chain_index.sqlite`) + JSON export. ~1 day.
2. **Existing-dimension extractors** — python analysis scripts that
   project gov-decisions + events + ActivityResults into the 6
   already-measurable dimensions. ~3 days.
3. **PR/CI joins** — extend `kanban-pr-mirror.ts` to also write
   PR-state + CI-result records keyed on dispatch workflow_id. ~1
   day. Unblocks 4 more dimensions.
4. **Routing query API** — `routeFor()` typed function in
   `libs/contracts` that reads the sqlite. Replaces the hardcoded
   TIER_DRIVER map in `apps/runner/src/dispatcher.ts` and
   `kanban-card-to-request.ts`. ~2 days.
5. **Driver adapters for one external corpus** — start with
   Terminal-Bench (already integrated via `clawta/bench/`) → write
   the chitin-execute-request bridge that runs corpus tasks through
   the substrate. ~3 days.
6. **Add SWE-bench adapter** — second corpus shows the pattern
   generalizes. ~3 days.
7. **Long-context tagging + deterministic replay** — closes the
   last 2 dimensions. ~1 week.
8. **Operator-facing capability dashboard** — chitin-results
   plugin in the hermes dashboard surfaces the per-cell capability
   vectors. ~1 week.

Total: ~5 weeks of focused work to GA, with shippable milestones at
each step.

## The moat

Why this layer is defensible:

- **Execution boundary**: chitin sits between the agent CLI and the
  shell. Every tool call passes through. No other tool has this
  vantage.
- **Normalized telemetry**: chitin's chain JSONL is one schema across
  all drivers (claude-code, copilot, codex, gemini, openclaw, hermes).
  No other tool normalizes across drivers.
- **Driver abstraction**: chitin already has a typed
  `DriverIdSchema` + per-driver normalizers. Adding a new driver
  doesn't change the data model.
- **Policy gate**: chitin's kernel decides allow/deny per action.
  That decision is itself a data point about the (driver, model)'s
  ability to play within constraints.

Combine those four and chitin can answer questions no benchmark or
provider dashboard can:

- "What's Copilot/gpt-5.4's actual recovery rate against my repo's
  governance policy?" — empirical, repo-specific, dated.
- "Does Codex/gpt-5.5 outperform Anthropic/opus on patch quality
  across the last 100 dispatches?" — empirical, comparative.
- "Should this PR comment-respond escalation go to copilot or
  claude-code?" — empirical query, not a static rule.

That's a real moat. It's the substrate every serious operator will
eventually need; today only chitin can ship it.

## Open questions (operator decides)

1. **Storage**: sqlite alongside `chain_index.sqlite`, or a
   separate `compatibility.sqlite`? (My lean: separate — different
   write cadence, different consumers.)
2. **Replay determinism**: same-seed, same-prompt re-runs need a
   deterministic-replay harness. Build it now or wait until we
   have observable replay-divergence problems? (My lean: defer.)
3. **Corpus install footprint**: SWE-bench is large (~5GB);
   should chitin install all corpora by default or be opt-in
   per corpus? (My lean: opt-in.)
4. **Operator-facing dimension weights**: the 12 dimensions are
   equal-weighted in the routing query unless the operator
   declares otherwise. Should chitin ship default weights per
   optimization target? (My lean: yes — discoverable defaults
   per target name.)
