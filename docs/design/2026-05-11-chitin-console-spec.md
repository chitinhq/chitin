# Chitin Console Spec

## Assumptions

1. The first deliverable is local-only and reads the operator's existing
   `.chitin` directory.
2. Hermes remains the operational UI for tickets, dispatch, approvals,
   clarification, and swarm coordination.
3. OpenClaw and Lobster remain dispatch/execution substrates; Chitin only
   observes their governed tool calls.
4. The browser UI, if built, must sit on top of shared read APIs rather than
   parsing JSONL independently.
5. The first milestone is CLI-first. A browser dashboard can come later, built
   on the same telemetry query layer and optional local read API.

Correct these before implementation if any are wrong.

## Objective

Build a Chitin observability surface for heterogeneous agent swarms. The
surface should answer what happened across drivers, agents, sessions, policy
decisions, chain integrity, and routing signals.

The primary user is the local operator running Hermes, OpenClaw, Lobster, and
frontier CLI agents while dogfooding Chitin. Success means the operator can
inspect live and historical governance data without leaving Chitin's boundary
or duplicating Hermes' operational responsibilities.

## Tech Stack

- Go kernel remains the canonical write path for gate evaluation, chain
  emission, governance DB writes, and policy enforcement.
- `libs/contracts` remains the canonical TypeScript schema layer.
- `libs/telemetry` owns low-latency read-side indexing and query APIs over
  canonical JSONL and derived SQLite views.
- `python/analysis` owns deeper batch/offline analysis such as decision
  pattern mining, debt streams, skill mining, predictive scoring,
  floundering calibration, routing ELO, and report generation.
- `apps/cli` exposes the first console commands through Commander.
- A future local browser UI may be added only after the shared read/query API
  is stable enough for both CLI and browser consumers.

## Commands

- Install: `pnpm install`
- CLI dev: `pnpm exec nx run @chitin/cli:run -- --help`
- CLI tests: `pnpm exec nx run @chitin/cli:test`
- Telemetry tests: `pnpm exec nx run @chitin/telemetry:test`
- All TypeScript tests: `pnpm exec vitest run`
- TypeScript lint: `pnpm exec oxlint .` and `pnpm exec eslint .`
- Go kernel build: `pnpm exec nx run execution-kernel:build`
- Go kernel tests: `(cd go/execution-kernel && go test ./...)`

## Project Structure

- `docs/design/2026-05-11-chitin-console-spec.md` documents the product and
  technical boundary.
- `libs/telemetry/src/` owns reusable query functions for decisions,
  sessions, agents, signals, and chain status.
- `libs/telemetry/tests/` covers query behavior using fixture `.chitin`
  directories.
- `python/analysis/` remains the batch/deeper-insight layer for report-style
  analytics and model/calibration work.
- `apps/cli/src/commands/` exposes `chitin inspect ...` commands backed by
  `libs/telemetry`.
- `apps/cli/tests/` covers command output, filters, and error handling.
- Future browser code, if approved, should live under a new app only after
  CLI/query APIs prove the shape.

## Console Surface

### Milestone 1: Source Map and Shared Telemetry Queries

Move reusable reads into `libs/telemetry` before adding new console commands:

- decisions query with filters and pagination
- session timeline query with chain verification metadata
- agent summary query
- denial summary query
- rule-hit summary query
- signal extraction from `router.signal` rows
- serializable response shapes that can later back a local API

The CLI should use `libs/telemetry` for fast interactive inspection. It may
surface artifacts produced by `python/analysis`, but it should not port
analysis algorithms into TypeScript unless they are needed for interactive
read latency or browser/API consumption.

#### Source Map

| Console concern | Primary source | Reader layer | Notes |
|---|---|---|---|
| Event/session timeline | `.chitin/events-*.jsonl` materialized to `.chitin/events.db` | `libs/telemetry` | JSONL is canonical; `events.db` is a rebuildable interactive index. |
| Chain continuity | `.chitin/events-*.jsonl` plus event `seq`/`prev_hash`/`this_hash` fields | `libs/telemetry` | Verify from event rows for inspect output; `chain_index.sqlite` is kernel tail bookkeeping, not the audit source. |
| Current chain tail | `.chitin/chain_index.sqlite` | Go kernel | Useful for emit-time invariants; avoid making it the console's historical source. |
| Gate decisions | `.chitin/gov-decisions-YYYY-MM-DD.jsonl` | `libs/telemetry` for interactive reads; `python/analysis` for batch reads | Daily JSONL is the audit log for allow/deny/mode/rule/reason/escalation. |
| Router signals | `router.signal` rows in `gov-decisions-YYYY-MM-DD.jsonl` and decision payloads in event chain | `libs/telemetry` | Prefer decision JSONL for inspect summaries because router scores are stamped there as `predicted_blast`, `floundering_score`, `drift_score`, and `routing_decision`. |
| Agent denial totals and lockdown | `.chitin/gov.db` tables `denials` and `agent_state` | `libs/telemetry` read-only SQLite query | Kernel-owned state; console must never mutate it. |
| Cost/envelope state | `.chitin/gov.db` envelope tables | `libs/telemetry` read-only SQLite query | Include only when an inspect view needs tier/cost context beyond decision rows. |
| Decision pattern mining | `.chitin/gov-decisions-*.jsonl` plus `python/analysis/out/decisions-*` | `python/analysis` | Surface generated artifacts or summaries; do not port pattern detection into CLI by default. |
| Skill/debt/prediction/routing reports | `python/analysis/out/*` and analysis-specific stores such as `fingerprint-outcomes.sqlite` | `python/analysis` | Batch/deeper insight layer; CLI can point at latest artifacts or render summaries. |

