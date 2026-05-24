# Console Reviews Suite — Design

Five wireframes for the chitin-console `/reviews` surface, the operator-facing window into the dialectic gate that spec 104 (`DispatchMachineReviewer`) wires up. Approved as a coordinated **suite**, not a single winning variant — each serves a distinct operator question.

## Background

Spec 094 (PR Review Mechanism) defined the dialectic gate: primaries + arbiter, `StructuredVerdict` contract, closed `FailureKind`s (`FailureMalformedJSON` / `FailureMalformedShape` / `FailureTimeout` / `FailureError`). Spec 104 wired the activity (`go/orchestrator/activities/review/dispatch_machine_reviewer.go`) to actually invoke drivers and translate their `Result.Explanation` into `Outcome.Verdict`.

That work emits verdict data to `~/.chitin/gov-decisions-*.jsonl` and the chain ledger. **The console screens here consume that data.** They do not change spec 104's contracts.

## The five variants

Open `index.html` for a side-by-side comparison board.

| Variant | Layout metaphor | Operator question it answers |
|---|---|---|
| **A** Pipeline DAG (`variant-A.html`) | Horizontal swim lanes per in-flight PR; columns are workflow stages (snapshot → P1 → P2 → arbiter → decision); active stage pulses, stalled rows go amber | "Where is each PR stuck right now?" |
| **B** Dialectic Theater (`variant-B.html`) | Centered PR card + three reviewer "witness stands" with verdicts and rationale quotes; arbiter slot below | "What did the AIs actually say about THIS PR?" |
| **C** Operations Terminal (`variant-C.html`) | Bloomberg-density: 8 KPI tiles, center live event tape, right rail of per-driver tiles with sparklines | "Give me everything, I'll synthesize." |
| **D** Health Funnel (`variant-D.html`) | 5-stage funnel (received → snapshot → dispatched → verdicts → decisions) + grouped "broken right now" failure list | "Is the system healthy, and if not, exactly where?" |
| **E** Activity Stream (`variant-E.html`) | Single 720px-centered column of progressive cards, one per PR review, calm typography, mobile-first | "Let me dip in occasionally and read it like a feed." |

All five share the **chitin palette** locked in `apps/chitin-console/src/styles.css` (`#0A0E15` bg / `#D4A574` orange / Space Grotesk + Space Mono) and **the same mock PR data** (PR #948 awaiting arbiter, PR #919 with a codex `FailureMalformedShape`, PR #946 decided, etc.), so visible differences are presentation, not content.

## Proposed routing for spec 105 (when implemented)

| Route | Variant | Default-or-secondary |
|---|---|---|
| `/reviews` | A — Pipeline DAG | Default landing |
| `/reviews/:prNum` | B — Dialectic Theater | Per-PR drill-down |
| `/reviews/live` | C — Operations Terminal | Power-user firehose |
| `/reviews/health` | D — Health Funnel | Ops health-check |
| `/reviews/feed` | E — Activity Stream | Mobile / passive catch-up |

`/reviews` would slot into the existing primary nav (already present in `apps/chitin-console/src/app/app.html`). Sub-routes would be reachable via secondary nav inside the Reviews surface.

## Data shape (mocked here, real source noted)

| UI element | Real source | Reader |
|---|---|---|
| PR list + verdict + drivers | `~/.chitin/gov-decisions-YYYY-MM-DD.jsonl` | `libs/telemetry` |
| Verdict rationale + blocker count | Spec 104 `StructuredVerdict` rows (in same JSONL via decision payload) | `libs/telemetry` |
| Driver tile (latency / fail-rate / ELO) | `swarm_elo` table + `gov-decisions` aggregations | `libs/telemetry` |
| Stage timing (snapshot, dispatch, verdict, decision) | Chain events `~/.chitin/events-*.jsonl` | `libs/telemetry` (read-only) |
| KPI strip (in-flight, cost-24h, fail-rate, decisions-24h) | Aggregate over `gov-decisions` window | `libs/telemetry` |
| Funnel drop-off counts | Stage-by-stage event counting | `libs/telemetry` |

No new write paths. No new kernel state. Console remains read-only per the architectural hard rule (Go kernel owns writes).

## Status

**Design approved 2026-05-24.** Implementation deferred to a follow-up spec (next free slot: spec 105).

These wireframes are the design artifact for that spec. They should not be deleted when spec 105 ships — they remain the reference for visual intent.

## Notes

- Generated via `/design-shotgun` after the AI image API path was blocked (no OpenAI key configured). Wireframes are hand-coded HTML+CSS that mirror the production palette directly, not AI mockups.
- The source-of-truth copies persist at `~/.gstack/projects/chitinhq-chitin/designs/driver-review-dispatch-20260524/` along with `approved.json` (machine-readable suite-approval record).
- To view: `xdg-open docs/design/2026-05-24-console-reviews-suite/index.html` (or open any individual `variant-*.html`).
