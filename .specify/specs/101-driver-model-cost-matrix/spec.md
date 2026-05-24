---
spec_id: 101
title: Driver × Model × Cost-Tier Matrix
status: Draft
owner: chitinhq
created: 2026-05-23
depends_on:
  - 075
  - 076
  - 094
related:
  - 099
---

# Spec 101 — Driver × Model × Cost-Tier Matrix

## Why

The current driver registry (spec 075) treats every driver as opaque: a driver IS its default model. `codex.New()` hard-codes `gpt-5.x-codex`. `claudecode.New()` defaults to whatever Claude Code's CLI picks (likely Sonnet). `copilot.New()` shells out to `chitin-kernel drive copilot` which defaults to Opus 4.7.

The operator pays for FIVE distinct billing surfaces:

| Surface | Cost | Headroom |
|---|---|---|
| OpenAI codex CLI subscription | $100/mo | capped |
| Ollama Cloud (GLM 5.1 frontier) | $100/mo | "lots of headroom" |
| GitHub Copilot CLI | metered per token (model-dependent) | depends entirely on which model |
| Anthropic Claude Code | $200/mo credit pool | limited; avoid Opus |
| Local Ollama (Qwen 3.6) | $0 per request | local resource, use sparingly |

The current code makes **none** of this visible at routing time. `SelectDriver` (spec 075) picks the first ready driver alphabetically — that's `claudecode` before `codex` before `copilot`, and silently spends Anthropic credit when openclaw via Ollama Cloud would have been free-headroom.

**Empirical demonstration from the 2026-05-23 live demo:** the first dispatch picked `copilot` for `code.implement`, which pulled Opus 4.7 through Copilot's metered backend — visible later as the failure path that surfaced this entire cost framework. With `CHITIN_DRIVER_ALLOW=codex` + `CHITIN_CODEX_MODEL=gpt-5.5` (added in PR #949), dispatch was pinned to the right driver-model. But that's a single-driver pin, not a cost-aware policy.

This spec is the policy: a **driver × model** routable unit (not just driver), a **cost-tier** classification, and a **cheapest-qualifying-first** routing preference.

## User Stories

### US1 (P1) — Per-driver model environment hooks

> As the operator, I set `CHITIN_<DRIVER>_MODEL` for any driver in the registry to override its default model at registration time. `CHITIN_CLAUDECODE_MODEL=claude-haiku-4-5` makes claudecode register with Haiku as its bound model. `CHITIN_COPILOT_MODEL=gpt-4.1` makes the Copilot CLI driver register with the free GPT-4.1 model. Empty / unset preserves the driver's hard-coded default.

**Independent test:** Set `CHITIN_CLAUDECODE_MODEL=claude-haiku-4-5`; run `chitin-orchestrator` startup; the registered claudecode driver's `Card().Model` returns `"claude-haiku-4-5"`. Dispatch a node — the claudecode CLI is invoked with `--model claude-haiku-4-5`.

### US2 (P1) — Cost-tier classification on the Card

> Each (driver, model) pair has a declared `CostTier` — one of: `free`, `subscription-paid`, `credit-pool-bounded`, `metered`. The capability card surfaces this so `SelectDriver` can sort candidates by cost. Defaults are conservative: drivers without an explicit tier are treated as `metered` so the operator opts into cheaper paths consciously.

**Independent test:** With the default registry, every driver's `Card().CostTier` is populated. Inspect via `chitin-orchestrator status --drivers` (new flag) — outputs driver_id, model, cost_tier, capabilities.

### US3 (P2) — Cheapest-qualifying-first routing

> `SelectDriver` no longer picks alphabetically. It picks the cheapest qualifying driver for the requested capability, breaking ties by alphabetical driver_id (for determinism). Routing order: `free` → `subscription-paid` → `credit-pool-bounded` → `metered`. Operator override via `CHITIN_DRIVER_ALLOW` (already exists) continues to short-circuit: if the allow set names drivers, only those are eligible, and the cheapest among them wins.

**Independent test:** Default registry: dispatch a `code.implement` task. Without overrides, `SelectDriver` returns `openclaw` (subscription-paid, Ollama Cloud) before `claudecode` (credit-pool-bounded) before any metered surface. With `CHITIN_DRIVER_ALLOW=codex,claudecode`, returns `codex` (subscription-paid) before `claudecode` (credit-pool-bounded).

