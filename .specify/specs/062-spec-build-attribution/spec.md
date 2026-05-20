# 062 — Spec-build attribution

> Every kernel chain event and every Sentinel telemetry row carries the
> `(spec_id, build_id)` it belongs to. Without it there is no per-spec
> replay (063) and no per-spec learning (064).

**Status**: draft — grooming complete, Q1 and Q2 resolved below.

**Construction order**: 062 is SECOND. Blocked until spec 061 lands
(`spec_id` comes from `UnifiedSpec.spec_id`). Slice 1 can proceed
against a provisional `spec_id` type/constant; the actual ingestion
pipeline depends on 061.

## Ticket refs

- Workspace chitin parent task `t_0291fcfc` — root groom + implement.
- Slice 1 implementation: `t_9dca96c4`.
- Slice 2 implementation: `t_a147ea70`.

## Definitions

| Term | Meaning |
|------|---------|
| **spec_id** | Stable identifier for a ratified spec. Defined by spec 061 (`UnifiedSpec.spec_id`). Shape: `NNN-<slug>` (e.g., `062-spec-build-attribution`). |
| **build_id** | Stable identifier for one execution of a spec run. Minted once at dispatch; inherited by every downstream event in that run. |
| **build** | One end-to-end execution of a spec — from dispatcher claim to worker completion. A spec can have many builds over time. |
| **outermost dispatcher** | The subsystem that claims a spec-bearing ticket and spawns a worker. Currently hermes kanban (`scripts/kanban-flow dispatch`) for chitin-worker, and `mini_open` for Mini MCP sessions. |

## Resolved questions

### Q1 — build_id minting point

**Decision**: The outermost dispatcher mints the `build_id`.

**Rationale**:

1. **Single minting invariant.** Every downstream consumer — the chitin-kernel gate, hermes tool calls, chain events, Sentinel rows — must inherit the same `build_id`. If any intermediate layer mints its own, the chain fragments. The outermost dispatcher is the earliest point that knows both the `spec_id` and the execution lifecycle start.

2. **Dispatcher is the authority on run lifecycle.** The dispatcher knows when a run begins (claim + spawn) and when it ends (completion/failure/timeout). It is therefore the natural owner of `build:start` and `build:end` lifecycle events. Minting at a lower layer (e.g., inside the kernel) would give every gate invocation a different `build_id`, destroying the grouping invariant.

3. **Multiple dispatch paths share the same contract.** There are two live paths today:
   - **Hermes kanban dispatcher** — `scripts/kanban-flow dispatch` claims a ticket, resolves the spec reference, and spawns a worker. It sets `HERMES_KANBAN_TASK`, `HERMES_KANBAN_WORKSPACE`, etc. The `build_id` joins these as a dispatcher-set env var.
   - **Mini MCP dispatch** — `services/mini-mcp/server.py` resolves spec references and spawns a session. The `goal_id` derivation in spec 051 parallels `build_id` derivation here.
   
   Both paths must mint the same `build_id` shape. The spec defines the format; each dispatcher implements minting in its own layer.

4. **Worker inheritance, not creation.** Workers NEVER mint a `build_id`. They receive it from the dispatcher via an environment variable (`CHITIN_BUILD_ID`) and propagate it into every chitin-kernel gate invocation and chain event. If `CHITIN_BUILD_ID` is unset (e.g., manual `chitin-kernel gate evaluate` from the CLI), the event records `build_id: null` — this is correct and expected (operator ad-hoc invocations are not builds).

**Minting sites (concrete)**:

| Dispatcher | Minting site | Mechanism |
|------------|-------------|-----------|
| Hermes kanban | `scripts/kanban-flow dispatch` | Shell function `mint_build_id` generates the id, exports `CHITIN_BUILD_ID` into the worker's env |
| Mini MCP | `services/mini-mcp/server.py` | Python function `mint_build_id` generates the id, passes via kitty `--var` or env to the session |

**Inheritance chain**:

