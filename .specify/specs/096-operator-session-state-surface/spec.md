# Feature Specification: Operator session-state surface

**Feature Branch**: `feat/096-operator-session-state-surface`

**Created**: 2026-05-23

**Status**: Draft

**Input**: User description: "Spin out the kernel-side CLI + schema additions originally drafted inside spec 091's v1.1 amendment into their own spec. The new surface lets an operator clear a locked agent without losing audit history (today's `Reset()` deletes the row entirely) and exposes that state to in-process plugins so they can recover sticky stop-hook flags without restarting their host process. Consumer-agnostic — spec 091 v1.1 is the first consumer; any future plugin (hermes, future agents) can use the same surface."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Operator unlocks a locked agent and the audit trail survives (Priority: P1)

An operator observes via Discord (or via `chitin-kernel session status`) that the `clawta` agent is locked. They've fixed the underlying issue — e.g. relaxed a chitin.yaml rule, or determined the lockdown was a false positive — and want the agent to resume on its next tool call. They run `chitin-kernel session unlock -agent clawta -reason "policy relaxed in PR #999"`. The kernel marks the agent unlocked, records the unlock in the chain (with reason text and timestamp), and the next time clawta attempts a tool call, the gate evaluates normally. The denial counters and prior lockdown audit trail in `gov.db` are **preserved** — only the `locked` flag and an `unlock_ts` are written. The operator can later `chitin-kernel session status -agent clawta` and see: "currently unlocked; last locked at T1; unlocked at T2 by reason X; lifetime denial total: N".

**Why this priority**: this is the spec's reason to exist. The current `Counter.Reset()` method (the only existing kernel-side path to clear a lock) deletes the entire `agent_state` row plus all `denials` and `denial_events` for that agent, which erases the forensic history of how the lock occurred. An operator unlocking after a false-positive lockdown shouldn't have to choose between "keep the agent stuck" and "wipe the audit trail".

**Independent Test**: with `clawta` in lockdown (`locked=1` in `gov.db`), run the new `chitin-kernel session unlock` subcommand; verify (a) `locked=0`, (b) `unlock_ts` is populated, (c) denial counters are unchanged, (d) one `session_unlocked` chain event exists with the operator-supplied reason, (e) a fresh `gate evaluate` call for that agent does not return `continue:false` from a lockdown rule.

**Acceptance Scenarios**:

1. **Given** `gov.db agent_state` has `clawta` with `locked=1, locked_ts=T1, total=12`, **When** the operator runs `chitin-kernel session unlock -agent clawta -reason "policy relaxed"`, **Then** the row is updated to `locked=0, unlock_ts=T2 (T2 > T1), total=12 (unchanged)` and a `session_unlocked` chain event is written with `agent=clawta, reason="policy relaxed", ts=T2`.
2. **Given** `agent_state` has no row for `nobody`, **When** the operator runs `chitin-kernel session unlock -agent nobody`, **Then** the command exits non-zero with `error: no agent_state row for "nobody"` to stderr, no chain event is written, and `gov.db` is unchanged.
3. **Given** the unlock just landed for `clawta`, **When** a plugin queries `chitin-kernel session status -agent clawta`, **Then** the response JSON contains `{agent:"clawta", locked:false, locked_ts:"T1", unlock_ts:"T2", lock_epoch:N, total:12}` where `lock_epoch` is a monotonically-increasing counter that lets consumers tell one lockdown generation from the next.

---

### User Story 2 — A plugin discovers an operator unlock and clears its in-process state (Priority: P1)

An in-process plugin (the first consumer is the chitin-governance openclaw plugin from spec 091 v1.1, but the surface is general) holds a sticky in-memory flag indicating "this agent is in lockdown". The plugin doesn't write to `gov.db`; the kernel does. When the plugin's hot path (a `before_tool_call` handler, an authorization check, whatever) sees its own in-memory flag set, it consults the kernel for the authoritative state. If the kernel reports a more-recent unlock than what the plugin observed when it set its flag, the plugin clears its flag and proceeds with normal evaluation. The plugin does NOT need to query the kernel on every call — only on the path where its own flag is set.

