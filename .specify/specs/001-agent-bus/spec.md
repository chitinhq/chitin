# Feature Specification: Agent-Bus — chitin-owned message bus for swarm + human-in-loop

**Feature Branch**: `feat/agent-bus-mvp`

**Created**: 2026-05-15

**Status**: Draft

**Input**: User description: "build it fully integrated back and forth within our swarm, so it can post messages, show essentially the message threads from discord as well as add agent to agent communication, queue up messages or briefs to read when i go into claude code as human in the loop, and have an mcp server for all agents to also interact with it"

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Agents post + read threaded messages via MCP (Priority: P1)

Every agent (claude-code, hermes, clawta, codex, copilot) can post a top-level thread, reply within a thread, list recent threads filtered by board/audience/status, and read a full thread's message history. Reddit-style threading: each thread has a title + author, each message has an author + optional parent message.

**Why this priority**: This is the floor. Without persistent threaded comms that all agents share, every cross-agent coordination still happens through Discord cron-relays or ad-hoc kanban comments. Once this exists, every other slice (inbox, console UI, Discord mirror, attachments) is additive.

**Independent Test**: Spin up the MCP server, have two agent processes (e.g., a CLI invocation + a test script) post and read a thread, verify they see each other's messages in stable order.

**Acceptance Scenarios**:

1. **Given** an empty agent-bus, **When** agent `red` posts a thread titled "watchdog change request" with body "@hermes please clear loop counter," **Then** the thread is durably stored, `bus_list_threads()` returns it for any caller, and `bus_read_thread(id)` returns the title + that initial message.
2. **Given** an existing thread, **When** agent `hermes` posts a reply, **Then** the reply appears in `bus_read_thread(id).messages` in created-at order and the thread's `updated_at` advances.
3. **Given** a thread with `audience='hermes'`, **When** agent `clawta` calls `bus_list_threads(audience='clawta')`, **Then** the thread is excluded from the list (audience filter respected).

### User Story 2 — Operator inbox surfaces unread directives at session start (Priority: P2)

When a human (operator `red`) starts a Claude Code session in chitin, the agent-bus inbox shows any unread messages addressed to them — directives from agents, briefs from hermes, etc. — so context surfaces automatically without manual polling.

**Why this priority**: This is the human-in-loop unlock. Today, briefs and asks land in Discord; the operator must context-switch to find them. With inbox-at-session-start, the operator opens Claude Code and immediately sees what's pending.

**Independent Test**: Start a Claude Code session in chitin with at least one unread `audience='red'` message; verify the inbox appears as the first system context.

**Acceptance Scenarios**:

1. **Given** 3 unread messages addressed to `red`, **When** the operator starts a Claude Code session, **Then** the session-start hook injects a summary (count + top 3 by recency) into the system context.
2. **Given** the operator reads a message, **When** they call `bus_mark_read(message_id)` (or the inbox auto-marks on display), **Then** that message no longer appears in the next session's inbox.

### User Story 3 — Console UI: list, view, compose threads (Priority: P3)

The chitin-console (Angular) gains a `/threads` route showing the thread list (filterable by board, status, audience), a thread detail view with messages rendered in order, and a compose form for new threads + replies. Same data store the MCP server uses.

**Why this priority**: Operators want a real UI for browsing history, not just the inbox blurb at session start. Console-UI parity with Jira/Linear's discussion view but for the swarm.

**Independent Test**: Open the console, navigate to `/threads`, see the thread list, click into a thread, send a reply, verify the reply lands and is visible to MCP callers.

**Acceptance Scenarios**:

1. **Given** 5 threads in the bus, **When** the operator visits `/threads`, **Then** they see 5 rows with title, author, last-update, board, status.
2. **Given** a thread detail view, **When** the operator posts a reply via the compose form, **Then** the reply renders in the thread and is visible to a subsequent `bus_read_thread()` from the MCP.

### User Story 4 — Discord bidirectional mirror (Priority: P4)

Threads can be linked to a Discord channel/thread. Messages posted in the linked Discord thread are ingested into the bus (read mirror). Messages posted in the bus can be auto-forwarded to the linked Discord thread (write mirror). Both directions are opt-in per thread.

**Why this priority**: Today's directive to hermes had to go through a one-shot cron hack. With Discord mirroring, agent-bus threads become first-class in both surfaces.

**Independent Test**: Link a bus thread to a test Discord channel; post a message in Discord, verify it appears in the bus; post a message in the bus, verify it appears in Discord.

**Acceptance Scenarios**:

1. **Given** a bus thread linked to `discord_thread_id=12345`, **When** a Discord user posts in 12345, **Then** the message appears in `bus_read_thread()` within ~30s.
2. **Given** the same linked thread, **When** an agent posts via `bus_reply()`, **Then** the message also appears in Discord 12345.

### User Story 5 — Attach / link specs, PRs, kanban tickets, files (Priority: P5)

Every thread can carry typed links: spec-kit specs (`.specify/specs/NNN-slug/`), GitHub PRs, kanban tickets, repo file paths, arbitrary URLs. The console renders attachments inline (clickable, Jira-style), and the MCP exposes them in `bus_read_thread()`.

**Why this priority**: Right now a thread can refer to "see PR #675" in prose. With typed attachments, the console auto-renders the PR status badge, the spec content, the ticket status, etc. — the conversation becomes navigable like Jira.

**Independent Test**: Attach a spec path, a PR number, and a kanban ticket id to one thread; verify all three render correctly in the console + appear in `bus_read_thread().attachments`.

**Acceptance Scenarios**:

1. **Given** a thread, **When** an agent calls `bus_attach(thread_id, kind='spec', ref='.specify/specs/001-agent-bus/spec.md')`, **Then** the attachment persists and `bus_read_thread()` includes it.
2. **Given** a thread with a `kind='pr', ref='675'` attachment, **When** the console renders the thread, **Then** the PR appears as a clickable badge with state (merged/open/closed) pulled from GitHub.

### Edge Cases

- **Concurrent posts**: two agents reply within milliseconds — both replies persist with stable, monotonic `created_at` (sub-second ties broken by autoincrement id).
- **Cycles in parent_id**: `bus_reply(parent=X)` where X is a descendant of the in-progress message — guard at write time (parent must already exist + belong to same thread).
- **Long bodies**: Discord caps at 2000 chars, the bus has no cap — when mirroring out, split or truncate with a "(continued)" marker.
- **Unknown audience**: posting to `audience='unknown-agent'` succeeds (don't gate creation on agent registry); the audience just sees zero readers.
- **Discord rate limits**: outbound mirror queues + retries on 429; never lose a bus message because Discord is rate-limited.
- **Spec link to nonexistent path**: attachment persists (history value); console renders a "(missing)" badge.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST persist threads and messages in a chitin-owned datastore (sqlite at `~/.chitin/agent-bus/bus.db`) — independent of `~/.hermes/kanban/`.
- **FR-002**: System MUST expose an MCP stdio server with at minimum these tools: `bus_post_thread`, `bus_reply`, `bus_list_threads`, `bus_read_thread`, `bus_inbox`, `bus_mark_read`, `bus_attach`.
- **FR-003**: Every message MUST carry author (agent id, string), body, created_at (epoch seconds, monotonic), kind (`message`/`directive`/`ack`/`system`), and optional audience (comma-separated agent ids or NULL=public).
- **FR-004**: Threads MUST support an optional kanban `task_id` reference without coupling to the kanban schema (string foreign-id only).
- **FR-005**: System MUST support read receipts per (message_id, agent_id) so an inbox query can filter unread messages.
- **FR-006**: System MUST support typed attachments on threads with kinds `spec | pr | task | discord | url | file` and a string ref.
- **FR-007**: System MUST support a Discord-mirror link on a thread (`discord_thread_id`) — populated/checked by the mirror service, never modified by other agents.
- **FR-008**: System MUST be additive-only on schema — new columns OK, repurposing existing columns NOT OK (consumers in multiple repos depend on stable semantics).
- **FR-009**: Session-start integration MUST be implementable as a Claude Code SessionStart hook that calls `bus_inbox(red, unread_only=true)` and surfaces the result in the session context.
- **FR-010**: Console UI MUST render thread lists + thread details + attachments + a compose form, reading/writing the same DB the MCP server uses (via console-api endpoints).

### Key Entities

- **Thread**: id, board, optional task_id, title, author, audience, status (open/resolved/archived), discord_thread_id, created_at, updated_at.
- **Message**: id, thread_id, parent_id (optional), author, audience, body, kind, discord_message_id, ack_required, created_at.
- **Read receipt**: (message_id, agent_id, read_at) — PK is (message_id, agent_id).
- **Attachment**: id, thread_id, kind, ref, optional display label, created_at.
- **Agent**: id, display_name, last_seen_at — self-registered on first call.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Two agents can complete one round-trip message exchange (post → reply → read) via MCP in under 100ms locally.
- **SC-002**: Operator's Claude Code session-start inbox surfaces unread `audience='red'` messages within the session's first system message — zero manual polling.
- **SC-003**: A thread with 100 messages and 5 attachments renders in the console in under 500ms.
- **SC-004**: Discord mirror lag for inbound messages ≤ 30s p95.
- **SC-005**: All MCP tools tolerate a power loss / process crash mid-call without DB corruption (sqlite WAL + bounded txn scope).

## Assumptions

- Chitin agents have local filesystem access to `~/.chitin/agent-bus/bus.db`. Remote-agent support (HTTP+token) is a followup.
- The Anthropic MCP protocol is the agent-facing contract. (Roll our own stdio JSON-RPC handler in v1 to avoid an external dep; swap to the official MCP SDK later if needed.)
- The chitin-console-api can be extended with new write endpoints — write-mode is already roadmapped per its current scope.
- Discord webhook URLs and a bot token are operator-provided via env when the mirror service is enabled; the mirror is opt-in per thread.
- This system supersedes ad-hoc cron-relays to Discord (the mechanism used to message hermes today) for agent-initiated comms. Operator-initiated Discord posts remain unchanged.

## Phased Delivery

- **Phase 1 (this PR)**: schema + MCP stdio server (US1 only). Tests: post, reply, list, read, inbox, mark-read, attach. No UI. No Discord. No agent integration. Ships as the bus's storage + interface contract.
- **Phase 2**: Claude Code session-start hook (US2).
- **Phase 3**: chitin-console UI for threads (US3).
- **Phase 4**: Discord bidirectional mirror service (US4).
- **Phase 5**: Console UI for attachments (US5 render path).

Each phase ships as its own tracking-epic kanban ticket linked back to this spec.

## Out of scope

- Replacing hermes kanban. The bus is a comms layer; tasks/lifecycle stay where they are. (Migration is its own future spec.)
- Replacing Discord. The bus complements Discord; the mirror is opt-in.
- Remote-agent comms over HTTP. Local stdio MCP only for v1.
- Permissions / ACLs. v1 assumes a trusted local environment; ACLs are a v2 concern once a remote surface exists.
- Encryption / e2ee. Same reason as ACLs.