```
dispatcher mints build_id
  → sets CHITIN_BUILD_ID env var
    → worker process inherits
      → chitin-kernel gate evaluate reads CHITIN_BUILD_ID from env
        → stamped on every Decision → stamped on every chain Event
      → hermes governance plugin passes build_id to gate invocation
      → Sentinel telemetry rows include build_id column
```

### Q2 — build_id shape

**Decision**: Structured identifier encoding spec_id and timestamp, with a
random uniqueness suffix.

**Shape**:

```
build-<spec_id>-<timestamp>-<nonce>
```

Where:
- `spec_id` — the `NNN-<slug>` from `UnifiedSpec.spec_id` (e.g.,
  `062-spec-build-attribution`). Dashes in the slug are preserved.
- `timestamp` — UTC unix seconds at mint time, encoded as base36
  (0-9a-z), zero-padded to 8 chars. Base36 keeps the id compact while
  remaining lexicographically orderable for timestamps within the same
  spec. Example: `1716134400` → base36 `1l3jhx4`.
- `nonce` — 4 hex chars from `crypto/rand` (16 bits of entropy).
  Collision probability within the same second: 1/65536. Two
  dispatches of the same spec in the same second is already an edge
  case; the nonce makes it practically impossible.

**Full example**:

```
build-062-spec-build-attribution-1l3jhx4-a3f7
```

**Why structured, not bare UUID**:

1. **Human readability.** Operators scanning chain logs, Discord event feeds, or Sentinel dashboards can immediately identify which spec and roughly when a build occurred. A bare UUID tells you nothing.
2. **Deterministic grouping for replay (063).** The `by-spec` query `SELECT * WHERE spec_id = ?` works on structured ids. With bare UUIDs, every replay requires a join or a separate index.
3. **Greppable.** `grep -r "build-062-" ~/.chitin/` finds every event for spec 062. No secondary index needed for ad-hoc investigation.
4. **Learning correlation (064).** The `by-build` query for learning joins on `build_id`, and the `by-spec` query for cross-build comparison uses the `spec_id` prefix. Both are natural operations on the structured id.

**Determinism for replay**:

Replay (spec 063) will replay events belonging to a specific `build_id`. The `build_id` itself is NOT deterministic — it embeds a random nonce. This is intentional:

- Replay operates on a stored `build_id`, not by reconstructing one.
- The `by-spec` query lists all builds for a spec; the replay consumer picks one.
- Deterministic replay means "given build_id X, replay all events with that id" — the id is a lookup key, not a computed hash.

For the learning pipeline (064), cross-build comparison uses the `spec_id` prefix to group builds of the same spec. The timestamp enables chronological ordering without a separate sort key.

**Comparison with goal_id (spec 051)**:

| Dimension | goal_id (051) | build_id (062) |
|-----------|---------------|----------------|
| Scope | Mini session | Spec run (any dispatcher) |
| Format | `spec-NNN-<hash>` | `build-<spec_id>-<ts>-<nonce>` |
| Minting | Mini MCP layer | Outermost dispatcher |
| Uniqueness | Hash collision guard | Timestamp + nonce |
| Lifecycle | Session start → end | Dispatch claim → completion |

The `build_id` is longer but carries more context (full spec_id, not just number) because it must be globally meaningful across dispatch paths, not just within Mini sessions.

## File-system scope

Worker MAY write under:
- `go/execution-kernel/internal/gov/policy.go` — `Decision` struct gains `BuildID` field
- `go/execution-kernel/internal/event/event.go` — `Event` struct gains `BuildID` field
- `go/execution-kernel/cmd/chitin-kernel/` — env var parsing, event emission
- `go/execution-kernel/internal/chain/` — SQLite schema migration for `build_id` column
- `scripts/kanban-flow` — `mint_build_id` function, `CHITIN_BUILD_ID` export
- `libs/contracts/src/fingerprint.ts` — TypeScript contract for build_id shape
- `.specify/specs/062-spec-build-attribution/**` — this spec

Worker MUST NOT write under:
- `services/mini-mcp/` — spec 050 owns Mini dispatch; 062 only defines the contract
- `python/analysis/` — spec 064 owns learning queries; 062 provides the column
- `apps/cli/` — no CLI changes for Slice 1