### Milestone 2: CLI Inspect Commands

Add CLI commands that read from local telemetry state:

- `chitin inspect live`
  - Shows recent decisions across all drivers.
  - Supports `--limit <n>`, `--driver <name>`, `--agent <id>`,
    `--decision <allow|deny>`, and `--since <duration>`.
- `chitin inspect session <chain-id>`
  - Shows an ordered session timeline with action, target summary, rule,
    decision, escalation, signal rows, and hash continuity status.
- `chitin inspect agent <agent-id>`
  - Shows deny counts, most-hit rules, recent sessions, blast/floundering/drift
    trends when present, and current lockdown/severity state when indexed.
- `chitin inspect denials`
  - Shows denied actions with rule, reason, suggestion, driver, agent,
    session, and timestamp.
- `chitin inspect rules`
  - Shows most-hit rules and recent examples for each rule.

CLI output is the first product surface. It should be good enough for dogfood
operations before a browser dashboard exists.

### Milestone 3: Optional Local Read API

After the CLI proves the read model, expose the same telemetry queries through
a local read-only API if the browser dashboard needs process separation.

Candidate command:

- `chitin inspect serve --host 127.0.0.1 --port <port>`

Constraints:

- Bind to loopback by default.
- Read-only endpoints only.
- No dispatch, ticket mutation, approval prompting, or policy mutation.
- No background daemon unless explicitly approved.
- Reuse `libs/telemetry` response types exactly.
- Expose analysis artifacts as files or report summaries when useful; do not
  make the API responsible for running long batch jobs by default.

Decision for this implementation slice: defer `chitin inspect serve`. The CLI
now exercises the shared telemetry read model directly; adding a local HTTP
surface should wait until browser work begins or another consumer needs
process-separated access.

### Milestone 4: Local Browser Console

Only after Milestones 1-3 are stable, consider a browser app that consumes the
same query API through a local process. Candidate views:

- live decision stream
- session timeline
- agent risk profile
- policy explainability
- swarm overview
- replay and audit export

## Code Style

Prefer small query functions with explicit filter objects and serializable
return values:

```ts
export interface DecisionFilters {
  readonly driver?: string;
  readonly agent?: string;
  readonly decision?: 'allow' | 'deny' | 'monitor';
  readonly since?: Date;
  readonly limit?: number;
}

export async function listDecisions(
  chitinDir: string,
  filters: DecisionFilters = {},
): Promise<DecisionRow[]> {
  // Open derived indexes through telemetry helpers; do not parse from CLI.
}
```

CLI commands should format results only. They should not own indexing,
normalization, policy interpretation, analysis algorithms, or chain
verification logic.

## Testing Strategy

- Unit-test telemetry queries against temporary `.chitin` fixtures.
- Keep existing `python/analysis` tests authoritative for batch analysis
  behavior.
- CLI-test command behavior with fixture directories and deterministic output.
- Include at least one fixture with:
  - allowed and denied decisions
  - multiple drivers
  - a `router.signal` row
  - linked chain hashes
  - one intentionally broken chain for verification output
- Browser UI tests are out of scope until the browser milestone is approved.

## Boundaries

Always:

- Treat canonical JSONL and kernel-owned DB files as source data, not UI-owned
  mutable state.
- Keep write authority and policy evaluation in the Go kernel.
- Keep shared read logic in `libs/telemetry`.
- Keep deeper/batch analysis logic in `python/analysis`.
- Make command output useful in plain terminals and logs.
- Preserve local-only operation unless a future decision explicitly changes
  the product boundary.
- Keep any API endpoint read-only and loopback-bound by default.

Ask first:

- Adding a new app for browser UI.
- Adding runtime dependencies beyond current CLI/telemetry needs.
- Porting Python analysis algorithms into TypeScript.
- Changing event, decision, or governance DB schemas.
- Writing back to Hermes, OpenClaw, Lobster, GitHub, or Discord.
- Adding long-running daemons or background services.
- Binding any API to a non-loopback interface.

Never:

- Dispatch agents, schedule work, manage kanban, or mutate tickets.
- Prompt for operator approvals or implement approval persistence.
- Route models or select frontier agents.
- Spawn or consult an LLM from the kernel hot path.
- Run long batch analysis jobs implicitly from lightweight inspect commands.
- Make Chitin the source of truth for Hermes/OpenClaw operational state.

## Success Criteria

- The operator can answer "what just happened?" from Chitin data with one
  command.
- The operator can inspect one session by chain id and see tool-call order,
  decisions, rules, signals, and chain health.
- The operator can identify noisy or risky agents from Chitin-derived data.
- The CLI and any future browser UI use the same telemetry read model.
- The console reuses `python/analysis` outputs for deeper insights instead of
  duplicating that layer.
- Any API surface is read-only and backed by the same telemetry read model.
- No milestone duplicates Hermes dispatch, ticketing, approval, or
  clarification flows.

## Implementation Plan

1. Inventory current read paths.
   - Confirm how `events.db`, `gov-decisions-YYYY-MM-DD.jsonl`,
     `gov.db`, `chain_index.sqlite`, and `python/analysis/out/*` overlap.
   - Decide which source each console query should use for Milestone 1 without
     changing kernel-owned schemas.
   - Mark each view as interactive telemetry, batch analysis artifact, or a
     composition of both.
2. Add telemetry query primitives.
   - Implement typed functions in `libs/telemetry` for decisions, sessions,
     agents, denials, rules, and signals.
   - Prefer existing derived SQLite indexes where sufficient; parse decision
     JSONL only where no derived read model exists yet.
3. Add focused fixtures and tests.
   - Build temporary `.chitin` fixtures with mixed drivers, allow/deny rows,
     router signals, and chain continuity cases.
   - Test query filters and stable response shapes before wiring CLI output.
4. Add `chitin inspect` CLI group.
   - Register commands in `apps/cli/src/main.ts`.
   - Keep command code limited to option parsing and formatting.
   - Include a machine-readable output flag only if needed for Hermes/OpenClaw
     handoff; otherwise keep initial output human-readable.
5. Evaluate local API need.
   - If browser work starts, add a read-only loopback API that calls the same
     telemetry functions.
   - Decide whether the API should expose generated analysis artifacts as
     static/read-only resources.
   - Do not add the API just to serve the CLI.
6. Build browser dashboard later.
   - Use the local API/read model after the CLI has proven the data shape.
   - Keep browser scope observability-only.

## Task Breakdown

- [x] Task: map current telemetry sources and choose source-of-truth per
      inspect view.
  - Acceptance: spec has a short source map for events, decisions, signals,
    chain health, agent state, and Python analysis artifacts.
  - Verify: review against existing `libs/telemetry`, `python/analysis`, and
    Go governance files.
  - Files: `docs/design/2026-05-11-chitin-console-spec.md`.
- [x] Task: add telemetry decision/session query types.
  - Acceptance: `libs/telemetry` exports typed query functions with filters
    and serializable rows.
  - Verify: `pnpm exec nx run @chitin/telemetry:test`.
  - Files: `libs/telemetry/src/*`, `libs/telemetry/tests/*`.
- [x] Task: add `chitin inspect live` and `chitin inspect denials`.
  - Acceptance: commands render deterministic recent decision output from
    fixtures.
  - Verify: `pnpm exec nx run @chitin/cli:test`.
  - Files: `apps/cli/src/main.ts`, `apps/cli/src/commands/*`,
    `apps/cli/tests/*`.
- [x] Task: add `chitin inspect session`.
  - Acceptance: command shows ordered events, governance decisions, signal
    rows, and chain health for a chain id.
  - Verify: telemetry and CLI tests pass.
  - Files: `libs/telemetry/src/*`, `apps/cli/src/commands/*`,
    relevant tests.
- [x] Task: add `chitin inspect agent` and `chitin inspect rules`.
  - Acceptance: commands summarize recent risk/rule patterns without mutating
    governance state.
  - Verify: telemetry and CLI tests pass.
  - Files: `libs/telemetry/src/*`, `apps/cli/src/commands/*`,
    relevant tests.
- [x] Task: decide whether to add `chitin inspect serve`.
  - Acceptance: decision is documented after CLI dogfood, including whether a
    browser dashboard needs process-separated access.
  - Verify: spec update reviewed before implementation.
  - Files: this spec and, if approved later, API/browser files.

## Open Questions

1. Should `chitin inspect live` be a one-shot recent view first, with
   streaming `--follow` deferred until the static output is useful?
2. What identifier should be the primary join back to Hermes kanban work:
   `workflow_id`, `chain_id`, `agent_instance_id`, or a separate ticket id
   field?
3. Should Chitin expose read-only Hermes/OpenClaw reference links when present,
   or should cross-system navigation remain outside Chitin for now?
4. Which Python analysis outputs are worth surfacing in `chitin inspect` first:
   decisions, debt, skill mining, prediction, routing ELO, or floundering
   calibration?
