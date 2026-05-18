# pos-002 — Channel routing fix: bus self-echo dedup + cron mis-route

> Lane: **Clawta** (implementation) · author: red · pending lane ratification
> Branch: `clawta/pos-002-channel-routing-spec`
> Board: `personal-os`
> Companion specs: `001-gateway-watchdog` (Ares lane)

## Problem

Two independent failure modes produce stale or misrouted messages in the agent-bus and Discord integration:

1. **Bus self-echo (mechanical):** Outbound bus messages posted to Discord via `discord_push.try_push` were re-imported by the inbound mirror poller (`discord_mirror.poll_once`), producing duplicate bus rows. The dedup column (`discord_message_id`) was never stamped on the outbound row, so the inbound poller could not recognize the echo. Proof: message 3419 (outbound, `discord_message_id=NULL`) was echoed back as message 3420 (inbound duplicate) in the #swarm production channel.

2. **Channel mis-route (convention):** Three cron-summary jobs (board-watchdog, chain-summary, hermes-clawta-bridge) all deliver to the same Discord channel (`1503438297597350062`, #ares formerly #hermes). When Clawta replies to messages in that channel, Discord's thread-channel inheritance forces the reply to stay in #ares rather than routing to #clawta or #swarm where it belongs. Proof: msg 3411 + 3426 are live evidence of a Clawta reply landing in #ares.

---

## Section A: Bus Self-Echo Dedup Fix

### Metadata

- **pos:** 002
- **slug:** channel-routing-fix
- **component:** services/agent-bus
- **status:** implemented (merged as PR #756, commit `1e269c4`)

### Root Cause Analysis

The dedup mechanism in `discord_mirror.ingest_messages` (line 179-184) checks whether an inbound Discord message is already present in the bus by querying:

```sql
SELECT 1 FROM messages WHERE discord_message_id=? LIMIT 1
```

This filter is sound when `discord_message_id` is populated: if the column contains the Discord snowflake, the inbound poller recognizes the message as already present and skips it.

**The bug:** when an agent sends a message via `bus_reply`, the outbound path was:

1. `server.bus_reply` INSERTs a new `messages` row (line 110-114). The INSERT does **not** include `discord_message_id` — it is left NULL.
2. `discord_push.try_push` POSTs the message to Discord. Before the fix, `try_push` returned `None` unconditionally — it never captured the created message's snowflake from the Discord API response.
3. `_post_via_webhook` sent the webhook request **without `?wait=true`**, receiving a `204 No Content` response with no message object.
4. `_post_via_bot` received the full message object but did not parse or return the `id` field.
5. The `messages.discord_message_id` column remained NULL for the outbound message.
6. On the next `poll_once` cycle, the inbound mirror saw the Discord message (snowflake = X), queried `messages WHERE discord_message_id = 'X'`, found nothing (because X was never stamped), and INSERTed a duplicate row.

**In summary:** the round-trip integrity gap was that `discord_push` discarded the snowflake returned by Discord's API, so the bus DB had no way to link the outbound row to its Discord projection, and the inbound poller treated it as a new message.

### Code Path Where the Snowflake Should Be Stamped

The stamping point is in `server.bus_reply`, immediately after the `try_push` call returns:

```
services/agent-bus/server.py, lines 127-151:

    # Per pos-002 AC5: stamp the returned Discord snowflake back onto
    # the bus row BEFORE the next inbound mirror poll runs.
    snowflake = discord_push.try_push(
        channel_id=channel_id, author=author, body=body
    )
    if snowflake:
        try:
            conn.execute(
                "UPDATE messages SET discord_message_id=? WHERE id=?",
                (snowflake, message_id),
            )
            conn.commit()
        except Exception as exc:
            # Stamp failure is non-fatal — the message is already on
            # both Discord and the bus; it'll just produce a duplicate
            # on the next inbound poll. Log loudly so this surfaces.
            print(
                f"[bus_reply] WARN: failed to stamp snowflake "
                f"{snowflake} onto bus msg {message_id}: {exc}",
                file=__import__("sys").stderr,
            )
```

The two prerequisite changes in `discord_push.py` that make the snowflake available:

1. **`_post_via_webhook`** (lines 132-165): appends `?wait=true` to the webhook URL so Discord returns the full message object (instead of `204 No Content`). Parses `data.get("id")` from the JSON response and returns it.

2. **`_post_via_bot`** (lines 168-194): parses `data.get("id")` from the `POST /channels/{id}/messages` response and returns it.

3. **`try_push`** (lines 197-239): return type changed from `None` to `str | None`. Returns the snowflake from whichever path succeeded, or `None` if no channel/no auth/failure.

### Proposed Fix

**Already implemented and merged** as PR #756 (`1e269c4`). The fix applies three changes:

#### Change 1: Webhook returns snowflake (`discord_push.py`)

`_post_via_webhook` now sends `?wait=true` on the webhook URL and parses the response JSON for `data["id"]`. Returns the snowflake string on success, `None` on failure.

#### Change 2: Bot POST returns snowflake (`discord_push.py`)

`_post_via_bot` now parses `data.get("id")` from the channel message response and returns it. This path already received the full message object; the change is purely extractive.

#### Change 3: `bus_reply` stamps snowflake back (`server.py`)

After `try_push` returns a non-None snowflake, `bus_reply` executes:
```sql
UPDATE messages SET discord_message_id=? WHERE id=?
```
The stamp is wrapped in a try/except — failure is non-fatal (the bus write already succeeded; the only consequence is a potential duplicate on next poll). Failures are logged to stderr at WARN level.

#### Important: `bus_post_thread` does NOT need stamping

`bus_post_thread` (line 58-82) passes `channel_id=None` to `try_push` because a freshly created thread has no `discord_thread_id` yet. `try_push` returns `None` immediately when `channel_id` is falsy. This is correct: the thread-to-channel link is established later (by the inbound poller or operator), at which point new replies on that thread will go through `bus_reply` and get stamped.

### Acceptance Criteria

#### AC5a: Outbound message snowflake stamp

**Given** a bus thread with `discord_thread_id` set to a valid Discord channel,
**When** `bus_reply` is called with a message on that thread,
**Then** the returned Discord snowflake is written to `messages.discord_message_id` on the source row BEFORE `bus_reply` returns.

Testable assertion:
```python
result = bus_reply(conn, author="red", thread_id=thread_id, body="test")
assert result["discord_message_id"] is not None  # snowflake returned
row = conn.execute(
    "SELECT discord_message_id FROM messages WHERE id=?",
    (result["message_id"],),
).fetchone()
assert row[0] == result["discord_message_id"]  # stamped back
```

#### AC5b: Inbound dedup skips stamped messages

**Given** an outbound bus message with `discord_message_id` already stamped,
**When** the inbound mirror polls the same Discord channel and sees that message,
**Then** `ingest_messages` skips it (dedup hit) and no duplicate row is created.

Testable assertion (reproducing the 3419→3420 pattern):
```python
# 1. Agent sends outbound via bus_reply
snowflake = "1505953282919501824"
result = bus_reply(conn, author="red", thread_id=1, body="outbound msg")
# Stamp landed (AC5a already covers this, but confirm for the dedup test):
assert result["discord_message_id"] == snowflake

# 2. Inbound poll sees the same Discord message
msgs = [{"id": snowflake, "content": "outbound msg", "author": {"username": "red"}}]
ingested = ingest_messages(conn, thread_id=1, messages=msgs)
# 3. Must be 0 (dedup skip), NOT 1 (would be the 3419→3420 echo bug)
assert ingested == 0, f"expected dedup skip, got {ingested} new rows"
```

#### AC5c: Stamp failure is non-fatal

**Given** a database that rejects the UPDATE (e.g., read-only connection),
**When** `bus_reply` calls `try_push` and gets a snowflake,
**Then** the WARN log fires to stderr but `bus_reply` still returns successfully with the message ID.

#### AC5d: `bus_post_thread` path remains unaffected

**Given** `bus_post_thread` with no `discord_thread_id`,
**When** the thread is created and `try_push(channel_id=None)` is called,
**Then** `try_push` returns `None` and no stamp occurs — correct, because the channel link doesn't exist yet.

### Evidence of Live Fix

Smoke-verified in production on 2026-05-18:
- **Before fix:** outbound messages had `discord_message_id = NULL`; inbound mirror re-ingested as duplicate rows.
- **After fix:** outbound message 3730 had snowflake `1505953282919501824` stamped; next mirror cycle produced zero duplicates.

### Files Changed

| File | Change |
|------|--------|
| `services/agent-bus/discord_push.py` | `_post_via_webhook` sends `?wait=true`, parses `id`; `_post_via_bot` parses `id`; `try_push` returns `str\|None` |
| `services/agent-bus/server.py` | `bus_reply` captures snowflake, stamps `messages.discord_message_id` via UPDATE; returns `discord_message_id` in result dict |
| `services/agent-bus/tests/test_bidirectional_liveness_e2e.py` | Existing e2e covers outbound→inbound round trip (AC5b implicitly) |

---

## Section B: Channel Mis-Route Convention Fix

### Root Cause Analysis

#### How cron-summary jobs select their target channel

Each cron job has a `deliver` field that determines where its output is sent. The Hermes scheduler resolves this field at fire time via `_resolve_single_delivery_target()` in `cron/scheduler.py`:

| `deliver` value | Resolution logic | Result |
|---|---|---|
| `"local"` | Suppress auto-delivery entirely | Output saved to job log only |
| `"origin"` | Use the job's `origin` dict (platform + chat_id + thread_id captured at creation time) | Sends to whichever channel/thread the job was created from |
| `"discord"` (bare platform name) | If `origin.platform == "discord"`, use `origin.chat_id`; otherwise fall back to `DISCORD_HOME_CHANNEL` env var | Sends to the Discord home channel (currently `1503438297597350062` = **#ares**) |
| `"discord:CHAT_ID"` or `"discord:CHAT_ID:THREAD_ID"` | Explicit target | Sends to the specified channel/thread |

#### Current routing of the three affected jobs

| Job | `deliver` | `origin.chat_id` | Resolved target | Actual Discord channel |
|---|---|---|---|---|
| `board-watchdog` (388e38b20bd5) | `"discord"` | `1503438297597350062` | Platform matches origin → use origin chat_id | **#ares** (renamed from #hermes) |
| `chain-summary` (65b3b4c43863) | `"discord"` | `1503438297597350062` | Platform matches origin → use origin chat_id | **#ares** |
| `hermes-clawta-bridge` (8544ef19b897) | `"origin"` | `1503438297597350062` | Use origin directly | **#ares** |

All three jobs deliver to the **same channel**: Discord channel `1503438297597350062`. The channel was originally named #hermes and was renamed to #ares. The `chat_name` field in the job origin is stale — some jobs show `"ChitinHQ / #hermes"` and others `"ChitinHQ / #ares"`, but both resolve to the same channel ID.

#### Why Clawta replies inherit the wrong channel

When a cron job fires and delivers output to Discord, the gateway sends the message to channel `1503438297597350062` (#ares). If the job creates a thread (or replies in an existing thread), the thread belongs to #ares. When Clawta sees the message and replies, its reply follows Discord's **thread-channel inheritance**: a reply in a thread stays in the thread's parent channel. Since the thread is in #ares, Clawta's reply also lands in #ares — not in #clawta or #swarm where it belongs.

**Key Discord behavior**: Replies and thread messages inherit the channel of the parent message/thread. There is no mechanism in the Discord API to "move" a reply to a different channel; the only way to route a message to a specific channel is to send it directly to that channel's ID.

Channel ID reference (ChitinHQ Discord guild):

| Channel name | Discord channel ID | Notes |
|---|---|---|
| #ares | `1503438297597350062` | Formerly #hermes. Cron home channel (`DISCORD_HOME_CHANNEL`). |
| #clawta | `1503439202719760405` | Clawta's designated channel. |
| #swarm | `1505613628286701588` | Multi-agent coordination. Has `channel_prompts` config. |
| #icarus | `1506035223928770570` | Icarus channel (not involved in this bug). |

#### Evidence: msg 3411 + 3426 pattern

When `hermes-clawta-bridge` (or `board-watchdog`, or `chain-summary`) posts its cron output to #ares, Clawta picks up the message and responds in the same thread. The Clawta response ends up in #ares because that is the parent channel of the thread. This is the mis-route: Clawta is a dispatch agent whose coordination output should go to #clawta or #swarm, not #ares which is the operator's primary channel.

### Proposed Fix: Channel-Override in Deliver Target

#### Recommended approach: Explicit `deliver` targets per job

The simplest and most robust fix is to **replace `deliver: "discord"` and `deliver: "origin"` with explicit channel targets** for each job, removing dependency on the `origin` field (which is captured at creation time and can become stale or mis-named).

**Why not thread-channel remapping**: Discord does not support moving a message or thread to a different channel after creation. A reply always stays in the thread's parent channel.

**Why not a post-filter**: A post-filter that detects mis-routed messages and resends them to the correct channel adds latency, duplicates messages, and requires state tracking. It's fragile and non-obvious to users.

**Why channel-override in reply payload**: Hermes's gateway `deliver` system already supports explicit `"discord:CHAT_ID"` and `"discord:CHAT_ID:THREAD_ID"` syntax. This is the established routing mechanism. Using it is zero-cost: no new code paths, no heuristics, no post-hoc filtering.

#### Routing table after fix

| Job | Current `deliver` | Proposed `deliver` | Target channel | Rationale |
|---|---|---|---|---|
| `board-watchdog` | `"discord"` | `"discord:1505613628286701588"` | **#swarm** | Watchdog reports are operational coordination → swarm channel |
| `chain-summary` | `"discord"` | `"discord:1503438297597350062"` | **#ares** (unchanged) | Chain governance summaries are operator-facing, belong in the primary channel |
| `hermes-clawta-bridge` | `"origin"` | `"discord:1503439202719760405"` | **#clawta** | Bridge coordination is Clawta's domain; its output belongs in #clawta |

**Why `board-watchdog` → #swarm**: Watchdog reports (promote-demote loops, ticket grooming) are operational coordination messages. They're primarily consumed by agents (Clawta, Ares) monitoring the swarm, not by the operator. #swarm already has a `channel_prompts` entry for multi-agent coordination, making it the natural home.

**Why `chain-summary` stays in #ares**: Chain governance summaries are human-readable reports. The operator needs to see them. #ares is the primary channel. No change needed, but switching to an explicit target makes the routing declarative rather than relying on origin/home-channel coincidence.

**Why `hermes-clawta-bridge` → #clawta**: This is the Clawta bridge agent. Its coordination messages (claiming P0/P1 tickets, escalating failures) are Clawta-domain operations. Sending them to #clawta ensures Clawta's own context channel is where they appear, and **Crucially**: when Clawta replies to a bridge message, the reply stays in #clawta (correct domain), not #ares (wrong domain).

### Complete Affected Job List with Exact Targets

| # | Job ID | Job Name | Current `deliver` | Current Target | Proposed `deliver` | Proposed Target |
|---|---|---|---|---|---|---|
| 1 | `388e38b20bd5` | board-watchdog | `"discord"` | #ares (via origin) | `"discord:1505613628286701588"` | #swarm |
| 2 | `65b3b4c43863` | chain-summary | `"discord"` | #ares (via origin) | `"discord:1503438297597350062"` | #ares (explicit) |
| 3 | `8544ef19b897` | hermes-clawta-bridge | `"origin"` | #ares (via origin) | `"discord:1503439202719760405"` | #clawta |

Other cron jobs with `deliver: "discord"` or `deliver: "origin"` that also route to #ares but are **NOT mis-routed** (they are operator-facing and should stay in #ares):

| # | Job ID | Job Name | `deliver` | Target | Recommendation |
|---|---|---|---|---|---|
| 4 | `84f4226bbdde` | board-audit | `"discord"` | #ares | Keep — operator-facing audit report |
| 5 | `06430879caa7` | doc-sync | `"discord"` | #ares | Keep — operator-facing doc sync |
| 6 | `b23a453ab782` | autonomous-board-engine | `"origin"` | #ares | Keep — operator dispatch |
| 7 | `ad2fc9492509` | blocked-ticket-digest | `"origin"` | #ares | Keep — operator digest |
| 8 | `3bed5c65952a` | chain-governance-canary | `"discord"` | #ares | Keep — operator canary |
| 9 | `ea11e28b814f` | readybench-poller | `"discord"` | #ares | Keep — operational but operator-visible |
| 10 | `f4a320a03a5f` | swarm-retro | `"discord"` | #ares | Keep — operator retrospective |
| 11 | `4f55777ee280` | swarm-standup | `"discord"` | #ares | Keep — operator standup |

### Acceptance Criteria

#### Test: Reproduce msg 3411/3426 pattern

**Pre-fix reproduction:**

1. Trigger `hermes-clawta-bridge` cron job (or simulate its delivery by sending a message to channel `1503438297597350062` / #ares with content matching the bridge output pattern).
2. Observe that Clawta picks up the message and replies in the same thread/channel (#ares).
3. Verify the Clawta reply appears in #ares, NOT in #clawta. This is the mis-route.

**Post-fix verification:**

1. Update `hermes-clawta-bridge` job's `deliver` field from `"origin"` to `"discord:1503439202719760405"`.
2. Update `board-watchdog` job's `deliver` field from `"discord"` to `"discord:1505613628286701588"`.
3. Update `chain-summary` job's `deliver` field from `"discord"` to `"discord:150343829753530062"` (explicit, no behavior change).
4. Trigger each job and verify delivery lands in the correct channel:
   - `hermes-clawta-bridge` output → #clawta (`1503439202719760405`)
   - `board-watchdog` output → #swarm (`1505613628286701588`)
   - `chain-summary` output → #ares (`1503438297597350062`)
5. **Critical**: Verify that Clawta's reply to the `hermes-clawta-bridge` message now appears in #clawta (the same channel the bridge message was posted to), NOT in #ares.

### Implementation Commands

```bash
# Update hermes-clawta-bridge: origin → explicit #clawta
hermes cron update 8544ef19b897 --deliver "discord:1503439202719760405"

# Update board-watchdog: discord → explicit #swarm
hermes cron update 388e38b20bd5 --deliver "discord:1505613628286701588"

# Update chain-summary: discord → explicit #ares (declarative, no behavior change)
hermes cron update 65b3b4c43863 --deliver "discord:1503438297597350062"
```

> **Note**: The `deliver` field also accepts comma-separated multi-target delivery, e.g. `"discord:1503439202719760405,discord:1505613628286701588"` to fan out to both #clawta and #swarm. This may be useful for `board-watchdog` if both channels should receive the report.

### Channel Routing Convention Document

Once the routing fix is deployed, add a `channel_routing` convention document to the workspace `.specify/` directory listing the canonical routing table above, so future cron job creation follows the convention by default rather than relying on `deliver: "origin"` or `deliver: "discord"` (which both resolve to whichever channel the job happened to be created from — a historical accident, not a design choice).

---

## Invariants and Boundaries

- **boundary:** outbound bus messages MUST stamp `discord_message_id` before commit
- **boundary:** Clawta cron-summary replies MUST land in #clawta or #swarm, never #hermes (now #ares)
- **boundary:** failing test = msg 3411 reproduces in current code

These boundaries are restated from the original task specification and are non-negotiable acceptance gates.

---

## Acceptance Test Plan

### Section A (Bus Self-Echo Dedup)

| AC | Description | Test method |
|---|---|---|
| AC5a | Outbound message snowflake stamp | Unit test: `bus_reply` returns `discord_message_id` and it matches the DB row |
| AC5b | Inbound dedup skips stamped messages | Integration test: outbound message with stamped snowflake is not re-ingested by `ingest_messages` |
| AC5c | Stamp failure is non-fatal | Unit test: mock DB that raises on UPDATE → WARN logged, `bus_reply` still succeeds |
| AC5d | `bus_post_thread` path unaffected | Unit test: `bus_post_thread` with `channel_id=None` → `try_push` returns None, no stamp |

### Section B (Channel Mis-Route)

| Step | Action | Expected result |
|---|---|---|
| 1 | Trigger `hermes-clawta-bridge` after `deliver` update | Output appears in #clawta |
| 2 | Trigger `board-watchdog` after `deliver` update | Output appears in #swarm |
| 3 | Trigger `chain-summary` after `deliver` update | Output appears in #ares |
| 4 | Clawta replies to bridge message in #clawta | Reply stays in #clawta, NOT #ares |

### Cross-cutting (Both Sections)

- After bus self-echo fix deploy: run full bidirectional liveness e2e and verify zero duplicate rows for 10+ consecutive outbound messages.
- After channel routing fix deploy: verify no cron job output appears in #ares from jobs that should route elsewhere (scan #ares for `board-watchdog` or `hermes-clawta-bridge` output for 2+ cron cycles).

---

## File-system scope

This spec authorizes changes to:

- `.specify/specs/002-channel-routing-fix/spec.md` (this file)
- `services/agent-bus/discord_push.py` (already merged in PR #756)
- `services/agent-bus/server.py` (already merged in PR #756)
- `services/agent-bus/tests/test_bidirectional_liveness_e2e.py` (existing coverage)

Cron job `deliver` field updates are config changes executed via `hermes cron update`, not file changes.

## Definition of done

- All 4 ACs for Section A pass (bus self-echo dedup).
- Channel routing verification for Section B passes (3 jobs deliver to correct channels).
- No cron job with Clawta involvement delivers to #ares unless it is explicitly operator-facing (chain-summary).
- Channel routing convention document committed to `.specify/`.

<!-- reviewed-spec: pending Clawta lane ratification -->