Any other path under `chitin/` requires a spec amendment before dispatch.

## Goal

Two-layer attribution so that every chain event and telemetry row can be
traced back to the spec run that produced it:

1. **Schema**: add `(spec_id, build_id)` columns to chain events and Sentinel
   telemetry rows.
2. **Minting + lifecycle**: the outermost dispatcher mints `build_id` and emits
   `build:start` / `build:end` events; all downstream events inherit it.

Without this, per-spec replay (063) and per-spec learning (064) have no
grouping key — every build's events are mixed into the same undifferentiated
chain.

## Slices

### Slice 1 — Schema columns + minting + migration

**Scope**: Data layer. No lifecycle events yet.

1. **Decision struct**: add `BuildID string \`json:"build_id,omitempty"\`` to
   `gov.Decision` in `go/execution-kernel/internal/gov/policy.go`.
2. **Event struct**: add `BuildID string \`json:"build_id,omitempty"\`` to
   `event.Event` in `go/execution-kernel/internal/event/event.go`.
3. **Env var**: `chitin-kernel gate evaluate` reads `CHITIN_BUILD_ID` from
   environment. When present and non-empty, stamps it on every Decision
   and Event. When absent/empty, `build_id` is omitted (backward compat
   with operator ad-hoc invocations).
4. **Decision emission**: `buildDecisionEvent` in `gate_emit.go` propagates
   the `BuildID` from the Decision into the Event payload and the Event
   struct's `BuildID` field.
5. **SQLite migration**: add `build_id TEXT` column to `chain_index.sqlite`
   events table. Existing rows get `NULL` (legacy sentinel). Add index on
   `(build_id)` and `(spec_id)` if not already present.
6. **Minting function**: add `mint_build_id(spec_id string) string` to
   `scripts/kanban-flow`. Called during `dispatch`, export as
   `CHITIN_BUILD_ID` to the worker environment. Shape: `build-<spec_id>-<ts36>-<nonce4>`.
7. **Hermes plugin**: `chitin-governance` plugin passes `CHITIN_BUILD_ID`
   from env to `chitin-kernel gate evaluate --build-id` flag (analogous to
   `--agent`, `--session-id`).

**Tests (unit)**:
- `TestMintBuildIdFormat`: mint produces id matching `^build-.+-.+-[0-9a-f]{4}$`
- `TestMintBuildIdUniqueness`: two calls produce different nonces
- `TestBuildIdFromEnv`: gate evaluate with `CHITIN_BUILD_ID` set stamps it on Decision
- `TestBuildIdAbsent`: gate evaluate without env var produces Decision with empty BuildID
- `TestMigrationAddsColumn`: migration adds `build_id` column, existing rows have NULL
- `TestBuildIdInEventPayload`: full round-trip — env var → Decision → Event has build_id

**AC for Slice 1**:
- AC1: `mint_build_id("062-spec-build-attribution")` produces an id matching `^build-062-spec-build-attribution-[0-9a-z]{8}-[0-9a-f]{4}$`
- AC2: `chitin-kernel gate evaluate` with `CHITIN_BUILD_ID=build-062-...-...` stamps it on every Decision and Event
- AC3: Without `CHITIN_BUILD_ID`, `build_id` field is omitted (not empty string) in JSON — backward compat
- AC4: SQLite migration adds `build_id` column; existing rows have NULL; new rows have the minted id
- AC5: `chitin-governance` plugin passes `CHITIN_BUILD_ID` from env to gate when set

### Slice 2 — Build lifecycle events + by-build/by-spec queries

**Scope**: Event and query layer. Depends on Slice 1.

1. **Lifecycle events**: the dispatcher emits two canonical chain events
   per build:
   - `build:start` — minted alongside the `build_id` itself, before worker spawn. Payload: `{spec_id, build_id, dispatcher, profile, workspace}`.
   - `build:end` — emitted by the dispatcher after worker completion (success, failure, or timeout). Payload: `{spec_id, build_id, outcome, duration_seconds}`.
   Both events are written to the same JSONL file as all other chain events
   and materialized into `chain_index.sqlite`.