**Why this priority**: without this consumer-facing query interface, spec 091 v1.1 cannot recover from a sticky stop without an openclaw process restart. The kernel side's `session unlock` would be inert from the plugin's perspective. This story is the bridge that makes the operator gesture in US1 actually reach in-process state.

**Independent Test**: write a stub Node consumer that maintains its own in-memory `{ locked: bool, lockEpoch: int | null }` per agent. Set it manually to mirror a known kernel lockdown state (`locked: true, lockEpoch: 5`). Issue `session unlock`. Issue `session status`. Verify the response carries a higher `lock_epoch` (or `locked:false`) than the consumer cached. Verify the consumer correctly transitions to "unlocked" and clears its flag.

**Acceptance Scenarios**:

1. **Given** a consumer cached `{locked: true, lock_epoch: 5}` from a prior status query, **When** the operator issues `session unlock` and the consumer queries `session status` again, **Then** the response carries `{locked: false, lock_epoch: 6}` (lock_epoch advanced by ≥1) and the consumer's comparison logic transitions to "unlocked".
2. **Given** the consumer cached `{locked: true, lock_epoch: 5}`, **When** no unlock has occurred yet and the consumer queries `session status`, **Then** the response carries `{locked: true, lock_epoch: 5}` (epoch unchanged) and the consumer keeps its sticky-stop flag set.
3. **Given** `chitin-kernel` is unavailable (binary missing, query times out), **When** the consumer attempts a status query, **Then** the consumer receives an error from its query helper and (per its own policy) MUST fail-closed by keeping the lock set. This spec does NOT require consumers to implement any particular fallback; it only contracts that the kernel returns a distinguishable error rather than a misleading "unlocked" default.

---

### User Story 3 — Operator inspects current session state on demand (Priority: P2)

An operator running an incident wants to see, for one agent or all agents, the current lock state, last lock time, last unlock time, lifetime denial count, and current escalation level (`normal | elevated | high | lockdown`). They run `chitin-kernel session status` (no `-agent` flag) to dump all agents, or `chitin-kernel session status -agent <id>` for one. Output is JSON-by-default (so it composes with `jq`) with a `--text` flag for human-readable. The command is read-only — it never writes to `gov.db` or the chain.

**Why this priority**: incident response surface. Operators currently shell into sqlite manually (`sqlite3 ~/.chitin/gov.db "SELECT * FROM agent_state ..."`) which is fragile (depends on schema knowledge) and not chitin-native. A first-class subcommand makes the state queryable from any chitin-aware tool.

**Independent Test**: with `gov.db` containing a known set of agent rows, run `chitin-kernel session status` and verify the JSON structure matches the documented shape; run with `-agent` for a specific row; run with `-agent` for a non-existent row and verify the error is operator-readable.

**Acceptance Scenarios**:

1. **Given** `agent_state` has 3 rows, **When** the operator runs `chitin-kernel session status`, **Then** stdout contains a JSON array of 3 objects each carrying `{agent, locked, locked_ts, unlock_ts, lock_epoch, total, level}` and exit code is 0.
2. **Given** the operator passes `--text`, **When** the same query runs, **Then** stdout contains a fixed-column human-readable table with the same five fields plus a `LEVEL` column.
3. **Given** the operator passes `-agent nobody` for a non-existent agent, **When** the command runs, **Then** exit code is non-zero and stderr contains `error: no agent_state row for "nobody"`.

### Edge Cases

