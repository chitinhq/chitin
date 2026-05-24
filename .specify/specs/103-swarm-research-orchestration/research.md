# Spec 103 — Phase 0 Research

## R1 — SQLite driver choice

**Decision:** `modernc.org/sqlite` v1.49.1 (already in `go/orchestrator/go.mod`).

**Rationale:**
- Pure-Go, no cgo dependency — keeps `chitin-orchestrator` build cgo-free, matches existing project posture
- Already a transitive dependency; using it costs zero added supply chain
- Sufficient for swarm-results.db scale (single-writer, low QPS, <1 GB lifetime)

**Alternatives considered:**
- `mattn/go-sqlite3` — cgo, faster, but cgo on this codebase would require cross-compilation tooling changes. Rejected.

## R2 — Temporal Schedules reuse vs new pattern

**Decision:** Build on the existing `EnsureSchedules` pattern at `go/orchestrator/schedules/schedules.go:126`. Add a parallel `EnsureSwarmSchedules(ctx, c, entries)` function that takes operator-config-derived entries (vs the static `Registry()` for spec 081's cron migration).

**Rationale:**
- The spec 081 pattern is the established convention — same `client.ScheduleClient().Create()` call, same `isAlreadyExists` handling, same `SCHEDULE_OVERLAP_POLICY_SKIP`.
- New function (not generalized) because the input shape differs: swarm schedules come from `swarm-schedule.yml` (operator-managed), spec 081 schedules come from a static registry (engineer-managed). The shapes will diverge as swarm gets `tag`, `skills`, `message`, `gateway_session` fields.

**Implementation note:** Swarm Schedule IDs are namespaced — prefix `swarm-` to avoid collision with spec 081's IDs.

**Alternatives considered:**
- Generalize `EnsureSchedules` to take an `[]JobSpec` argument — over-eager refactor; spec 081's Registry caller is on every existing test and would all need updating. Rejected.

## R3 — Hermes MCP stdio client

**Decision:** Write a small in-process MCP stdio client at `go/orchestrator/internal/gateway/hermes_mcp.go`. JSON-RPC over child process stdin/stdout. No external SDK.

**Rationale:**
- No existing Go MCP client in the codebase (verified by grep)
- MCP stdio is small (~few hundred LOC); pulling in a third-party SDK is more risk than it's worth
- Owning the client lets us control timeout, env-pinning (`HERMES_HOME=/home/red/.hermes`), and the security-boundary invariant in one place

**Implementation note:** the client wraps `exec.Command("hermes", "mcp", "serve")` with attached stdio, sends `messages_send` then optionally `events_wait`, parses replies. SIGTERM + 5s grace + SIGKILL on activity timeout to avoid leaked subprocesses (FR edge case: "`hermes mcp serve` subprocess hangs").

**Alternatives considered:**
- `github.com/mark3labs/mcp-go` or similar — pulls a network surface that's irrelevant here (we only need stdio). Rejected.
- Shell out to a separate `hermes` CLI that wraps MCP — adds another binary to the dispatch chain. Rejected.

## R4 — OpenClaw CLI adapter

**Decision:** Thin `exec.CommandContext("openclaw", "sessions", "send", "--session", s, "--message", m)` wrapper. Capture stdout/stderr, exit code. Activity returns the captured streams as part of the workflow result.

**Rationale:**
- OpenClaw CLI is stable and on PATH per spec assumptions
- Subprocess naturally routes through kernel PreToolUse hooks for §1 compliance
- Output capture lets the workflow's chain event record both the message sent and the agent's immediate response (if any)

**Implementation note:** `--session` value comes from the schedule entry's `gateway_session` field (FR-001) or — if omitted — from a `gateway_session_resolver` that runs `openclaw sessions list` and picks the matching session for the agent.

## R5 — fsnotify-based vault ingestion

**Decision:** Use `github.com/fsnotify/fsnotify` (already in `go.mod` as indirect; add direct require). Watch each `ingestion-sources.yml` entry's `root` recursively if `watch: true`. Debounce via per-file 5s timer (FR-012).

**Rationale:**
- fsnotify is the canonical Linux inotify wrapper for Go
- Debounce solves the Obsidian-autosave-churn edge case in the spec
- Per-source watch (not single global watcher) makes config reloads cleaner — adding/removing a source = registering/unregistering a watcher

**Alternatives considered:**
- Polling with `os.Stat` mtime — burns CPU on large vaults. Rejected.
- One global watcher with path filtering at the consumer — simpler but blocks the "remove a source = remove the watcher" affordance. Rejected.

**Edge cases the implementation must handle:**
- IN_DELETE: update queue row's status to `source_deleted`, do NOT delete the row
- IN_MOVED_FROM / IN_MOVED_TO: treat as delete + create
- Watcher EOF on directory rename: re-register the watcher with the new path
- fsnotify channel overflow (default 4096 buffered events): drop with a chain warning, log per-source overflow counter

## R6 — Frontmatter parser

**Decision:** `gopkg.in/yaml.v3` (already in `go.mod` indirect; add direct require).

**Rationale:** Same parser used elsewhere; YAML 1.2 covers the frontmatter shape needed (key: value scalars, lists of strings, dates).

**Implementation note:** delimiter is `---\n` at file start; tolerate missing frontmatter (write a row with `frontmatter_json` = `null`).

## R7 — Chain-mining output routing

**Decision:** Convention-based. The agent writes to `~/.chitin/sentinel-findings/swarm-mined-*.json`. Sentinel's existing watcher (out of scope to modify; spec 080) is told to pick up that glob. Chitin does NOT mediate the write.

**Rationale:**
- The spec is explicit (FR-015): the agent's mining output ROUTE is the agent's choice
- Coupling chitin to the write path would require the agent to call back into chitin instead of writing a file
- The convention path is operator-runbook-documented; sentinel's watcher follows the convention

**Implementation note:** The first impl PR (PR-B) extends sentinel's watcher with the new glob. If sentinel's watcher is not extensible enough, a small `swarm-mined-watcher` activity inside `IngestionWorkflow` handles it; sentinel reads from the same SQLite store.

## R8 — Queue dedup + correlation

**Decision:**
- **Dedup:** by `(source, file_path)`. fsnotify can deliver duplicate IN_MODIFY in burst; debounce + UPSERT on `(source, file_path)`.
- **Correlation field `triggered_by_chain_event`:** populated by joining queue inserts that happened within `cadence_window` of a `swarm_invocation` event for the same agent. Heuristic — first cut uses 4× cadence (e.g., a 6h-cadence schedule wins a finding that lands within 24h). Documented as best-effort in spec.md.

**Rationale:**
- UPSERT keeps re-ingestion idempotent (operator edits the source file → row updates in place, status preserved unless explicitly transitioned).
- The correlation heuristic is intentionally simple for v1; spec 078 (auto-spec-authoring) is the consumer that needs the correlation.

## R9 — `swarm-summary` aggregation

**Decision:** Pure chain replay. The CLI subcommand:
1. Loads chain events from `~/.chitin/events-*.jsonl` within `--days N` (default 7)
2. Filters by `--agent`, `--tag`
3. Groups by `(agent, tag, day)`; counts `swarm_invocation`, distinct tool-call action_targets, error-event count
4. Renders a table

**Rationale:**
- No new storage; chain IS the source of truth (constitution §1)
- Replay is cheap at current scale (~5k events on host); pagination unnecessary for v1
- Output stays operator-friendly (table); machine-readable JSON via `--json` flag for sentinel consumption

**Implementation note:** the existing chain replay infrastructure lives in `go/execution-kernel/internal/event/`. Either link directly or shell out to `chitin-kernel chain query`. Prefer direct link for performance — verify package boundary is clean (no cycle).

## R10 — Testing strategy

**Decision:** Three test layers, mirroring spec 098 / 099 patterns:

1. **Unit:** mock `gh`, `openclaw`, `hermes` binaries on PATH via test-injected dirs. Assert constructed argv + captured output. Use `t.TempDir()` for SQLite + vault fixtures.
2. **Integration (`test/swarm_e2e_test.go`):** spin a temporal-dev test server, register `SwarmInvocationWorkflow` + `IngestionWorkflow`, drive a schedule fire, assert chain events + queue rows land.
3. **Contract:** validate the YAML config parsers against `contracts/swarm-schedule-config.md` and `contracts/ingestion-sources-config.md` schemas. Validate chain event payloads against `contracts/chain-events.md`. Validate queue DDL against `contracts/queue-schema.md`.

**Rationale:** matches established patterns; reuses existing temporal-dev fixture; allows independent slicing of PR-A / PR-B / PR-C.

## Resolved NEEDS CLARIFICATION

All technical-context unknowns from plan.md resolved above. No remaining `NEEDS CLARIFICATION` markers.
