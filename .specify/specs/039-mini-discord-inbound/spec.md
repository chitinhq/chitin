# Spec 039: Mini — Discord-inbound mention listener

**Status**: slice 1 ratified 2026-05-19. R1 (per-session thread binding) +
B2 (first-inbound binds) are the binding routing choice. Q2 (rate limit)
and Q3 (extraction vs parallel) deferred — see *Open questions* below.
Slot 039 confirmed free (38 was the previous numbered spec; 728/730 are
ticket-numbered, not slice-numbered).

**Author lens (Knuth)**: name the boundaries — routing, auth, race, failure.
Every "TBD" is a place two implementations would disagree.

## Summary

Add an inbound path so the operator (and other swarm agents) can invoke a
running Mini session from Discord. Currently Mini's Discord webhook is
outbound-only (state transitions → channel); there is no way to address
Mini back. This slice closes the gap by adding a `mini-mention-listener`
daemon that polls the agent-bus DB for messages addressed to a Mini
session and routes them to `MiniSession.nudge(...)`. Modelled exactly on
`clawta-mention-listener` (swarm/bin/clawta-mention-listener).

## Motivation

Three concrete pains:

1. **Operator can't redirect a stuck Mini without SSH'ing to the box.**
   When Octi nudges and the model is still wedged, the operator's only
   recourse is `mini nudge <goal_id> "..."` from a terminal. Discord is
   already where they live — they should be able to type there.
2. **Hermes and Clawta can talk to each other via bus, but Mini is a
   write-only sink.** No peer-agent can hand Mini a follow-up. This
   blocks any future "Clawta reviews Mini's PR and asks for a change"
   workflow that doesn't go through GitHub.
3. **Octi (slice 2) already imports `MiniSession.nudge`** — the verb
   exists, it just has no Discord-side trigger.

## Non-goals (this slice)

- **No new Discord bridge.** Hermes' existing bus↔Discord mirror is
  re-used. If the bridge is down, this listener degrades to bus-only
  (still works for swarm peers, just not for Discord users).
- **No state-machine transitions from Discord.** This slice only adds
  `nudge`. `pause` / `resume` / `stop` from Discord is a follow-up.
- **No auth model beyond "if it landed in the bus, it's trusted."**
  Hermes already enforces channel ACLs upstream; Mini trusts the bus.
  Per-Mini ACLs are a follow-up.

## Architecture

### Components

```
Discord channel
   ↓ (Hermes bus↔Discord mirror — already exists)
~/.chitin/agent-bus/bus.db
   ↓ (60s poll — NEW: mini-mention-listener)
MiniSession(goal_id).nudge(message)
   ↓ (filtered tail — already exists)
status.json updates → webhook → back to Discord channel
```

### New surface (proposed)

- `swarm/bin/mini-mention-listener` — Python daemon, 60s poll loop.
  Same shape as `clawta-mention-listener`.
- `swarm/bin/install-mini-mention-listener.sh` — systemd-timer
  installer, sibling to `install-clawta-mention-listener.sh`.
- `swarm/mini/_internal/routing.py` — new module. Single responsibility:
  given a bus message, return the `goal_id` it targets (or None).
- `.swarm/octi/<goal_id>/thread_id` — new file in state dir. Single
  line: the bus thread ID this session is bound to. Written by
  `mini open` (or first-write by the listener — see AC4 below).

### Routing: thread-id ↔ goal-id (the only genuinely new design call)

**The boundary case**: a bus message arrives on thread `T` mentioning
"@mini". Which `goal_id` does it target?

Two viable answers:

- **(R1) Per-session thread binding.** Each Mini session is bound to
  exactly one bus thread. State dir contains `thread_id`. Listener
  reverse-indexes `thread_id → goal_id` by scanning `.swarm/octi/*/`.
  Inbound message on thread `T` routes to the goal whose `thread_id ==
  T`. Unbound threads → no-op (or one-time welcome message that asks
  the operator to bind).
- **(R2) Goal-id in message body.** No per-session binding; listener
  parses `@mini <goal-id-prefix>` from the message. If prefix matches
  exactly one running session, route; if ambiguous or no match, post
  an error reply.

