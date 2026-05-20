# 042 — Octi agent-bus identity + idempotency contract

> Parent: spec 040 (Octi scaffolding).
> Closes Clawta critique #2 (agent-bus thread 17, msg 7690).
> Live proof of the gap: during the Octi RFP itself, Ares' Discord
> reply to a brief posted in **thread 17** landed in **thread 1** (the
> `#swarm` catchall mirror thread). The mirror has no notion of
> which bus thread a Discord message replies to.

## Summary

Define the stable identity, idempotency, and dedup contract for every
message that crosses the agent-bus ↔ Discord boundary in either
direction. Two failure modes were proven live during the Octi RFP
that produced this spec:

1. **Catchall mis-routing (resolved by architecture, not code)** —
   when #swarm existed, inbound Discord replies routed to a single
   catchall bus thread per channel regardless of which bus thread
   they replied to. Resolved 2026-05-19 by the operator's channel
   architecture change (only #ares + #clawta survive; no shared
   channels = no catchall ambiguity).
2. **Multi-audience routing has no Discord destination** — bus
   threads with `audience=ares,clawta` previously routed to #swarm
   (now deleted). With per-agent-channels only, the bus must
   fan out: one Discord post per audience channel, with a shared
   spec-042 anchor linking them as the same bus thread.

The fix: every cross-boundary message carries a stable identity
(`octi.message.v1`) with a `bus_thread_anchor` field. The outbound
sender stamps it; on multi-audience, the outbound fans out to each
audience's per-agent channel, all sharing the same anchor. The
inbound poller looks the anchor up. The bus dedups by
`(channel_id, discord_message_id)` so retries are safe.

2026-05-20 amendment: bus-originated Discord posts must also resolve
agent mentions to Discord-native user entities (`<@user_id>` or
`<@!user_id>`), not plain text display names such as `@Clawta`.
Display-name canonicalization is insufficient because Discord does
not notify on raw text; the bus is the right boundary to perform this
normalization before `discord_push.try_push`.

## Ticket refs

- Closes: Clawta critique #2 (msg 7690).
- Reproducer in the wild: agent-bus thread 17 RFP (msg 7679) →
  Ares' Discord reply → thread 1 msgs 7685-7687 instead of thread 17.
- Spec 023 — agent-bus bidirectional liveness — shipped the mirror
  itself; this spec adds the identity layer on top of it.
- Related: spec 021 (outbound stale-env) — addressed a different
  failure mode in the same surface.

## File-system scope

Worker MAY write under:

- `services/agent-bus/identity/` — new Python package
  - `services/agent-bus/identity/anchor.py` — anchor generator + parser
  - `services/agent-bus/identity/dedup.py` — `(channel_id,
    discord_message_id)` dedup table + lookup
  - `services/agent-bus/identity/migration.py` — backfill anchors on
    existing bus threads that have a `discord_thread_id`
- `services/agent-bus/discord_push.py` — patch to embed anchor on
  every outbound post and send already-resolved Discord user mention
  entities (extends spec 023)
- `services/agent-bus/discord_mirror.py` — patch to parse anchor on
  every inbound message and route to the correct bus thread
- `services/agent-bus/mentions.py` — canonical agent identity and
  Discord user-id mention resolver
- `services/agent-bus/server.py` — MCP surface: expose
  `bus_resolve_anchor(discord_message_id)` for diagnostics; call
  the mention resolver before writes that can be mirrored to Discord
- `services/agent-bus/tests/test_identity_anchor.py` — unit
- `services/agent-bus/tests/test_discord_mention_entities.py` —
  unit: plain-text `@agent` inputs become `<@user_id>` entities
- `services/agent-bus/tests/test_inbound_thread_routing.py` — unit
- `services/agent-bus/tests/test_bidirectional_identity_e2e.py` — **e2e**
- `services/agent-bus/migrations/042-add-message-identity.sql`
  — schema migration (new `messages.octi_anchor` column +
  `discord_dedup` table)
