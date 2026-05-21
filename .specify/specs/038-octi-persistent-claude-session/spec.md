# Spec 038: Octi — Deterministic Workflow Governance for Swarm Operations

## Summary

Octi is the deterministic workflow layer for swarm operations. It owns git state
gates, ticket/state-machine transitions, leases, retry policy, loop/freeze
detection, agent communication contracts, handoff rules, and verifier-required
progression. It drives pluggable backends (Mini being the first) as workflow
activities, not the other way around.

**Slice 1 is Mini** — the persistent kitty + Claude Code + Discord bridge
interface. Octi's workflow engine is downstream, built once the interface seam
exists and has been proven through operator use.

## Delivery slices

| Slice | Artifact | What it proves |
|-------|----------|---------------|
| 1 | **Mini** (`swarm/bin/mini`, `swarm/mini/`) | Persistent kitty session, `status.json` writes, filtered Discord tail, input lease, operator handoff. Proves the interface seam. |
| 2 | **Octi controller loop** | Stall detection, nudge on stale `status.json`, verify on `done` claim, escalation with evidence. Proves the loop. |
| 3 | **Octi state machine** | Transition enforcement (`starting→working→blocked→verifying→done|needs_review|failed`), controller-verified completion, recovery mode. Proves the determinism. |
| 4 | **Octi worker + Temporal integration** | `swarm/bin/octi-worker`, Temporal workflows for retry/timers/history/replay. Proves durability at scale. |

Slice 1 delivers Mini. Slices 2-4 build Octi on top of the interface Mini
proves. This spec defines the full architecture so slices don't paint into
corners, but only slice 1 AC is binding for MVP.

## Stack context

| Layer | Role | Determinism |
|-------|------|-------------|
| **Chitin** | Governance around tool calls & driver lifecycle | Deterministic policy evaluation |
| **Icarus** | Driver runtime (local LLM, thick determinism) | Deterministic-first execution |
| **Mini** | Persistent Claude/kitty interface adapter | Transport/interface |
| **Octi** | Deterministic workflow governance for swarm ops | State machine + verifier |

Mini is a pluggable activity backend that Octi drives, not the brain. The
full spec is runtime-neutral (language/framework TBD) but workflow-engine
shaped: durable state machine first, pluggable activities second. Slice 1 ships
Mini as the first activity backend, proving the interface before building the
engine around it.

## Dependency chain (explicit)

```
Mini (slice 1): persistent kitty + Claude Code + Discord bridge
  ↳ Proves: status.json contract, input lease, operator handoff, filtered tail

Octi spec iteration (concurrent): operator/spec work conducted through Mini,
  with red/Claude Code able to handle signed governance steps
  ↳ Proves: the interface seam supports real governance workflows

Octi implementation (slices 2-4): deterministic workflow engine after the
  interface path exists
  ↳ Proves: state machine, verifier, retry policy, Temporal durability
```

## Motivation

Hermes cron dispatches are fire-and-forget: no terminal, no visibility, no
continuity. Swarm behavior is currently fuzzy — git state, ticket transitions,
retry policy, and agent handoffs are ad-hoc. Mini makes the session durable,
observable, and handoff-safe. Octi makes the rest deterministic: state machines
with required transitions, leases with expiry, verifiers with pass/fail, and
escalation with evidence.

## Architecture

### Slice 1: Mini

**Launcher** (`swarm/bin/mini`): CLI entrypoint. Creates or reuses a named
kitty window/tab with stable title, env, and cwd. Starts an interactive
`claude --dangerously-skip-permissions` session inside it.

**State directory** (`.swarm/octi/<goal-id>/`): Per-goal runtime state.
- `status.json` — mandatory structured state (see schema below)
- `operators.jsonl` — append-only operator log: `{who, role, action, timestamp, detail}`
- `input.lock` — lease file for input injection: `{holder, acquired_at, expires_at}`
- `transcript.log` — full local append-only terminal transcript

**Filtered tailer**: Reads `transcript.log` locally, posts event-shaped
updates to Discord via webhook. Default surface is `#octi`; `#swarm` gets
coordination-only summaries.

Slice 1 delivers `mini open`, `mini status`, `mini nudge`, `mini watch`,
`mini stop` — the interface adapter and bridge, not the workflow engine.

### Slices 2-4: Octi (downstream of Mini)

**Controller loop**: Outer loop that drives the kitty session via Mini's
public interface (`MiniSession`). Watches `status.json` for staleness, sends
nudges on stall, runs verifiers on completion claims.