- **Unlocking an already-unlocked agent** — the command succeeds (idempotent), still emits a `session_unlocked` chain event, but the `lock_epoch` does NOT advance (consumers MUST treat "no epoch change" as "no transition"). This prevents accidental double-unlock from confusing consumers that compare epochs.
- **Locking a never-seen agent via `session lock`** (operator kill-switch CLI, mirrors the existing `Counter.Lockdown()` Go method) — the row is created with `locked=1, total=0, locked_ts=NOW, lock_epoch=1`. This is a documented bootstrap path for "preemptively lock this agent before its first denial".
- **Concurrent unlock from two operators** — the SQLite transaction serializes; the second unlock is a no-op (already unlocked) but still emits its own chain event with the second operator's `-reason`. Consumers see one `lock_epoch` advance (from the first), not two.
- **Chain emission failure during unlock** — the lock state in `gov.db` MUST be committed FIRST, then the chain emission attempted. A chain-emission failure leaves the agent unlocked (the operator-facing effect) but logs a warning. Operators can later replay the chain event manually. The reverse ordering (emit then commit) is forbidden — it would risk a chain event for an unlock that didn't actually happen.
- **Unlock with no `-reason` flag** — accepted; the chain event records `reason: null` (or empty string). Operators are encouraged but not required to supply context.
- **Status query during a write transaction** — readers use SQLite's WAL read-without-blocking semantics; queries return the last committed state. No new locking semantics introduced.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The kernel MUST expose a `chitin-kernel session unlock -agent <id> [-reason <text>]` subcommand that sets `agent_state.locked = 0`, populates `agent_state.unlock_ts` with the current UTC RFC3339 timestamp, advances `agent_state.lock_epoch` by 1, leaves `total` and the `denials` / `denial_events` tables untouched, and emits one `session_unlocked` chain event with payload `{agent, reason, ts, lock_epoch_after, locked_ts_before, total_at_unlock}`.

- **FR-002**: The kernel MUST expose a `chitin-kernel session status [-agent <id>] [--text]` subcommand that reads from `agent_state` without modifying any table or emitting any chain event, returning JSON-by-default with the shape `{agent, locked, locked_ts, unlock_ts, lock_epoch, total, level}` (single object when `-agent` is given, array otherwise; `--text` switches to a fixed-column table). Exit code is 0 on success, non-zero on missing-agent or IO error.

- **FR-003**: The kernel MUST expose a `chitin-kernel session lock -agent <id> [-reason <text>]` subcommand wrapping the existing `Counter.Lockdown()` semantics (sets `locked=1, locked_ts=NOW, total=max(total, 10)`) AND additionally advancing `lock_epoch` by 1 AND emitting one `session_locked` chain event with payload `{agent, reason, ts, lock_epoch_after, source: "operator_cli"}`. This subcommand is the operator kill-switch — distinct from automatic lockdowns from `RecordActionDenial`.

- **FR-004**: `agent_state` schema MUST gain two new columns: `unlock_ts TEXT` (RFC3339, nullable, populated by `session unlock`) and `lock_epoch INTEGER NOT NULL DEFAULT 0` (monotonically increasing per agent; incremented by every lock and every unlock). The schema migration MUST be additive and backward-compatible — existing rows get `unlock_ts = NULL, lock_epoch = 0` and continue to function with all existing kernel code paths.

- **FR-005**: Automatic lockdowns inside `Counter.RecordActionDenial` (the existing total≥10 escalation path at `escalation.go:122-125`) MUST also advance `lock_epoch` and emit a `session_locked` chain event with `source: "auto_escalation"`. This keeps `lock_epoch` honest as a generation counter — consumers can rely on epoch advances to detect ANY new lockdown, not just operator-CLI ones.

- **FR-006**: The `session_unlocked` and `session_locked` chain event payloads MUST include `lock_epoch_after` so consumers reading the chain can correlate chain events with the epoch numbers they see via `session status`. The chain remains the canonical audit log; `gov.db` is the live state.

- **FR-007**: All three subcommands (`unlock`, `lock`, `status`) MUST honor a `--policy-file` / `--db-path` override flag (matching existing kernel CLI conventions at `cmd/chitin-kernel/main.go`) so they work in test sandboxes without touching the operator's real `~/.chitin/gov.db`.

