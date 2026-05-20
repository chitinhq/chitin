# Spec 050: Mini MCP — spec-driven dispatch + reliable nudge

**Status**: slice 1 ratified 2026-05-19 — red signed off on Q1–Q4.
Slot 050 confirmed free (049 was the last numbered spec; 040–047 are
the octi cluster, 048 unused).

**Resolved questions** (red, 2026-05-19):
- Q1 → exact match only. No fuzzy partial-slug matching.
- Q2 → optional `invoked_by` arg, fall back to `$OCTI_OPERATOR`, then `mcp`.
- Q3 → per-session Discord threads deferred to slice 2.
- Q4 → status-transition watcher design resolved during slice-1 build.

**Author lens (Knuth)**: name the boundaries — what is a valid spec
reference, what happens when one is missing, what exactly commits a
prompt in the TUI. Every "TBD" is a place two implementations disagree.

## Summary

Mini now has a working MCP control surface (`services/mini-mcp/server.py`,
PR #795) and a one-way Discord event log (`OCTI_DISCORD_WEBHOOK_URL` →
#mini). Two gaps block it from being trustworthy:

1. **`mini_open` accepts free-form goal text.** Operators (and agents)
   can dispatch arbitrary, undefined work. Per constitution §1
   ("spec before dispatch"), a Mini session should only ever run work
   that has a ratified spec. `mini_open` should take **spec
   reference(s)**, resolve them to `.specify/specs/NNN-<slug>/spec.md`,
   and compose the session's `/goal` from them.

2. **Nudges reach the Claude TUI but never submit.** `inject_via_temp_file`
   appends `\r`, but the operator confirmed live (2026-05-19) that the
   nudge text lands in the prompt and just sits there unsent. The
   prompt is never committed, so the session never acts on the nudge.

This spec defines spec-driven dispatch and a reliable submission
mechanism. It also pins the Discord event-log shape so the log is a
useful operator surface, not noise.

## Motivation

Three concrete pains, all observed in the 2026-05-19 session:

1. **Undefined work.** `mini_open(goal="smoke-test inbound: respond
   pong")` spawned a session against a throwaway goal. There is no
   spec, no acceptance criteria, no way to verify completion. The
   constitution already forbids dispatching un-specced tickets — the
   MCP tool should enforce the same gate.
2. **Silent nudge failure.** Every nudge this session "succeeded"
   (listener logged `nudged`, MCP returned `ok`) but the operator
   watched the prompt sit unsubmitted in the kitty window. A nudge
   that doesn't commit is worse than an error — it looks like success.
3. **Event-log shape undefined.** Mini posts `🐙 session.opened` /
   `📣 nudge.sent` / `🛑 session.stopped` as flat messages. The
   operator wants a feed of *who invoked Mini from where*, with
   per-session detail grouped — not an undifferentiated stream.

## Non-goals (this slice)

- **No Discord inbound.** Discord is one-way: event log only. No
  listener, no `@mini` routing, no channel-as-control. (Superseded
  scope from spec 039 — the operator explicitly retired it.)
- **No spec authoring.** `mini_open` consumes existing specs; it does
  not create or scaffold them.
- **No multi-box orchestration.** One operator box, one kitty instance.

## Requirements

### R1 — `mini_open` takes spec references, not free-form goals

`mini_open` MUST accept a `specs` parameter: a non-empty list of spec
references. It MUST NOT accept a free-form `goal` string. (The Mini
*CLI* `swarm/bin/mini open --goal` keeps free-form goals for operator
break-glass; the constraint is enforced at the MCP layer only.)

A spec reference resolves to a directory under
`.specify/specs/` by one of:

- **Number**: `"050"` → glob `.specify/specs/050-*/`. Exactly one
  match required.
- **Full slug**: `"050-mini-mcp-spec-dispatch"` → exact directory.

Range/list ergonomics:

- The `specs` list MAY contain multiple references:
  `["039", "040", "042"]`.
- A single element MAY be a numeric range string: `"039-042"` expands
  to `039, 040, 041, 042`. A range whose endpoints don't both resolve
  is a hard error (see R3).

### R2 — Composed `/goal` from resolved specs

Given resolved spec dirs, `mini_open` composes the session goal as:

```
Implement the following ratified specs in one shot, in order:

  1. .specify/specs/039-mini-discord-inbound/spec.md — <title line>
  2. .specify/specs/040-octi-scaffolding/spec.md — <title line>

Read each spec.md fully before starting. Honor every acceptance
criterion. Write status.json transitions per the standard Mini
contract. Do not start spec N+1 until spec N's `verify` passes.
```

`<title line>` is the first `# ` heading of each `spec.md`. The Mini
CLI is then invoked as `mini open --goal "<composed text>"`.

### R3 — Missing / ambiguous spec is a hard error

If any reference resolves to zero directories, or a bare number
matches 2+ directories, `mini_open` MUST return a JSON-RPC error
(`-32602`, invalid params) naming the offending reference. It MUST
NOT spawn a partial session. All references are resolved **before**
any session is created.

### R4 — Reliable prompt submission (the nudge defect)

The current `inject_via_temp_file` appends `\r` to the payload and
sends it in a single `kitty @ send-text --from-file`. This does not
commit the prompt: Claude Code's TUI treats the whole `send-text`
blob as pasted input (bracketed-paste semantics), so the trailing
`\r` is literal text inside the paste, not an Enter keypress.

**Fix** (corrected after live diagnosis 2026-05-19 — see below). Three
mechanisms were tried against a 57-line composed goal:

1. Trailing `\r` folded into the body `send-text` — fails. The `\r` is
   literal content inside the bracketed paste, not a submit.
2. A separate `kitty @ send-key enter` — fails. It commits a short
   typed input but is a no-op against an input holding a collapsed
   `[Pasted text]` chip.
3. A separate `send-text` carrying a raw `\r`, bracketed paste
   **disabled** so the CR is not folded into a paste — **works**.
   Methodically confirmed: the paste-chip count went 1 → 0 and the TUI
   began processing.

So the mechanism is:

- `send-text --bracketed-paste=auto --from-file=<body>` — deliver the
  prompt body as a properly start/end-delimited paste.
- `send-text --bracketed-paste=disable --stdin` with `\r` on stdin —
  a distinct raw carriage return that commits the prompt.

`kitty @ send-key` is NOT used for submission.

Additionally, injection at session-open/recovery time MUST wait for
Claude Code's TUI to be ready (the `bypass permissions` footer marker)
before sending — otherwise the keystrokes race the TUI boot and are
lost.

The invariant: **after `inject_via_temp_file` returns, the prompt has
been submitted to Claude — not left in the input buffer.**

### R5 — Discord event-log shape

The #mini event log is the operator's window into Mini activity. Each
event MUST identify *who* invoked Mini and *from where*:

- `🐙 session.opened` — include the invoking agent/operator identity
  and the source (MCP client id, or `cli` for break-glass), plus the
  spec list and goal_id.
- `📣 nudge.sent` — include the holder identity and the goal_id.
- `🛑 session.stopped` — include reason and goal_id.
- `✅ status.<state>` — NEW: on each `status.json` state transition
  (`starting`/`working`/`blocked`/`verifying`/`done`/`failed`), post a
  one-line event so the operator can follow progress without SSH.

Per-session grouping (channel-level feed + threaded detail) is desired
but deferred — see Open questions Q3.

## Boundary cases

1. **Empty `specs` list** → `-32602` error, no session.
2. **Range `"042-039"` (reversed)** → hard error; ranges are ascending only.
3. **Duplicate refs** `["039","039"]` → dedupe, warn, proceed.
4. **Spec dir exists but `spec.md` missing** → hard error naming the dir.
5. **Nudge to a session whose kitty window is gone** → existing
   `kitty @ send-text` failure surfaces as a JSON-RPC error; R4's
   separate `send-key` must fail the same way, not silently.
6. **status.json written rapidly (N transitions in one tick)** → each
   distinct state posts once; identical consecutive states are not
   re-posted (dedupe on last-posted state per goal_id).

## Open questions

- **Q1 — spec ref by slug substring?** R1 allows number or full slug.
  Should a partial slug (`"mini-discord"`) fuzzy-match? Risk: ambiguous
  matches. Proposed: no fuzzy matching in this slice; exact number or
  exact slug only.
- **Q2 — who is "the invoking agent"?** MCP gives no caller identity by
  default. Options: (a) require an `invoked_by` arg on `mini_open`;
  (b) read `$OCTI_OPERATOR`; (c) default to `mcp`. Proposed: optional
  `invoked_by` arg, fall back to `$OCTI_OPERATOR`, then `mcp`.
- **Q3 — per-session Discord threads.** R5 defers threaded grouping.
  Discord webhooks can create a thread via `?thread_name=`. Worth a
  slice 2? Proposed: yes, slice 2 — flat feed with identity tags is
  enough to ship slice 1.
- **Q4 — status-transition watcher.** R5's `status.<state>` events
  need something watching `status.json`. Mini's `watch` subcommand
  already tails events — does it post, or does a new watcher? Proposed:
  resolve during slice 1 design review.

## Acceptance criteria

- **AC1** — `mini_open(specs=["050"])` resolves slot 050, composes a
  goal referencing `spec.md`, spawns a session. `mini_open(goal=...)`
  is rejected (no such parameter).
- **AC2** — `mini_open(specs=["999"])` returns `-32602` naming `999`,
  spawns nothing.
- **AC3** — `mini_open(specs=["039-040"])` expands the range, composes
  a 2-spec goal in ascending order.
- **AC4** — after a nudge, the target session's prompt is committed:
  a follow-up `mini_status` (or kitty buffer inspection) shows Claude
  acted on the nudge, not that it sits in the input buffer.
- **AC5** — every `status.json` state transition produces exactly one
  `✅ status.<state>` post in #mini; no duplicate posts for an
  unchanged state.
- **AC6** — `🐙 session.opened` in #mini names the invoking identity,
  the source, and the spec list.
- **AC7** — installer: any new daemon (e.g. a status watcher for AC5)
  ships with `swarm/bin/install-*.sh` in the same PR (constitution §4).

## Slice plan

- **Slice 1** — R1, R2, R3, R4 (spec dispatch + nudge fix). AC1–AC4, AC6.
  **Shipped 2026-05-19, PR #795.**
- **Slice 2** — R5 event-log shape (detailed below). AC5, AC7.

---

# Slice 2 — detailed specification

**Status**: DRAFT 2026-05-19 — awaiting red sign-off. Expands R5 into
implementable requirements and resolves Q3/Q4.

**Resolved questions**:
- Q3 → **yes, per-session Discord threads.** A flat feed of three
  event types is not "read out the full details per session" (the
  operator's words). Each session gets its own thread.
- Q4 → **extend `mini watch`.** `mini watch` is already the
  per-session "long-running Discord event tailer". It is the natural
  owner of status-transition posting — per-session watcher, per-session
  thread. `mini open` starts it (the `watch.pid` file already exists in
  the stop path). No new standalone daemon.

## Slice 2 summary

Today Mini posts three flat events to #mini (`🐙 session.opened`,
`📣 nudge.sent`, `🛑 session.stopped`) as undifferentiated channel
messages. The operator wants: a **channel-level feed of which agent
invoked Mini against which spec, from where**, and **per-session
detail grouped in a thread** so a session's full activity is followable
without SSH.

## Slice 2 requirements

### S2-R1 — channel feed line names invoker, source, specs

The `🐙 session.opened` message posted to the #mini channel (not the
thread) is the feed entry. It MUST carry:

- the invoking identity (`invoked_by` from spec 050 R1 — agent id,
  `$OCTI_OPERATOR`, or `mcp`),
- the source surface (`mcp` client, or `cli` for break-glass),
- the resolved spec list (the `specs` array — spec numbers, not the
  composed prose),
- the `goal_id`.

Example:

```
🐙 Mini session opened — spec 037,038,039 — invoked by `ares` via mcp
   goal_id: spec-037..039-3a1f   ·   thread ↳
```

### S2-R2 — per-session Discord thread

On `session.opened`, the webhook post MUST create a Discord thread for
that session. The thread id is persisted to the session state dir
(e.g. `<state_dir>/discord_thread.id`). All subsequent events for that
session — `nudge.sent`, `status.<state>`, `session.stopped` — post
**into that thread**, not the channel.

The channel feed therefore stays scannable (one line per session); the
thread holds the session's full event stream.

### S2-R3 — status-transition events

`mini watch`, extended, MUST observe the session's `status.json` and
post one `status.<state>` event into the session thread on each
distinct state transition:

```
✅ status: working    — composing the routing module
🔶 status: blocked    — waiting on operator: <blocker>
✅ status: verifying  — running `verify`
🏁 status: done       — verify passed
```

Dedupe: an unchanged state is not re-posted. The watcher tracks the
last-posted state per `goal_id` (boundary case 6 from slice 1).