**State machine**: Transition enforcement with controller-verified completion.
`state=done` is a claim, not acceptance. No verifier → `needs_review`.

**Worker + Temporal** (`swarm/bin/octi-worker`): Durable workflows for retry,
timers, history, replay. Proves durability at scale.

The controller loop never imports Mini internals — only `MiniSession`.
This boundary is enforced by CI (see AC12).

### Status file schema (mandatory)

```json
{
  "state": "starting | working | blocked | verifying | done | failed | needs_review",
  "updated_at": "<unix_timestamp>",
  "summary": "<one-line current action>",
  "next": "<next intended step>",
  "blockers": ["<human-needed blocker>"],
  "verify": "<command to run for completion verification>"
}
```

Claude Code MUST write to `status.json` periodically. The `updated_at` field
is the primary liveness signal — not terminal output.

### Completion semantics

- `state=done` is a **claim**, not acceptance. The controller runs the
  `verify` command independently before marking completion.
- If no `verify` command is configured, completion lands in `needs_review`,
  never auto-`done`.
- The controller does NOT trust exit codes or TUI output for completion.
- Slice 1: completion semantics are advisory only (no controller loop yet).
  The `verify` field is written but not executed automatically until slice 2.

### Stall detection (three layers, priority order)

1. `status.json` `updated_at` stale beyond `3 × interval + jitter` → nudge
2. Kitty window output activity (tool calls, file writes) → still alive, just
   not updating JSON
3. Cursor at input prompt for >N seconds with no status update → likely stuck

Slice 1: layers 1 and 3 are observable via `mini status`. Automatic nudge
on stall is slice 2.

### Command set

Slice 1 (`mini`):

| Command | Purpose |
|---------|---------|
| `mini open --spec/--ticket/--goal` | Launch/reuse kitty session with goal |
| `mini status` | Read `status.json` + operator log |
| `mini nudge --message` | Lease-locked input injection |
| `mini watch` | Summarize transcript events to Discord |
| `mini stop` | Terminate session, mark `state=failed` |

Slices 2-4 (`octi`, added on top of `mini`):

| Command | Purpose |
|---------|---------|
| `octi open --spec/--ticket/--goal` | Full lifecycle: open + controller loop |
| `octi verify` | Run verifier independently |
| `octi pause` | Stop outer loop nudges/verifies (session stays alive) |
| `octi resume` | Re-enable outer loop |
| `octi stop` | Terminate session, mark `state=failed` |

### `pause`/`resume` semantics

`pause` and `resume` control the **outer loop only**. They stop/start automatic
nudging, verification, and escalation. The kitty terminal session keeps running
and Claude Code is not expected to self-suspend. Claude-honored pause is a
future explicit contract, not part of the MVP.

Slice 1: pause/resume not applicable — there is no outer loop yet. Manual
nudge via `mini nudge` is the slice-1 substitute.

### Discord surface contract

| Mode | Content | Default |
|------|---------|---------|
| Filtered tailer | Events: started, goal changes, step summaries, questions, stalls, errors, verifier results, done/failed | ✅ Default |
| Raw full tail | Complete terminal output | ❌ Opt-in debug only (`--debug-tail`) |

Webhook URL from `OCTI_DISCORD_WEBHOOK_URL` env var or local config. **Webhook
URLs are never committed to git.** Leaked/pasted URLs are considered compromised
and must be revoked immediately.

### Worktree management

- Auto-create worktree unless `--cwd-is-worktree` is explicit.
- Worktree path: `~/workspace/chitin-octi-<slug>` (slice 1 uses same convention)
- Branch: `octi/<slug>` or `agent/octi-<ticket>` if ticket-backed.
- No primary checkout edits (constitution §2).

### Input lease protocol

Before injecting input via kitty remote control:
1. Acquire `.swarm/octi/<goal-id>/input.lock` with `{holder, acquired_at, expires_at}`
2. Send input
3. Release lock

If lock is held and not expired, other operators must wait. This prevents
dual-paste conflicts.

### Operator roles

| Role | Rights |
|------|--------|
| `owner` | Goal setter, can change goal, can stop |
| `watcher` | Can view status, read transcript |
| `reviewer` | Can nudge, can verify, can approve completion |

Roles are not hardcoded to people. Default flow: goal setter owns until
`working`, then any operator can monitor/nudge under lease.

## File scope

### MAY write under

- `swarm/bin/mini` (slice-1 launcher script)
- `swarm/mini/` (slice-1 implementation package)
- `swarm/bin/octi` (slice-2+ launcher script)
- `swarm/octi/` (slice-2+ implementation support package)
- `swarm/bin/octi-worker` (slice-4 placeholder; not invoked in slice 1)
- `.swarm/octi/` (runtime state — not committed)