**Binding (slice 1): R1.** Matches how humans actually use Mini ("the
session I'm watching in thread X"), composes with Hermes' existing
thread-per-task convention, and keeps the listener stateless beyond
the filesystem scan. R2 stays as the documented fallback when the
listener encounters an unbound thread (it posts an error that names
the running goals; operator can then bind explicitly).

**Binding moment**: when is `thread_id` written?

- **(B1) `mini open` writes it.** Requires `mini open` to know its
  Discord thread, which it currently doesn't. Operator would pass
  `--thread <id>` or env var. Manual, but explicit.
- **(B2) First inbound message binds it.** State dir starts without
  `thread_id`. First time a bus message mentions `@mini` and quotes
  the goal id (or arrives in a thread whose subject includes the goal
  id), the listener writes `thread_id` and routes. Subsequent messages
  on that thread route without parsing.
- **(B3) Mini's first outbound webhook post binds it.** Discord
  returns the message ID in the response; with the
  `wait=true` query param, Hermes' bridge can capture the channel/
  thread and persist it. Auto, but requires bridge changes.

**Binding (slice 1): B2.** First inbound message on a thread binds
the `thread_id → goal_id` mapping for the matched goal. B1
(`--thread` flag at `mini open`) is deferred — operator-facing
override, not blocking for slice 1. B3 (webhook-response binding)
requires Hermes bridge changes and is out of scope.

A goal is "matched" for the binding attempt when the inbound message
body contains a substring that uniquely matches exactly one live
goal-id under `.swarm/octi/*/`. Ambiguous match → R2 fallback error
listing candidates. No match → no-op + log skip (the message wasn't
for any Mini).

## File-system scope

Worker MAY write under:
- `swarm/bin/mini-mention-listener`
- `swarm/bin/install-mini-mention-listener.sh`
- `swarm/mini/_internal/routing.py`
- `swarm/tests/test_mini_mention_listener.py`
- `swarm/tests/test_mini_routing.py`
- `swarm/docs/mini.md` (operator docs — append, not rewrite)
- `.specify/specs/039-mini-discord-inbound/**`
- `infra/systemd/mini-mention-listener.service`
- `infra/systemd/mini-mention-listener.timer`

Worker MUST NOT write under:
- `swarm/octi/**` — controller is downstream of this slice
- `swarm/mini/__init__.py` — public surface is frozen at `MiniSession`
- `services/agent-bus/**` — bus contract is fixed
- `swarm/bin/install-mini.sh` — separate installer, not the inbound one

## Boundary cases (name them now, not in code review)

1. **Empty inbound** — `@mini` with no body → no-op, log skip.
2. **Mini session paused** — `controller.paused` flag set by Octi.
   Listener still routes; `MiniSession.nudge` no-ops gracefully (it
   already does, per slice-1 code). Operator gets no reply.
3. **Mini session stopped** — state dir exists but kitty window is
   dead. Listener detects via `MiniSession.is_alive()` (slice 1).
   Replies "session stopped" once, then skips the message until the
   operator restarts.
4. **Duplicate inbound** — same bus message ID seen twice. Listener
   marks-read after route; bus's `unread` flag is the dedupe key.
   Same pattern as clawta-mention-listener.
5. **Multiple Minis on same thread** — fails AC2 (1:1 binding). The
   `thread_id → goal_id` index must be 1:1; if two sessions claim the
   same thread, the listener refuses to bind the second and posts an
   error.
6. **Listener restart mid-message** — message in-flight when the
   daemon dies. Bus marks-read happens after route succeeds; on
   restart, message is replayed. Idempotency: `MiniSession.nudge`
   appends to the kitty stdin, so a double-send is a double-type, not
   a crash. Acceptable but noisy. Future: dedupe by message ID in
   state dir.
7. **Bus DB locked** — listener uses `sqlite3` like clawta does;
   60s poll absorbs transient locks. Hard failure → systemd
   restart-on-failure.
8. **Goal id collision** — operator opens two sessions with the same
   prefix. R2 fallback path posts an error listing both.

## Acceptance criteria (binding for slice 1 of 039)

> **AC1** (binding). `mini-mention-listener --once` reads
> `~/.chitin/agent-bus/bus.db` for messages where (`audience='mini'`
> OR body matches `@mini` regex) AND `unread=1`, and exits after
> processing the current batch.

> **AC2** (binding). Per-session thread binding is 1:1. Scanning
> `.swarm/octi/*/thread_id` MUST yield at most one goal-id per
> thread-id. The listener enforces this at write time and rejects
> binding a thread already claimed by a different live goal.

> **AC3** (binding). On a routed message, the listener calls
> `MiniSession(state_dir=...).nudge(content)` and posts a reply on
> the same bus thread via `bus_reply`, in that order. If `nudge`
> raises, no reply is posted and the message stays unread.

> **AC4** (binding). First-inbound thread binding (B2): if the
> matched goal has no `thread_id` file, the listener writes one
> atomically (tmp + rename) with the inbound bus thread id before
> calling `nudge`. Mode 600.

> **AC5** (binding). The listener never imports from `swarm/octi/`.
> The boundary regex `from[[:space:]]+.*swarm\.octi.*[[:space:]]+import`
> over `swarm/bin/mini-mention-listener` returns zero matches.
> (This mirrors the AC10 boundary on the octi side.)

> **AC6** (binding). The listener writes its goal-id routing decision
> to `<state_dir>/inbound.jsonl` (append-only) before calling `nudge`.
> Schema: `{ts, bus_msg_id, bus_thread_id, content_sha, decision}`.
> `decision ∈ {nudged, no_op_paused, no_op_dead, error}`.

> **AC7** (non-binding, slice 1 polish). The listener tolerates the
> Hermes bridge being down: routed nudges still succeed via the bus,
> the reply hop is best-effort.

## Out of scope (slice 2+ of 039)

- Pause/resume/stop from Discord (`@mini pause`, etc.) — separate AC
  set; needs an auth model first.
- Per-Mini ACLs (which Discord user IDs can address which Mini) —
  needs `operators.jsonl` integration.
- B3 webhook-response binding — needs Hermes bridge changes.
- Cross-channel routing (Mini posts to #octi, can be addressed from
  #swarm) — single-channel assumption holds for slice 1.

## Open questions for operator ratification

- ~~**Q1.**~~ **Resolved 2026-05-19**: R1 + B2 locked. First-inbound
  binding, per-session thread, R2 fallback on unbound/ambiguous.
- **Q2.** What's the rate limit per Mini? Clawta has no rate limit;
  Mini is a slower consumer (the kitty session needs time to read).
  Proposal: drop messages if the kitty's stdin buffer exceeds N
  bytes, log to `inbound.jsonl` as `decision=no_op_rate_limit`.
- **Q3.** Does this listener replace `clawta-mention-listener`'s code
  by extraction (shared `_listener.py`) or stay parallel? Extraction
  saves ~80 LOC but couples two daemons. Slice 1: stay parallel.

## Reference: clawta-mention-listener

The closest existing precedent is `swarm/bin/clawta-mention-listener`
(commit 5a4d5e5). Read it. The mental model — 60s poll, bus.db query
shaped by audience+mention regex, subprocess-shell to the agent, reply
on success, mark-read after — transfers directly. The only difference
is the routing layer (clawta has one global agent; mini has N
per-goal sessions).
