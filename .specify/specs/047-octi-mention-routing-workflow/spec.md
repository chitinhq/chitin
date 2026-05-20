# 047 — Octi mention routing workflow (closes Clawta critique #3)

> Parent: spec 040 (Octi scaffolding).
> Depends on: 042 (agent-bus identity contract).
> Migration target:
> `swarm/bin/clawta-mention-listener`,
> `swarm/bin/mini-mention-listener`,
> `swarm/bin/install-clawta-mention-listener.sh`,
> `swarm/bin/install-mini-mention-listener.sh`.

## Summary

**2026-05-19 channel architecture revision**: with the operator
deleting #swarm + #mini + #hermes and keeping only **#ares** and
**#clawta** (each owned by exactly one agent), the cross-channel
mention-routing problem is largely **architecturally dissolved**.
There is no longer a shared channel where `@clawta` could appear in
front of Ares and vice versa. Each agent listens to its own channel
only.

What this spec still owns under the new architecture:

1. **Own-channel listener** — each agent's mention-listener workflow
   subscribes to its own per-agent Discord channel + own bus
   inbox. No cross-channel polling.
2. **DLQ** for messages in own channel that don't belong (bot
   spam, misdirected @-mentions, operator typos addressing the
   other agent — those are dead-lettered with reason, never
   silently dropped, never silently auto-routed).