- `.specify/specs/042-octi-agentbus-identity-contract/**`

Worker MUST NOT write under:

- Discord side (no bot changes — anchor is an in-message convention)
- `~/.hermes/.env` (already managed; no new env vars)
- Octi worker code (this spec is bus-side; Octi consumes the
  contract but doesn't author it)

## Goal

A Discord user replies to a specific bus-originated message in a
per-agent channel (`#ares` or `#clawta`). The reply lands in the
same bus thread that produced the original message, **never** in
that agent's catchall thread. Discord replies that don't carry an
anchor (operator typing freeform in a channel) still land in that
agent's own catchall thread — intentional and preserved. Retries
of the same inbound poll are idempotent: re-polling the same
Discord message twice produces zero duplicate bus messages.

## Requirements

### R1 — `octi.message.v1` anchor format

Every bus-originated Discord post embeds an anchor in a stable,
machine-parseable form. Two valid encodings:

**(a) Discord-message-reference (preferred)**: the outbound post uses
Discord's native `message_reference` field to point at the previous
bus message in the same thread. The inbound parser reads
`referenced_message.id` and looks it up in the dedup table.

**(b) Trailing zero-width anchor (fallback)**: when a bus post has no
prior message to reference (it's the first message of a new thread),
the outbound post appends an invisible-to-humans anchor line:

```
​<octi:thread=17;msg=7679;v=1>​
```

`​` is zero-width space — invisible in rendered Discord, but
the inbound parser strips and reads it.

The parser tries (a) first, falls back to (b). If neither resolves,
the message lands in the channel's catchall bus thread (today's
behavior — explicit fallback, not silent drop).

### R2 — Outbound stamps every post AND fans out multi-audience

`discord_push.try_push` (existing — spec 023) is extended:

**Per-audience fan-out** (NEW for the per-agent-channels architecture):
- For each audience id in the bus message's `audience` field, look
  up the route via `bus_routes_resolve(audience=<single>)`
- Post once per resolved channel (e.g., `audience=ares,clawta` →
  one post in #ares + one post in #clawta)
- All N fan-out posts share the SAME `octi.message.v1` anchor
  (R1.b zero-width form on first post, R1.a `message_reference`
  on subsequent replies-to-same-bus-message)
- Record N rows in `discord_dedup`, one per channel, all pointing
  to the same `bus_message_id`

**Anchor stamping rules**:
1. If the bus message has a parent_id within an existing thread that
   has at least one prior Discord-mirrored message in the same
   channel:
   - Use Discord `message_reference` pointing to that channel's
     prior mirrored message
2. Else (first message in this channel for this thread):
   - Append the zero-width anchor (R1.b) with the bus
     `thread_id` + `message_id`
3. After successful Discord post(s), write one
   `messages.octi_anchor` entry per channel via the dedup table
   (R4 already supports multi-channel via PRIMARY KEY)

**Single-audience case is the degenerate fan-out of N=1.** The
spec's pre-channel-revision behavior is preserved unchanged when
the bus message targets exactly one audience.

### R2a — Outbound mentions resolve to Discord user entities

Before any bus-originated body is persisted for a Discord-mirrored
thread or sent via `discord_push.try_push`, the bus resolves known
agent mentions to native Discord syntax:

```text
@clawta / @Clawta / @CLAWTA -> <@{clawta.discord_user_id}>
@ares / @Ares / @hermes     -> <@{ares.discord_user_id}>
```

The exact user IDs are loaded from the same canonical identity
configuration that defines the per-agent routes. The config MUST
distinguish:

- `agent_id` — stable bus/runtime identity (`clawta`, `ares`)
- `discord_channel_id` — where that agent's mirrored messages land
- `discord_user_id` — the user entity to mention in Discord content
- `display_aliases` — accepted human text aliases, e.g.
  `@clawta`, `@Clawta`, `@hermes`

The resolver MUST:

1. Rewrite only whole mention tokens, never emails, URLs,
   `@clawta-poller`, or unknown handles.
2. Preserve existing native Discord mentions (`<@id>` and `<@!id>`)
   unchanged.
3. Convert aliases before truncation so the pushed Discord payload and
   stored bus body agree.
4. Fail closed for configured agents with no `discord_user_id`: leave
   the text unchanged and emit structured log
   `octi.identity.unresolved_discord_user_id` with the missing
   `agent_id`.
5. Treat display-name canonicalization as a compatibility fallback
   only for non-Discord sinks; it MUST NOT be the Discord push path.

This requirement replaces the older "canonical case" invariant that
assumed `@Clawta` was sufficient to notify. It is not sufficient.
Discord notifications require the entity form.

### R3 — Inbound resolves anchor → bus thread

`discord_mirror.py` poll path is extended:

1. For each inbound Discord message:
   a. Check `message_reference.message_id` against the
      `discord_dedup` table → resolves to `bus_thread_id`
   b. Else, parse the message body for `​<octi:thread=...;v=1>​`
      → resolves to `bus_thread_id`
   c. Else, route to the channel's **agent-owned catchall** —
      i.e., a per-agent bus thread (`#ares_unsolicited`,
      `#clawta_unsolicited`) that the agent uses for freeform
      operator-to-agent comms with no specific RFP context
2. Insert the new bus message with `thread_id` from step 1

**Resolved by architecture, not code**: the prior "shared
channel catchall" failure mode is gone since 2026-05-19 — there is
no shared channel. Each agent's catchall is its own and routes
unambiguously.

### R4 — Dedup table

`discord_dedup` table:

```sql
CREATE TABLE discord_dedup (
    channel_id            TEXT NOT NULL,
    discord_message_id    TEXT NOT NULL,
    bus_message_id        INTEGER NOT NULL REFERENCES messages(id),
    bus_thread_id         INTEGER NOT NULL REFERENCES threads(id),
    direction             TEXT NOT NULL CHECK (direction IN ('outbound','inbound')),
    inserted_at           INTEGER NOT NULL,
    PRIMARY KEY (channel_id, discord_message_id)
);
CREATE INDEX idx_discord_dedup_bus_thread ON discord_dedup(bus_thread_id);
```

Every outbound + inbound message records its (channel_id,
discord_message_id) → bus mapping. Repeat polls hit the PRIMARY KEY
constraint and are no-ops (idempotent).

### R5 — Migration backfills existing threads

`migrations/042-add-message-identity.sql` adds the new column +
table. `identity/migration.py` is a one-shot backfill that:

1. Walks every `messages` row with a non-null `discord_message_id`
2. Inserts the corresponding `discord_dedup` row
3. Idempotent — re-running is safe

CI runs the backfill in a fresh sqlite fixture and asserts no
collisions.

### R6 — Retries are safe

The inbound poller (per spec 023's `agent-bus-inbound-poll` cron)
re-polls the most recent N messages on each tick to recover from
missed messages. R4's PRIMARY KEY constraint makes each retry
idempotent — the second insert is a no-op, no duplicate bus
messages.

### R7 — Anchor is the only routing signal — no heuristics

The inbound resolver MUST NOT guess thread membership from
content, author, recency, or thread title. Either the anchor
resolves, or the message falls through to the catchall. Heuristic
routing creates non-determinism (Ares' core thesis-violation
worry). Explicit > implicit, every time.

### R8 — Anchor-resolution latency

p99 anchor lookup (dedup table query) MUST be < 1ms. Measured in
`test_inbound_thread_routing.py` against a fixture of 10k dedup
rows. Index per R4 enforces.

### R9 — Diagnostic surface

`bus_resolve_anchor(discord_message_id)` is exposed via MCP and
returns:

```json
{
  "bus_thread_id": 17,
  "bus_message_id": 7679,
  "direction": "outbound",
  "inserted_at": 1779235623
}
```

Operator runs `chitin-kernel mcp call bus_resolve_anchor
<discord_message_id>` to debug "where did this message route" issues.

`bus_resolve_mention(agent_id)` is exposed for the same diagnostic
surface and returns the configured native mention:

```json
{
  "agent_id": "clawta",
  "discord_user_id": "123456789012345678",
  "native_mention": "<@123456789012345678>",
  "aliases": ["@clawta", "@Clawta"]
}
```

### R10 — Octi consumers reference this spec, not the underlying
mirror

Spec 047 (mention routing workflow) and any later Octi-side consumer
that reads inbound Discord messages MUST consume them via the bus
contract defined here — not by reading the Discord API directly.
This keeps Octi backend-agnostic about Discord, and keeps the
identity contract centralized.

## Acceptance criteria

1. A new bus thread created via `bus_post_thread(..., audience='ares,clawta')`
   **fans out** (R2): one post in `#ares` AND one in `#clawta`, both
   carrying the same R1.b zero-width anchor.
2. A reply created via `bus_reply(thread_id=N, ...)` posts to each
   audience's channel using Discord's `message_reference` (R1.a)
   pointing to that channel's prior bus-originated message.
3. A Discord user replying (via Discord's native reply UI) to a
   bus-originated message lands in the **same bus thread**, not
   the agent's catchall. Verified by e2e fixture: post to a bus
   thread, send a Discord reply in #ares, assert the new bus
   message carries the originating `thread_id`.
4. The reproducer that motivated this spec is fixed: Ares-style
   Discord-originating replies to RFP threads route to the RFP
   bus thread, not a catchall. e2e test
   `test_bidirectional_identity_e2e.py` replays the exact failure
   scenario (thread 17 / thread 19 → catchall thread 1).
5. A Discord user posting freeform in `#ares` (no reply target,
   no anchor) lands in the **#ares agent-owned catchall** bus
   thread — preserved fallback behavior, per-agent (R3).
6. Re-running the inbound poll over the same window produces zero
   duplicate bus messages (R6 idempotency).
7. `bus_resolve_anchor(<discord_msg_id>)` returns the correct
   `bus_thread_id` + `bus_message_id` for both inbound and
   outbound messages.
8. Bus-originated content containing `@clawta`, `@Clawta`,
   `@ARES`, or `@hermes` is stored and pushed as the configured
   Discord native mention (`<@user_id>`), while emails, URLs,
   hyphenated identifiers, unknown handles, and already-native
   mentions are unchanged.
9. `bus_resolve_mention("clawta")` and
   `bus_resolve_mention("ares")` return configured
   `discord_user_id` values and native `<@...>` strings.
10. `migrations/042-add-message-identity.sql` + backfill complete
   on a 10k-row fixture in < 5 seconds with no row loss.
11. p99 anchor lookup < 1ms over 10k dedup rows.
12. CI gate: any change to `discord_push.py` or `discord_mirror.py`
    that doesn't also update or reference the identity contract
    fails review (PR-template checklist).

## Test coverage

- `services/agent-bus/tests/test_identity_anchor.py` — unit: R1
  encoding/decoding, R7 no-heuristic-routing
- `services/agent-bus/tests/test_inbound_thread_routing.py` — unit:
  R3 resolution + R6 idempotency
- `services/agent-bus/tests/test_outbound_anchor_stamp.py` — unit:
  R2 stamping (both reply and first-message paths)
- `services/agent-bus/tests/test_discord_mention_entities.py` —
  unit: R2a alias-to-`<@user_id>` rewriting and non-mention
  passthrough
- `services/agent-bus/tests/test_dedup_migration.py` — unit: R5
  backfill correctness + idempotency
- `services/agent-bus/tests/test_bidirectional_identity_e2e.py` —
  **e2e**: AC3, AC4, AC5, AC6 — the full reproducer for the
  thread-17-vs-thread-1 bug
- `services/agent-bus/tests/test_anchor_lookup_perf.py` —
  **e2e**: AC9 latency

All test files carry `// spec: 042-octi-agentbus-identity-contract`
(or `# spec:` for Python) per spec 020 §1.1.

## Invariants

- **I1**: every cross-boundary message has a deterministic identity.
  No heuristic routing, ever.
- **I2**: idempotent inbound — re-polling the same Discord window
  never creates duplicate bus messages.
- **I3**: anchor format is frozen at v1. Schema evolution is by v2
  parallel encoding, not in-place change.
- **I4**: fallback to channel catchall is **explicit and observable**
  — when it happens, a structured log line
  `octi.identity.fallback_to_catchall` is emitted.
- **I5**: Octi workflows consume Discord via the bus contract here,
  never via the Discord API directly.
- **I6**: outbound Discord mentions are native user entities. Raw
  display-name text (`@Clawta`) is accepted as input, but is not the
  Discord projection.

## Out of scope

- Cross-channel message routing (e.g., move a message between
  `#swarm` and `#hermes`) — anchor is per-channel; cross-channel is
  a separate motion
- Reactions, edits, deletions — Discord side ops on existing
  messages; spec 023's mirror handles them in the abstract; this
  spec doesn't add new behavior there
- Anchor encryption / signing — the anchor is in plaintext; tamper
  resistance is provided by the dedup table (a forged anchor still
  hits a real bus_thread_id only if the attacker can write to the
  table, which requires kernel-gate access)
- Migrating away from sqlite — spec 048 (HA) addresses

## References

- Closes: Clawta critique #2, agent-bus thread 17 msg 7690
- Live reproducer: agent-bus thread 17 msgs 7685-7687 (Ares' Discord
  reply landed in thread 1)
- Parent surface: spec 023 (agent-bus bidirectional liveness)
- Predecessor identity-related spec: spec 021 (outbound stale-env)
- Discord message reference docs:
  https://discord.com/developers/docs/resources/channel#message-object-message-reference-structure

## Appendix A — Channel-removal migration plan (2026-05-19)

> Operator deleted #swarm, #mini, #hermes on 2026-05-19 ~20:50 EDT.
> Remaining channels: **#ares** (1503438297597350062), **#clawta**
> (1503439202719760405). This appendix folds in the migration
> steps that were originally task 23.

### A.1 — Bus routes (completed 2026-05-19 ~20:48 EDT)

| Action | Result |
|---|---|
| `bus_routes_set audience=ares channel=1503438297597350062` | added |
| `bus_routes_unset audience=mini` | removed (channel deleted) |
| `bus_routes_unset board=chitin` | removed (#swarm deleted) |
| `bus_routes_unset global=*` | removed (#swarm deleted) |

Final route table:
- `audience=ares` → #ares
- `audience=hermes` → #ares (alias — runtime ID)
- `audience=clawta` → #clawta

### A.2 — Historical thread cleanup (one-time)

Bus threads with `discord_thread_id` referencing a deleted channel
(e.g., thread 17, thread 19 mirrored to #swarm 1505613628286701588)
keep their bus content but lose their Discord visibility. The bus
content remains queryable via MCP / console.

**No automated cleanup**. The threads are history; deleting them
would lose audit trail. They stay in the bus with their
`discord_thread_id` pointing at the deleted channel — a one-line
NULL check in any future Discord-side reader is the only adjustment.

### A.3 — Multi-audience fan-out implementation (in scope for v1)

Per R2 of this spec: when a bus message has `audience=ares,clawta`,
`discord_push.try_push` posts once to #ares AND once to #clawta,
all sharing the same `octi.message.v1` anchor. e2e test
`test_bidirectional_identity_e2e.py` covers the new fan-out path.

### A.4 — What the operator does NOT need to do

The operator's "kill #swarm" directive is fully expressed by:
1. Discord-side channel deletions (done)
2. Bus route cleanup (done)
3. Spec 042 + 047 re-scope (done)
4. Spec 049 inter-agent-comms-only-via-artifacts requirement
   (done in spec 049 §R9)

No further operator action required. Implementation work to honor
the contract is captured in this spec's AC + Test coverage
sections.