2. **by-build query**: `chitin-kernel chain replay --build-id <id>` returns all chain events whose `build_id` matches, in chronological order. This is the foundation for spec 063.

3. **by-spec query**: `chitin-kernel chain replay --spec-id <id>` returns all builds (distinct `build_id` + metadata) for that spec, ordered by timestamp. This is the foundation for spec 064.

4. **Kanban integration**: `scripts/kanban-flow dispatch` emits `build:start` before spawning the worker and `build:end` after the worker process exits. The `post_cost_summary_comment` function gains access to `build_id` for comment attribution.

**Tests (integration)**:
- `TestBuildLifecycleEvents`: dispatch a ticket → verify `build:start` and `build:end` events in chain JSONL with correct `build_id`
- `TestByBuildQuery`: `chitin-kernel chain replay --build-id X` returns all events for build X
- `TestBySpecQuery`: `chitin-kernel chain replay --spec-id 062` returns list of builds for spec 062
- `TestBuildEndOnTimeout`: worker timeout produces `build:end` with `outcome: "timed_out"`

**AC for Slice 2**:
- AC6: Every completed dispatch has exactly one `build:start` and one `build:end` event in the chain
- AC7: `--build-id` query returns complete, ordered event set for that build
- AC8: `--spec-id` query returns all builds for that spec, ordered chronologically
- AC9: `build:end` includes `outcome` field with value `completed`, `failed`, or `timed_out`

## Invariants

1. **Single-mint invariant**: A `build_id` is minted exactly once, by the outermost dispatcher. No downstream component creates a new `build_id`.
2. **Inheritance invariant**: Every chitin-kernel gate invocation inside a build inherits the same `build_id` from `CHITIN_BUILD_ID`. Missing env var → `build_id: null` (backward compat).
3. **Lifecycle bracket invariant**: Every build has exactly one `build:start` and one `build:end`. No orphan starts, no missing ends.
4. **Schema backward compat**: Existing rows without `build_id` have NULL. Queries that don't filter on `build_id` return all rows. No data loss, no migration rollback needed.

## Out of scope

- **Spec 061** (UnifiedSpec and spec_id): defined there, consumed here. 062 assumes `spec_id` exists.
- **Spec 063** (per-spec replay): consumes `by-build` query from Slice 2.
- **Spec 064** (per-spec learning): consumes `by-spec` query from Slice 2.
- **Mini MCP dispatch integration**: defined here (contract), implemented in the spec 050 codebase.
- **Sentinel telemetry row changes**: follow the same schema pattern as chain events but are in a separate codebase (Sentinel). Slice 1 covers chain events; Sentinel adoption is a follow-up.
- **Build cancellation events**: not in scope. `build:end` covers the `timed_out` outcome; explicit cancellation is a future extension.

## Test coverage

Per chitin spec 020 §1.2, e2e is the default. However, Slice 1 is pure schema + unit logic (struct fields, env var reading, minting function) that can be thoroughly tested at the unit level. Slice 2 adds integration concerns (lifecycle event emission, CLI query commands).

| AC | Test layer | Test name | Justification |
|----|-----------|-----------|---------------|
| AC1 | unit | TestMintBuildIdFormat | Pure function, deterministic |
| AC2 | unit | TestBuildIdFromEnv | Env var → Decision stamp, no kernel needed |
| AC3 | unit | TestBuildIdAbsent | Omission is the backward-compat case |
| AC4 | integration | TestMigrationAddsColumn | SQLite migration test with temp db |
| AC5 | unit | TestPluginPassesBuildId | Plugin reads env → passes flag |
| AC6 | integration | TestBuildLifecycleEvents | Requires dispatch + chain JSONL |
| AC7 | integration | TestByBuildQuery | Requires chain writes + SQLite index |
| AC8 | integration | TestBySpecQuery | Requires multiple builds + query |
| AC9 | integration | TestBuildEndOnTimeout | Requires dispatch timeout + event read |