### MUST NOT write under

- `go/` (kernel)
- `apps/` (console)
- `libs/` (adapters/plugins)
- `swarm-controller/` (dispatch pipeline — Octi is a consumer, not an author)
- `.specify/` (governance — this spec is the exception, already placed)
- Any path under `~/workspace/chitin/` primary checkout

## Acceptance criteria (slice 1 — Mini)

1. `mini open --goal "..."` creates a named kitty window with stable title and
   starts an interactive Claude Code session.
2. `status.json` is written periodically by Claude Code with all six fields
   populated.
3. `mini status` reads and displays current state from `status.json`.
4. `mini nudge --message "..."` acquires input lease before sending, releases
   after.
5. `mini watch` posts filtered event-shaped updates to Discord via webhook.
6. `mini stop` terminates the kitty session and marks `state=failed` in
   `status.json`.
7. Webhook URL sourced from env/config, never committed to git.
8. All worktree operations use `~/workspace/chitin-octi-<slug>`, never
   primary checkout.
9. Temp prompt files (goal text, nudge text passed via kitty remote control)
   are written to `.swarm/octi/<goal-id>/` and unlinked after injection. On
   crash or early exit, stale temp files are cleaned up on next `mini open`
   or `mini status` invocation for the same goal-id. An `unlink` call that
   fails because the file is already gone is not an error.
10. Import boundary: Mini exports `MiniSession` as its public interface.
    Octi (slice 2+) MAY import `MiniSession`. Octi MUST NOT import Mini
    internals (kitty window management, prompt formatting, terminal parsing).
    This boundary is verified by a grep/assert in CI:
    `grep -r 'from.*mini.*import' swarm/octi/ | grep -v 'MiniSession'`
    must return zero matches.
11. `--recovery <goal-id>` on `mini open` resumes an existing goal's state
    directory and kitty session. If the goal-id has no existing state directory,
    `--recovery` is a usage error (not a fresh start).
12. `swarm/bin/octi-worker` is a slice-4 placeholder. Slice 1 invokes the
    Mini backend directly from `swarm/bin/mini`. The worker entrypoint
    exists only as a stub to reserve the file path.
13. Temp file unlink is verified in tests: a test creates a temp prompt file,
    injects it via kitty remote control, and asserts the file no longer exists
    on disk after injection completes. (P2 from security review: argv
    cleanliness without unlink verification leaves the main secret-material
    failure mode uncovered.)

## Acceptance criteria (slices 2-4 — Octi, non-binding for MVP)

These AC define the full architecture but are not binding until slice 1 ships
and the interface seam is proven.

- **Slice 2:** `octi verify` runs the `verify` command from `status.json`
  independently and reports pass/fail. Completion requires independent verifier
  pass. No verifier configured → `needs_review`, not `done`. Stall detection
  nudges automatically.
- **Slice 3:** State machine enforces transitions. `octi pause`/`octi resume`
  control outer loop only; terminal session stays alive. Recovery mode via
  `--recovery <goal-id>` as workflow input.
- **Slice 4:** `octi-worker` entrypoint, Temporal workflows for retry/timers/
  history/replay.

## Out of scope (MVP / slice 1)

- `claude -p` batch mode — terminal-resident continuity is the value prop
- Claude Code self-suspending on `pause` — outer loop only (slice 2+)
- Multiple simultaneous sessions per goal — one kitty window per goal-id
- Board integration (kanban ticket status updates) — prove the loop first,
  add board later
- Separate Octi repo or board — stays in Chitin/swarm until extraction
  trigger (3+ repos or independent release cadence)
- Automatic stall detection and nudge — slice 2
- Controller loop — slice 2
- State machine enforcement — slice 3
- Temporal integration — slice 4

## Extraction trigger

If Octi becomes reusable outside Chitin/swarm, or serves 3+ active cross-repo
dispatch consumers, move to `chitinhq/octi` with its own repo, board, and
spec-kit namespace.

## Dependency

- Kitty terminal emulator (must be installed)
- Claude Code CLI (must be installed and authenticated)
- Chitin governance hooks (applied by default, `--operator-unsafe` escape hatch
  only if explicitly requested)

## References

- Constitution §1.1: Spec before ticket
- Constitution §2: No primary-checkout edits; use worktrees
- Spec 037: sw-011 heartbeat proof tests (heartbeat/staleness pattern)
- Icarus ic-006: Deterministic-first post-check pattern