3. **Owner-preserving guarantee** (Clawta critique #3) preserved by
   the architecture itself: `@clawta` posted in #ares cannot be
   processed by Clawta (Clawta doesn't listen there); operator must
   re-post in #clawta. This makes the channel boundary the
   authority surface.

The original goal — replace `swarm/bin/clawta-mention-listener`
and `swarm/bin/mini-mention-listener` crons with deterministic
workflows — stands; the cross-channel scope shrinks dramatically.

> The pre-channel-revision text of this spec is preserved below as
> §A1 "historical context" for reference; v1 implementation
> proceeds against the revised summary above.

## Ticket refs

- Closes: Clawta critique #3 (agent-bus thread 17, msg 7690).
- Migration targets: `swarm/bin/clawta-mention-listener`,
  `swarm/bin/mini-mention-listener`,
  `swarm/bin/install-*-mention-listener.sh`
- Related: spec 039 (mini-discord-inbound) — its regex fix lives in
  the Mini repo and is consumed by this workflow as the
  per-agent matcher

## File-system scope

### MAY write under

- `swarm/octi/workflows/mention.go` — `MentionRoutingWorkflow`
- `swarm/octi/activities/mention/` — Activity packages
  - `match_mention.go` — applies the per-agent regex to a message
  - `dispatch_to_owner.go` — sends the message to the owning agent's
    task queue
  - `dlq.go` — dead-letter for un-owned mentions
- `swarm/octi/config/mention_ownership.yaml` — frozen ownership
  table (R2)
- `swarm/octi/workflows/mention_test.go` — unit
- `swarm/octi/e2e/mention_routing_test.go` — **e2e**: each
  agent receives only its own mentions
- `swarm/octi/cmd/octi-mention-replay/main.go` — replay tool
  for debugging mis-routes
- `.specify/specs/047-octi-mention-routing-workflow/**`

### MUST NOT write under

- Legacy mention-listener scripts (kept until bake; removed by
  installer)
- Per-agent regex implementations — owned by each agent's repo;
  this workflow imports them as Activities

## Goal

A Discord user posts `@Clawta please check the build` in `#swarm`.
The mention workflow receives the message via spec 042's bus →
inbound pipeline, matches `@Clawta` against the ownership table,
and dispatches the message to Clawta's task queue with full bus
thread context. **Mini never sees it. Ares never sees it.** The
workflow recorded "matched @Clawta, dispatched to clawta-py task
queue" as an OctiEvent. Mis-routes are dead-lettered with a
structured reason; they never silently disappear, and they never
trigger an agent to respond to a message not addressed to it.

## Requirements

### R1 — Workflow signature + lifecycle

```go
func MentionRoutingWorkflow(
    ctx workflow.Context,
    input MentionRoutingInput,
) error

type MentionRoutingInput struct {
    Board string `json:"board"` // optional; null = all boards
}

// Signal payload
type InboundMessageSignal struct {
    BusMessageID   int64  `json:"bus_message_id"`
    BusThreadID    int64  `json:"bus_thread_id"`
    Author         string `json:"author"`
    Body           string `json:"body"`
    DiscordMsgID   string `json:"discord_message_id,omitempty"`
    ChannelID      string `json:"channel_id,omitempty"`
    Timestamp      int64  `json:"ts_unix_ns"`
}
```

Long-lived workflow per board (or one global). Selector loop on
`InboundMessageSignal`. `ContinueAsNew` every 1000 signals.

### R2 — Channel ownership table (frozen v1, post-channel-revision)

`swarm/octi/config/channel_ownership.yaml`:

```yaml
schema: octi.channel_ownership.v1
channels:
  - channel_id: "1503438297597350062"  # #ares
    owner: ares
    task_queue: hermes-py
    accepts_mentions: ["@ares", "@hermes"]   # both map to Ares (runtime ID = hermes)
    matcher: word_boundary_case_insensitive
  - channel_id: "1503439202719760405"  # #clawta
    owner: clawta
    task_queue: clawta-py
    accepts_mentions: ["@Clawta", "@clawta"]
    matcher: word_boundary_case_insensitive
```

The table is **frozen at v1**. Adding a channel or changing an
owner requires `octi.channel_ownership.v2`.

Note: `@mini` and `@red` are NOT channel owners — Mini has no
channel (deleted 2026-05-19), and operator (red) reads both
channels but doesn't own one. References to `@mini` in either
channel land in DLQ (R5).

### R3 — One signal → one owner → one dispatch

For each `InboundMessageSignal`:
1. Iterate ownership table in declared order
2. Apply each entry's matcher to the message body
3. **First match wins** — stable, deterministic
4. Dispatch the message to that owner's task queue via a child
   Activity `DispatchToOwnerActivity`
5. If no match, send to DLQ Activity

Multiple matches in one message → first table entry wins. Logged
as `multi_mention_first_win` with all matched owners listed.

### R4 — No shared responder loop

The owning agent is the **only** consumer of a mention. The
workflow MUST NOT broadcast to multiple agents. CI gate
(`octi-no-broadcast-mention.yml`) greps Activity code for any
`for _, owner := range owners { dispatch(...) }` shape and fails.

This is the literal implementation of Clawta critique #3.

### R5 — DLQ for un-owned mentions

`DLQActivity` writes to `~/.chitin/octi-mention-dlq-YYYY-MM-DD.jsonl`:
```json
{
  "bus_message_id": 7679,
  "body_excerpt": "...@bogus please...",
  "reason": "no_owner_matched",
  "ts": 1779235623
}
```

Operator inspects DLQ via `octi mention dlq` CLI command. DLQ
entries are NOT auto-retried — a mention with no owner is a config
gap, not a transient error.

### R6 — Owner-side contract

The owning agent's task queue worker (e.g., the Python `clawta-py`
worker) registers an Activity `OnMentionedActivity` with input:

```go
type OnMentionedInput struct {
    BusMessageID   int64
    BusThreadID    int64
    OriginalAuthor string
    Body           string
    DiscordMsgID   string
    ChannelID      string
    ReceivedAt     int64
}
```

What the agent does with the mention is the agent's concern — but
it MUST emit a reply (or an explicit "no response" signal) within
60s, or the workflow marks the dispatch `unacknowledged` and
emits a structured warning.

### R7 — Spec 039 Mini regex consumed unchanged

Mini's mention matcher (per spec 039 — addressing punctuation
required) lives in the Mini repo. This workflow imports it as a
Go interface (`MentionMatcher`) wrapping the Mini regex; no
duplication. If Mini's regex changes, this workflow's matcher
behavior changes accordingly — a single source of truth.