### US4 (P2) — Telemetry on selection decisions

> Every `SelectDriver` invocation emits a chain event `driver_selected` carrying `capability`, `candidates` (the full ready+eligible pool with cost tiers), `chosen_driver`, `chosen_model`, `chosen_cost_tier`, `reason` (cheapest qualifying / allowlist forced / no other ready). Operators can replay this to verify cost-control intent over a window.

**Independent test:** Run 5 dispatches. Assert `~/.chitin/events-<run_id>.jsonl` contains 5 `driver_selected` events with the full decision context.

### US5 (P3) — Per-(driver, model) budget caps

> Optional `~/.chitin/driver-budgets.yml` declares monthly caps per driver-model pair (e.g., `claudecode/claude-opus-4-7: 50_usd`). The orchestrator tracks token spend in a SQLite ledger per worktree run, sums by month, and refuses to select a driver-model pair that's over budget. Refusal is loud: the rejected candidate is added to the `driver_selected` event's `excluded_for_budget` list, and the operator sees `chitin-orchestrator budgets` for current spend.

**Independent test:** Cap `codex/gpt-5.5: 0_usd`. Dispatch a code.implement task. `SelectDriver` excludes codex, falls back to next-cheapest. Chain event records the exclusion.

## Functional Requirements

### Card + model binding

- **FR-001** Add `Model` and `CostTier` to `driver.CapabilityCard` (`Model` may already be present; verify). Defaults documented per driver in code.
- **FR-002** Introduce `driver.CostTier` enum: `CostTierFree`, `CostTierSubscriptionPaid`, `CostTierCreditPoolBounded`, `CostTierMetered`. String constants are stable for telemetry replay.
- **FR-003** Every existing driver's `Card()` populates `CostTier` with its conservative default:

| Driver | Default model | Default CostTier |
|---|---|---|
| codex | `gpt-5.5` | subscription-paid |
| openclaw | `glm-5.1` (Ollama Cloud) | subscription-paid |
| hermes | `gpt-5.5` (codex shared) | subscription-paid |
| copilot | `gpt-4.1` (free tier — changes from current default) | free |
| claudecode | `claude-haiku-4-5` (changes from current default) | credit-pool-bounded |
| gemini | (operator-confirmed default) | TBD |
| local | n/a (operator hand-off) | free |

### Per-driver env hooks

- **FR-004** Add `CHITIN_<DRIVER>_MODEL` env var support in `main.go`'s `buildRegistry()` for every driver via the driver's existing `WithModel(...)` option (where present) or a new option added per driver in this spec.
- **FR-005** Document the env-var pattern in `chitin-orchestrator help` output and in `docs/operator/driver-config.md` (new).

### Cost-aware routing

- **FR-006** `driver.Registry.Select(ctx, capability)` returns the **cheapest ready, capability-matched** driver, tie-broken by alphabetical driver_id for determinism. Replaces the current alphabetical-only sort.
- **FR-007** When `CHITIN_DRIVER_ALLOW` is set, the candidate pool is filtered first by the allow set; then cheapest-wins applies within that filtered set. Existing behavior preserved for the empty case.
- **FR-008** A `BlockedUnroutableError` is returned only when no ready driver has the capability (existing semantics unchanged).

### Telemetry

- **FR-009** New chain event type `driver_selected` emitted via the existing kernel emit path. Payload: `capability`, `chosen_driver`, `chosen_model`, `chosen_cost_tier`, `chosen_reason`, `candidates` (array of `{driver, model, cost_tier, ready, excluded_reason?}`).
- **FR-010** Operator surface: `chitin-orchestrator status --drivers` prints the current registry with one row per driver: `id`, `model`, `cost_tier`, `capabilities`, `ready`, `ready_reason`.

### Budget enforcement (US5)

- **FR-011** `~/.chitin/driver-budgets.yml` schema: `{<driver>/<model>: <usd_cap_per_month>}`. Loaded at registry build time; refresh on SIGHUP.
- **FR-012** Spend tracking in `~/.chitin/driver-spend.db` (SQLite). Per-run token consumption attributed at `DeliverWorkProduct` time. Operator sees `chitin-orchestrator budgets` for current month-to-date.
- **FR-013** When a driver-model pair is over budget, `SelectDriver` excludes it from the candidate pool AND emits `driver_selected.excluded_for_budget`. No silent budget bust.

## Success Criteria

