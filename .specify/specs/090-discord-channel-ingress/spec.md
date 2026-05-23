# Feature Specification: Discord channel-ingress for @clawta

**Feature Branch**: `feat/090-discord-channel-ingress`

**Created**: 2026-05-22

**Status**: Draft

**Input**: User description: "Clawta only DMs me and never responds in the #clawta channel. The DM ingress works (openclaw-gateway is logged in as clawta's bot; Discord pushes DMs directly to the WebSocket; the existing `agentId: clawta` route fires). The channel-ingress path used to live in `clawta-mention-listener` polling `~/.chitin/agent-bus/bus.db` — that listener is the spec 088 cull target because its substrate (agent-bus) was decommissioned by spec 069. Build the replacement: have openclaw-gateway natively subscribe to guild-message events from #clawta, detect @-mentions, and route them through the same route rule DMs already use."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — @clawta in #clawta gets a response (Priority: P1)

An operator @-mentions @clawta in the `#clawta` Discord channel. Within roughly 30 seconds, Clawta replies in the same channel — same agent identity, same governance gate, same chain-event recording as a DM response would produce. The operator no longer has to DM Clawta to get answers; the public channel becomes a viable surface.

**Why this priority**: it's the only user story — without it the feature has no value. The current state ("Clawta DMs me but never responds in #clawta") is exactly the gap this closes.

**Independent test**: post the message `@clawta what time is it?` in `#clawta`. Within 60 seconds, Clawta posts a reply in `#clawta`. A new `events-openclaw-clawta-*.jsonl` session opens on the operator box with the channel name in the chain context. No `bus_db_missing` line appears in any log for this invocation (the path bypasses the dead mention-listener entirely).

---

### Edge Cases

- **The bot isn't a member of `#clawta`** — the spec assumes Clawta's bot/user account is already in the channel (and was in the channel under the old listener too). If not, an operator-side prerequisite is documented: "invite the bot to the channel before this feature works."
- **An operator @-mentions Clawta in a channel that *isn't* #clawta** — implementation phase decides scope: only `#clawta`, or any channel the bot is a member of, or a configurable allowlist. The default for this spec is "only #clawta initially; broaden in a follow-up if wanted."
- **Two @-mentions arrive in quick succession** — each spawns its own session, same as DMs do today. No deduplication / batching in this spec.
- **A bot's own message contains "@clawta"** — must be ignored (don't auto-respond to bots, including Clawta itself).
- **The lockdown-loop bug fires mid-response** (spec 091) — orthogonal; this spec ships independently of 091. A reply that's interrupted by lockdown is still a "Clawta responded in the channel" outcome from the gateway's perspective.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: openclaw-gateway MUST subscribe to Discord guild-message events for the `#clawta` channel — at minimum the `GUILD_MESSAGES` gateway intent enabled, and the bot/user account joined to the channel (operator-side prerequisite, documented in the quickstart).
- **FR-002**: An incoming guild-message in a subscribed channel MUST be inspected for an `@clawta` mention (either the text form or Discord's native `<@id>` form for Clawta's user ID — same detection logic the retired `clawta-mention-listener` carried). Messages without an `@clawta` mention MUST be ignored.
- **FR-003**: A detected `@clawta` mention in a subscribed channel MUST be routed through the existing `agentId: clawta` route in openclaw.json — same path DMs use. No new route rule; no new dispatch surface; reuses the existing dispatch.
- **FR-004**: The reply MUST be posted back to the same channel (not as a DM) — replies go where the mention came from.
- **FR-005**: Messages from bots (including Clawta itself) MUST be skipped — don't auto-respond to the bot's own messages.
- **FR-006**: This feature MUST NOT depend on `~/.chitin/agent-bus/bus.db`, the retired clawta-mention-listener, or any agent-bus successor. The new path is direct: Discord WebSocket → openclaw-gateway → route → Clawta.
- **FR-007**: The chain event recorded for a channel-mention invocation MUST carry enough context (channel name or id) for an operator to retrace the conversation post-hoc — same telemetry shape as DMs but with the channel coordinate filled in.

### Success Criteria *(mandatory)*

- **SC-001 (Channel works)**: an `@clawta` message in `#clawta` produces a reply in `#clawta` within 60 seconds on a freshly-restarted openclaw-gateway. Measured by a manual operator probe; can be automated as a smoke test if a Discord-bot-test fixture exists.
- **SC-002 (DM unchanged)**: existing DM behavior continues to work — operator DMs Clawta, gets a reply in DM, no regression in latency or content. Same probe pattern.
- **SC-003 (No bus_db_missing for channel invocations)**: after this feature ships, `~/.openclaw/logs/clawta-mention-listener.log` shows no NEW `bus_db_missing` lines tied to channel-mention processing. (The cron may still spam the line until spec 088 retires it, but channel-mention processing no longer flows through that path.)
- **SC-004 (Chain telemetry includes channel coordinate)**: a chain event for a channel-mention invocation includes the channel name/id in the session metadata. `jq '.payload | select(.channel)' ~/.chitin/events-openclaw-clawta-*.jsonl` returns at least one match for any channel-sourced session.

## Assumptions

- openclaw-gateway is built on top of a Discord client library that supports gateway intents and channel-message events. Adjusting intents is a config change, not a new dependency.
- The bot/user account Clawta uses is already a member of `#clawta`. If it isn't, the operator adds it as a prerequisite (single one-time setup).
- The chitin governance gate continues to apply to channel-response exec calls the same way it does for DMs. Channel mentions are not a governance bypass.
- Spec 088 (mention-listener cull) and spec 091 (lockdown loop fix) ship independently; this spec is decoupled from both. 088 removes the dead pipe; 090 builds the replacement; 091 fixes the harness honoring of `continue:false`.

### Scope

**In scope**:
- openclaw-gateway changes to subscribe to guild-messages from `#clawta`
- @-mention detection (text + Discord `<@id>` form)
- Routing through existing `agentId: clawta` rule
- Chain telemetry: include channel coordinate
- Operator-side prerequisite doc (channel membership, intent enablement)

**Out of scope**:
- Multi-channel support (just `#clawta` initially)
- A new dispatch substrate (reuses the existing one)
- The dead `clawta-mention-listener` cron (spec 088's territory)
- The lockdown-loop harness bug (spec 091)
- Migration of agent-bus historical data (decommissioned by spec 069)
- DM behavior changes (unchanged)

### Dependencies

- **Prerequisite (operational)**: the Discord bot must have the `GUILD_MESSAGES` gateway intent enabled in Discord's developer portal, AND be a member of `#clawta` with Read Messages permission. If either is missing, the implementer surfaces it as an operator-side step.
- **No code dependencies on other in-flight specs.** Spec 088 (cull) and spec 091 (lockdown) ship in parallel; their merges are independent of this one.