### R8 — Migration cutover

`install-octi-mention.sh --migrate`:
1. Disables `clawta-mention-listener` cron
2. Disables `mini-mention-listener` cron
3. Starts `MentionRoutingWorkflow`
4. Asserts within 60s the workflow has received at least one
   signal (using a synthetic test mention)
5. `--rollback` reverses

### R9 — Determinism

`workflow.Selector` for signal handling. Ownership table iteration
is slice order (frozen yaml). Time only via `workflow.Now(ctx)`.
`workflowcheck` passes.

### R10 — Bake parity

Over a 30-day bake period, the workflow runs in shadow mode
alongside the legacy mention-listeners. CI test compares routing
decisions over a fixture of 1000 historical mentions; ≥99% match
expected (the legacy listeners' regex bugs — including the spec
039 root cause — are EXPECTED divergences; those are improvements,
not failures).

## Acceptance criteria

1. A bus message containing `@Clawta` produces exactly one
   dispatch to the `clawta-py` task queue; no dispatch to
   `mini-cli` or `hermes-py`.
2. A bus message containing `@mini` produces exactly one dispatch
   to `mini-cli`.
3. A message with both `@mini` and `@Clawta` produces a dispatch
   to the **first-table-listed** owner (mini per R2 ordering) AND a
   structured `multi_mention_first_win` log.
4. A message with `@bogus` (no owner) lands in
   `~/.chitin/octi-mention-dlq-*.jsonl` with reason
   `no_owner_matched`.
5. Mini regex (spec 039) is the ONLY matcher used for `@mini`;
   verified by import graph + behavior fixture.
6. `octi-no-broadcast-mention.yml` CI gate refuses any code that
   would dispatch a single mention to multiple owners.
7. Owner acknowledgment latency: signal sent → owner Activity
   completes within 60s, OR `unacknowledged` warning surfaces.
8. Bake parity ≥99% over 1000 historical mentions; expected
   divergences documented.
9. `--migrate` disables both legacy mention-listener crons; one
   workflow replaces both.
10. `MentionRoutingWorkflow` passes `workflowcheck`; ownership
    table is read once at workflow start (Activity, not workflow
    code) to keep workflow deterministic across re-deploys.

## Test coverage

- `swarm/octi/workflows/mention_test.go` — unit
- `swarm/octi/activities/mention/*_test.go` — unit per matcher
- `swarm/octi/e2e/mention_routing_test.go` — **e2e**: AC1, AC2,
  AC3, AC4
- `swarm/octi/e2e/mention_no_broadcast_test.go` — **e2e**: AC6
- `swarm/octi/e2e/mention_parity_test.go` — **e2e**: AC8
- `swarm/octi/e2e/mention_dlq_test.go` — **e2e**: AC4

All files carry `// spec: 047-octi-mention-routing-workflow`.

## Invariants

- **I1** (closes Clawta critique #3): one mention → one owner →
  one dispatch. No shared responder loop, ever.
- **I2**: ownership table is the single source of truth. No
  per-agent local override.
- **I3**: un-owned mentions are dead-lettered, not silently
  dropped, and not auto-routed to "closest match."
- **I4**: spec 039 mini regex is reused, not duplicated.
- **I5**: deterministic first-match-wins on multi-mention messages.

## Out of scope

- New mention patterns or agent identities (frozen at v1; v2 spec
  for changes)
- Auto-learning the ownership table from past responses — explicit
  config only
- Reaction-based "this is for me" overrides — frozen table only
- Cross-platform mentions (e.g., Slack, Telegram) — Discord +
  bus only in v1

## References

- Closes: Clawta critique #3, agent-bus thread 17 msg 7690
- Migration targets:
  `swarm/bin/clawta-mention-listener`,
  `swarm/bin/mini-mention-listener`
- Mini regex source: spec 039 (mini-discord-inbound)
- Identity routing: spec 042 (provides the bus → workflow signal
  pipe)
- Parent: spec 040