- **FR-008**: The unlock operation MUST be transactional: the `gov.db` update and the chain event emission MUST NOT leave the system in a state where the chain event was written but the lock is still set, OR the lock is cleared but no chain event records why. The operationally-correct ordering is `gov.db` first, chain second; a chain failure after a successful `gov.db` update logs a warning and exits non-zero but does NOT roll back the lock (the operator gesture is the source of truth and was completed).

- **FR-009**: The `status` subcommand's JSON output MUST be deterministic — when called with no arguments, agents are sorted by `agent` ASCII order; epochs and timestamps are formatted consistently across invocations. This is so consumers and operator tooling can diff successive snapshots reliably.

- **FR-010**: `Counter.Reset(agent)` (which today deletes the row entirely) MUST be preserved as an existing-API "wipe everything" path for test fixtures and operator-level "this agent is decommissioned" gestures. The new `session unlock` is a softer operation; `Reset` is the destructive sibling. The spec does NOT change `Reset`'s behavior — it adds new operations alongside.

### Key Entities

- **`agent_state` row (extended)**: One row per agent. Existing columns: `agent (PK), total, locked, locked_ts`. NEW columns: `unlock_ts` (RFC3339, nullable) and `lock_epoch` (integer, monotonic per agent). The `(locked, lock_epoch)` pair is the load-bearing state consumers query: `locked` is the current verdict, `lock_epoch` is the generation counter that distinguishes "the same lockdown" from "a new lockdown after an unlock".

- **`session_locked` chain event**: Emitted on every lock transition (both operator CLI and automatic escalation). Payload: `{event_type:"session_locked", agent, ts, reason, lock_epoch_after, source: "operator_cli" | "auto_escalation"}`. Chain-frame fields (`chain_id`, `seq`, `prev_hash`, `this_hash`) follow existing chain-event conventions.

- **`session_unlocked` chain event**: Emitted on every unlock. Payload: `{event_type:"session_unlocked", agent, ts, reason, lock_epoch_after, locked_ts_before, total_at_unlock}`. The `total_at_unlock` field preserves the denial count at the moment of unlock for forensic reconstruction.

- **`lock_epoch`**: A monotonically-increasing integer per agent. Starts at 0 for a never-locked row. Advances by 1 on every lock transition (manual or automatic) and every unlock. Consumers compare a cached epoch against a freshly-queried epoch to detect transitions without needing to compare timestamps (which is fragile across clock skew or clock resets).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With an agent in lockdown, an operator can unlock it via a single CLI invocation in under 5 seconds wall-clock, with denial counters and prior chain events preserved verbatim. Verified by: lock + denial + unlock + status comparison + chain-event grep.

- **SC-002**: After an unlock, a consumer that queries `session status` and compares epochs deterministically detects the transition in 100% of cases (no race conditions, no missed transitions). Verified by: a stress test running 1000 lock/unlock cycles against a stub consumer and asserting 1000 detected transitions.

- **SC-003**: Spec 091 v1.1 (the first consumer) can recover a sticky-stopped openclaw session **without restarting the openclaw process**, validated by reproducing the 2026-05-23 failure mode (sticky `stopHookActive[sessionId] = true`), running `chitin-kernel session unlock -agent clawta`, and observing the next tool-call attempt evaluate through the gate normally.

- **SC-004**: The `gov.db` schema migration (adding `unlock_ts` and `lock_epoch`) succeeds on the operator's real database in production without data loss, with all pre-existing `agent_state` rows readable, and all existing kernel code paths (`Counter.RecordDenial`, `Counter.Level`, `Counter.IsLocked`, `Counter.Lockdown`, `Counter.Reset`, automatic escalation) continue to function with byte-identical observable behavior for callers who don't read the new columns.