- **SC-001** With no env overrides, an `impl` dispatch picks the cheapest driver for `code.implement` across 10 sequential runs — measured by `driver_selected` chain events showing `chosen_cost_tier=subscription-paid` (codex or openclaw), never `metered`.
- **SC-002** `CHITIN_<DRIVER>_MODEL` env hook works for every driver in the registry, verified by `chitin-orchestrator status --drivers` showing the override taking effect.
- **SC-003** Routing determinism: 100 sequential `SelectDriver` calls for the same capability against the same registry return the same `(driver, model)` pair every time (FR-006 tie-break invariant).
- **SC-004** Budget enforcement: a driver-model pair capped to $0 is never selected across a 7-day window. Measured by chain event scan.
- **SC-005** Copilot CLI default model changes from Opus 4.7 to GPT-4.1 (free), reducing operator's monthly Copilot spend to ~$0 unless explicitly overridden.

## Scope

### In scope

- Per-driver env-var model hooks (`CHITIN_<DRIVER>_MODEL`) for all 7 drivers
- `CostTier` enum + Card field + per-driver defaults
- Cheapest-qualifying-first `SelectDriver` algorithm
- `driver_selected` chain event with full decision context
- `chitin-orchestrator status --drivers` operator surface
- Budget config + SQLite ledger + over-budget exclusion (US5)
- Operator runbook `docs/operator/driver-config.md`

### Out of scope

- **Token-level cost computation.** Different backends bill differently (tokens, requests, time). v1 tracks spend at the operator-stated level (budget caps in USD/month) but doesn't attempt to model token cost — that's per-backend math and would be a separate spec.
- **Auto-tuning of model choices.** This spec lets the operator choose; it doesn't suggest. A future spec could auto-pick a lower-tier model when the higher-tier hits N% of budget — out of scope here.
- **Cross-driver fallback chains.** Spec 099 US3 has its own fallback semantics for Copilot-dispatched specs. v1 here is purely selection at dispatch time, not retry-on-failure.
- **Hermes reconfiguration.** Hermes currently shares codex's backend (redundant). Whether to keep / repoint / remove hermes is an operator decision; this spec preserves it as-is.

## Edge Cases

- **Two drivers tie on cost tier:** alphabetical driver_id tie-break (FR-006). Reproducible across runs.
- **All ready drivers excluded for budget:** `BlockedUnroutableError` returned with `reason: all_drivers_over_budget`. Operator sees the explanation in stderr.
- **Operator sets `CHITIN_CODEX_MODEL=` (empty):** treated as unset; driver default applies. (Documented in env var help text.)
- **Operator sets `CHITIN_CODEX_MODEL=nonexistent-model`:** registration succeeds (we don't validate model strings — we don't know the backend's universe). Failure surfaces at first `Invoke` when the codex CLI rejects the model. The codex driver logs the rejection clearly.
- **Budget config file missing:** treated as "no caps" (US5 disabled). No silent failure, no required dependency.

## Assumptions

- Every existing driver either has a `WithModel(...)` option or one can be added trivially (most do; verified for codex, copilot, claudecode in the live demo).
- The kernel chain accepts new event types without schema changes (constitution §1: emit pipeline is stable v2 envelope).
- `SelectDriver` activity replacement is replay-safe: the cost-tier sort is pure given the (registry × env × budget-ledger) state at the moment of the activity execution; Temporal records the activity result, replay uses the recorded result, so cost-tier sort changes between deploys don't break replay.

## Notes for Implementation Phase

**Implementation deferred** — design-only. Recommended sequence:

1. **Phase 1 (foundational):** Add `CostTier` enum + Card field; per-driver defaults.
2. **Phase 2 (US1):** `CHITIN_<DRIVER>_MODEL` env hooks in `buildRegistry()` for all 7 drivers.
3. **Phase 3 (US2):** `chitin-orchestrator status --drivers` operator surface.
4. **Phase 4 (US3):** Cheapest-qualifying-first `Registry.Select` + tie-break + tests.
5. **Phase 5 (US4):** `driver_selected` chain event in `SelectDriver` activity.
6. **Phase 6 (US5):** Budget ledger + over-budget exclusion. Heavier — own PR.
7. **Phase 7 (polish):** Operator runbook + Copilot default model change.

US5 is the largest phase and could ship after US1-4 land. US1-4 are the load-bearing cost-control wins; US5 is the safety net.
