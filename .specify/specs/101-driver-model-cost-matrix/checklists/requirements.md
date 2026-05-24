# Requirements Checklist — 101 Driver × Model × Cost-Tier Matrix

Design-stage verification. Items marked `[x]` were satisfied during spec authoring; the "Deferred to implementation" section enumerates gates the impl PR must satisfy.

## Empirical grounding

- [x] Spec opens with the actual cost-surface inventory the operator pays for (5 billing surfaces, totals)
- [x] Live-demo failure cited as the surfacing moment for this design
- [x] Demonstrates current behavior is alphabetical (not cost-aware) with concrete consequence (claudecode credit burn)

## Scope discipline

- [x] Driver × model × cost-tier as the routable unit, not just driver
- [x] Operator-explicit override (CHITIN_DRIVER_ALLOW already exists) preserved
- [x] Per-driver env hook pattern (CHITIN_<DRIVER>_MODEL) for all 7 drivers
- [x] Conservative default per driver (defaults documented in the table)
- [x] Out-of-scope explicitly: token-level cost computation, auto-tuning, cross-driver fallback chains, hermes reconfiguration

## Routing semantics

- [x] Cheapest-qualifying-first algorithm specified
- [x] Tie-break rule is total-ordered (alphabetical driver_id) — no map-iteration nondeterminism
- [x] Operator override semantics layered cleanly (allow set → cost sort → tie-break)
- [x] BlockedUnroutableError preserved for capability-miss

## Telemetry

- [x] driver_selected chain event with full decision context (FR-009)
- [x] Excluded-for-budget candidates surfaced in the event (FR-013)
- [x] Operator surface: chitin-orchestrator status --drivers (FR-010)

## Composition with existing specs

- [x] Spec 075 (driver contract): extends CapabilityCard, doesn't replace
- [x] Spec 094 (PR review): downstream consumer — review dialectic also gets cost-aware reviewer selection for free
- [x] Spec 099 (Copilot driver): orthogonal — 099 routes to GitHub-native Copilot; 101 routes among local drivers
- [x] Spec 076 (Scheduler): unchanged — selection is per-node activity, not workflow

## Constitution

- [x] §1 kernel-only chain writer: preserved (driver_selected event via existing emit path)
- [x] §6 swarm tooling exception: code lives under `go/orchestrator/`
- [x] §7 swarm is the orchestrator: preserved — selection logic is per-dispatch, no operator hand-off

## Deferred to implementation

1. **CostTier enum exact constants:** decide on naming (free / subscription-paid / credit-pool-bounded / metered vs alternatives). Use string constants stable for telemetry replay.
2. **WithModel option audit:** verify every driver (claudecode, codex, copilot, gemini, hermes, openclaw, local) either has WithModel or one can be added in the same PR.
3. **Copilot CLI free-model verification:** confirm GPT-4.1 is actually free via Copilot CLI (operator-stated; verify in impl PR).
4. **Default-model changes:** Copilot default flips from Opus 4.7 to GPT-4.1; claudecode default flips to Haiku. Both behavioral changes — call out in impl PR body.
5. **Budget ledger schema:** SQLite tables, indexes, query patterns. Probably one table `(driver_id TEXT, model TEXT, month TEXT, usd_spent REAL)` with `(driver_id, model, month)` PK.
6. **Token → USD conversion:** per-backend math, deferred per "Out of scope" but needed for US5 to be meaningful. Could ship US5 with operator-stated per-run cost estimates as a placeholder.
7. **Replay safety test:** SelectDriver activity must return deterministic result given (registry × env × budget-ledger) snapshot. Test by running 100 replays in a sandbox.
8. **`chitin-orchestrator status --drivers` output format:** column order, JSON shape, --text vs default JSON.
9. **Operator runbook docs/operator/driver-config.md:** when to set which env var, cost-tier table, budget-config example, troubleshooting (model rejection, budget exclusion).
10. **Spec 099 cross-reference:** spec 099 US3 has budget-routing-for-Copilot-only language. After 101 lands, that section becomes redundant — fold into 101 or supersede with a note.