- **SC-005**: Operators report (qualitatively) that `chitin-kernel session status` replaces ad-hoc `sqlite3 ~/.chitin/gov.db "SELECT ..."` invocations during incident response. Measured by: in the first week after merge, no operator-side incident notes reference raw sqlite queries against `agent_state` for state inspection.

## Assumptions

- The kernel binary `chitin-kernel` is available on the operator's PATH and on the paths that consumer plugins use to spawn subprocesses. Existing chitin deployments satisfy this; the spec does not introduce a new packaging requirement.
- `~/.chitin/gov.db` is writable by the operator's user and by any consumer process that the operator runs (i.e., the existing permission model is unchanged). The CLI subcommands inherit the same file-permission contract as the existing `chitin-kernel emit` and `chitin-kernel gate evaluate` subcommands.
- The chain emission path (`chitin-kernel emit` invoked internally as the kernel writes chain events) is already load-bearing and well-tested; this spec reuses it for `session_locked` and `session_unlocked` events without introducing a new emission mechanism.
- SQLite's WAL mode handles concurrent reads (status queries) and writes (lock/unlock) without consumer-visible blocking. WAL is already enabled in `escalation.go:30`.
- The first consumer of this surface is spec 091 v1.1; future consumers (hermes plugin, future agents) MAY use it but are not required to. The spec specifies the producer (kernel) contract; it does not mandate consumer behavior.
- Operators may continue to use `Counter.Reset()` (via test fixtures or future kernel CLI) for the "wipe and start over" path. Spec 096 is additive — no existing API is removed.

### Scope

**In scope**:

- `chitin-kernel session unlock`, `session lock`, and `session status` subcommands and their argument parsing
- `agent_state` schema additions (`unlock_ts`, `lock_epoch`) and the additive migration
- `session_locked` and `session_unlocked` chain event types and their payload schemas
- Internal kernel call sites that produce automatic lockdowns (`Counter.RecordActionDenial`) — those must also advance `lock_epoch` and emit `session_locked` so the generation counter remains honest (FR-005)
- A smoke test exercising the operator round-trip: lock → status → unlock → status → verify chain events

**Out of scope**:

- Consumer-side logic for any specific plugin — that lives in the consumer's spec (091 v1.1 for the openclaw plugin; any future plugin spec for its own consumption)
- The `Counter.Reset()` API behavior — preserved as-is for now (FR-010); a future spec may deprecate it but this one doesn't
- Per-rule or per-tool granularity unlocks — unlock is whole-agent for v1; finer-grained recovery is a future amendment if needed
- Distributed/multi-host coordination — `gov.db` is a single-host SQLite file; spec 070 (orchestrator) handles cross-host concerns separately
- Replacing the existing `Counter.Lockdown()` Go API — the new `session lock` CLI subcommand wraps it; the Go API stays for in-process callers
- A TTL/auto-unlock mechanism — operator must explicitly unlock; auto-unlock would mask upstream bugs (same rejection as spec 091 v1.1)

### Dependencies

- **Spec 091 v1.1** is the first consumer. Spec 091 v1.1's design depends on this spec's `session status` and `session unlock` subcommands existing; spec 096 must merge before 091 v1.1 implementation can begin.
- **Constitution §1**: kernel is the only chain/db writer. This spec extends what the kernel writes; it does not introduce a new writer.
- **Constitution §7**: swarm is the orchestrator. The new CLI subcommands are operator gestures, not driver bypasses — they reinforce §7 by giving the operator a chitin-native unlock instead of forcing direct sqlite manipulation.
- **`escalation.go` schema** at `go/execution-kernel/internal/gov/escalation.go:41-46`. The additive migration extends this CREATE TABLE; existing readers (lines 119, 166) and writers (lines 102, 123, 192, 201) continue working without changes.
- **Existing chain emission path** via `chitin-kernel emit -event-json -`. Reused for `session_locked` and `session_unlocked` events.