### S2-R4 — degrade safely when Discord is unavailable

A webhook failure (thread creation or post) MUST NOT block the session.
Mini's existing `_webhook.post` already swallows network errors; thread
creation gets the same treatment. If thread creation fails, events fall
back to channel-level posts for that session (no thread, but not lost).

### S2-R5 — installer for the watcher

If `mini watch` becomes a process `mini open` auto-starts, its lifecycle
(start on open, kill on stop via `watch.pid`) ships in this slice.
Per constitution §4, any operator-box script ships its installer in the
same PR.

## Slice 2 boundary cases

1. **Webhook has no thread permission** → thread creation 403s → S2-R4
   fallback to channel posts; log once, don't spam.
2. **Session opened, status.json never written** → no `status.*`
   events; thread still exists with the opened + stopped bookends.
3. **`mini watch` dies mid-session** → status events stop; the next
   `mini open`/recovery restarts it. Not fatal — status.json on disk
   remains the source of truth.
4. **Rapid transitions** (N states in one watch tick) → each distinct
   state posts once, in order; identical consecutive states deduped.
5. **Discord thread archived/deleted by a human** → posts into a dead
   thread 404 → S2-R4 fallback.

## Slice 2 acceptance criteria

- **S2-AC1** — `🐙 session.opened` in the #mini channel names the
  invoker, source, spec list, and goal_id (supersedes slice-1 AC6).
- **S2-AC2** — opening a session creates a Discord thread; its id is
  persisted to the state dir.
- **S2-AC3** — `nudge.sent`, `status.*`, and `session.stopped` post
  into the session thread, not the channel.
- **S2-AC4** — each `status.json` transition yields exactly one
  `status.<state>` post; an unchanged state is not re-posted.
- **S2-AC5** — a simulated webhook failure does not block session
  open, nudge, or stop.
- **S2-AC6** — the `mini watch` lifecycle (auto-start on open, kill on
  stop) ships with its installer (constitution §4).

## Slice 2 open questions

- **S2-Q1 — thread creation mechanism.** Discord webhooks create
  threads via `?thread_name=` (forum channels) or post into an
  existing thread via `?thread_id=` (text channels). #mini is a text
  channel — confirm the exact API path: webhook posts the headline,
  then a thread is started off that message. Resolve in design review.
- **S2-Q2 — watcher cadence.** `mini watch` poll interval for
  status.json. Proposed: reuse the mention-listener's 60s, or faster
  (5–10s) since status is local-file polling and cheap.
