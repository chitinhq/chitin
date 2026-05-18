# 023 — agent-bus ↔ Discord bidirectional liveness contract

> Operator directive 2026-05-17 21:50 EDT (after the bus failed to
> reach Ares + Clawta for an overnight-goal RFC):
>
> > *"get the discord chat working as phase 0. im sick of it not
> > working as expected. spec it out. get it working. write e2e tests."*

This spec is **Phase 0** — every other workstream tonight is gated
on it. Without bidirectional bus↔Discord liveness, the swarm
cannot coordinate; operator can't reach agents asynchronously;
agents can't reach operator outside their cron windows.

## Ticket refs

- Workspace chitin task #69 (closed — caught only the outbound half).
- This spec subsumes + extends chitin spec 021 (which addressed
  outbound stale-env only). Spec 021 stays in the record for
  the diagnosis history; this spec is the implementation contract.
- Operator pain log this session:
  - 21:08 EDT: bus_reply to thread #5 silently no-op'd; operator
    missed the spec-020 RFC; manual webhook workaround used.
  - 21:50 EDT: bus_reply to thread #9 reached Discord (via my
    direct webhook) but Ares + Clawta didn't read it because
    inbound poll cron doesn't exist.

## File-system scope

- `services/agent-bus/discord_push.py` (subsumes spec 021's edit)
- `services/agent-bus/discord_mirror.py` (extend with `poll-all`
  subcommand that iterates every linked thread)
- `services/agent-bus/server.py` (no functional change; this
  ensures bus_reply continues to call discord_push)
- `services/agent-bus/tests/test_discord_push_env_refresh.py` (new — spec 021's planned tests, brought forward)
- `services/agent-bus/tests/test_discord_inbound_poll_cron.py` (new — cron-wired inbound poll)
- `services/agent-bus/tests/test_bidirectional_liveness_e2e.py` (new — the operator's mandated e2e)
- `swarm/bin/install-agent-bus-cron.sh` (new — wires the inbound
  poll into `hermes cron` declaratively; idempotent re-install)
- `~/.hermes/cron/jobs.json` (the install script edits this; a
  fixture lives in `swarm/tests/fixtures/expected-cron-jobs.json`)
- `.specify/specs/023-agent-bus-bidirectional-liveness/**`

## Goal

Every operator-visible message in either direction reaches its
audience within a deterministic latency budget, with NO manual
intervention required after the install. When it fails, it fails
loud (named error in a known log) — not silently.

## The three failure modes this spec closes

### F1 — outbound stale env (was spec 021's surface)

`discord_push._load_env_once()` caches `~/.hermes/.env` at module
import. When the MCP server starts before webhook URLs exist in
`.env`, the daemon's in-memory map stays empty for its lifetime.
Every `bus_reply` thereafter either silently no-ops or falls back
to bot-token POST (which appears as the bot, not the calling
agent — already-rejected UX).

### F2 — inbound poll never runs

`services/agent-bus/discord_mirror.py poll` exists. It works when
invoked manually. It is NOT wired into `hermes cron`. So Discord
messages from operator, Ares, or Clawta never land in the bus DB
until someone manually polls. The agents' read paths see nothing
new even when messages exist in Discord.

### F3 — no cross-direction liveness verification

There is no test that asserts the round trip: post via `bus_reply`,
verify Discord received it AND inbound poll re-ingested it AND a
subsequent `bus_inbox` call surfaces it. Today, each direction has
narrow unit tests; neither has e2e.

## Requirements

- **R1 (subsumes spec 021)**: `discord_push.try_push` reads
  `~/.hermes/.env` mtime before every call. If mtime > cached:
  re-parse + update module globals. Stat-per-push cost is
  negligible (push rate is well under 1/sec).
- **R2**: A new `hermes cron` job `agent-bus-inbound-poll` runs
  every 60 seconds. It invokes `discord_mirror.py poll-all` which
  iterates every row in `threads` where `discord_thread_id IS NOT
  NULL` and polls each. Cron is idempotent (re-install overwrites).
- **R3**: `install-agent-bus-cron.sh` writes the cron job
  declaratively + signs the result. The install script is the
  source of truth; running it twice produces identical state.
- **R4**: A bidirectional e2e test (`test_bidirectional_liveness_e2e.py`)
  posts via `bus_reply` to a test thread, asserts (a) Discord
  webhook received the POST (via a local mock-server fixture in
  CI; live webhook in operator-local runs), (b) inbound poll
  re-ingests the same message, (c) `bus_inbox` for the audience
  agent surfaces it. Both halves of the round trip MUST be
  asserted in the same test; partial assertions are NOT acceptable
  per "either both work or neither does."
- **R5**: When discord_push fails (HTTP non-2xx, timeout,
  exception), it MUST log to a known file
  (`~/.hermes/logs/discord-push-failures.jsonl`) with timestamp,
  thread_id, channel_id, error class, body length. Today's silent-
  fail behavior is the bug being fixed; failures must surface.
- **R6**: The inbound poll MUST be safe to re-run while a previous
  invocation is still running (e.g. lock file or atomic cursor
  update); the 60s cadence can collide with a slow Discord API.

## Test coverage

### Why integration + e2e (per spec 020 §1.2 default)

The end-to-end surface here is the bus_reply call → real HTTP POST
to Discord webhook → real Discord channel → real cron poll →
real bus DB → real bus_inbox response. This is the e2e per spec
020's UI/HTTP definition (every layer crossed). A local mock-
server fixture stands in for Discord in CI; operator-local runs
can flip to the real webhook via env var.

| Spec AC | Test case | What breaks if removed |
|---------|-----------|------------------------|
| R1 outbound env refresh | `test_env_addition_picked_up_without_restart` (in test_discord_push_env_refresh.py) | F1 recurs |
| R1 outbound env removal | `test_env_removal_invalidates_webhook` | Stale webhooks linger after rotation |
| R1 outbound fail-open on missing .env | `test_missing_env_file_does_not_raise` | Existing behavior breaks |
| R2 inbound poll cron exists | `test_inbound_poll_cron_job_registered` (in test_discord_inbound_poll_cron.py) | F2 recurs |
| R2 poll-all iterates all linked threads | `test_poll_all_iterates_every_linked_thread` | Single-thread bug returns; new threads never poll |
| R3 install script idempotent | `test_install_script_idempotent` (running twice produces identical jobs.json) | Reinstall corrupts cron |
| R4 bidirectional e2e | `test_bus_reply_reaches_inbox_round_trip` (the operator-mandated e2e) | F3 returns; partial-direction silent-fail recurs |
| R5 failures logged | `test_push_failure_writes_to_jsonl` (force HTTP 500 from mock; assert log entry) | Silent-fail recurs |
| R6 poll concurrency safe | `test_concurrent_polls_do_not_double-ingest` | Duplicate ingestion under cron jitter |

All test cases carry `# spec: 023-agent-bus-bidirectional-liveness`
reference comment per the spec 020 contract.

## Latency budget

- Outbound (bus_reply → Discord visible): < 2 seconds (HTTP round trip)
- Inbound (Discord posted → bus_inbox returns it): < 90 seconds
  (60s cron cadence + 30s slack for Discord API)
- Bidirectional round trip (bus_reply by A → bus_inbox by B
  surfaces the same message via the Discord mirror): < 90 seconds

Anything outside these budgets in production is a regression and
must be filed as a follow-up spec.

## Acceptance Criteria

- **AC1**: After this PR merges + `install-agent-bus-cron.sh`
  runs, the operator can post via `bus_reply` to any
  Discord-mirrored thread and see it in Discord within 2s, without
  the manual-webhook workaround.
- **AC2**: Within 90s of a message posted to a mirrored Discord
  channel (by operator, Ares, Clawta, or any agent), the message
  is queryable via `bus_inbox`.
- **AC3**: When the operator deletes a webhook URL from
  `~/.hermes/.env` and the next bus_reply happens, discord_push
  does NOT continue using the cached URL — it either falls back
  to bot-token or fails-loud per R5.
- **AC4**: All 9 named test cases above pass against the local
  mock-server fixture (`make test-agent-bus`); the bidirectional
  e2e (`test_bus_reply_reaches_inbox_round_trip`) also passes
  against the live operator-box install (manual `make
  test-agent-bus-live`).
- **AC5**: Failure logging works: deliberately breaking the
  webhook URL produces an entry in
  `~/.hermes/logs/discord-push-failures.jsonl` within 2s.

## Invariants

- **inv-1: every visible message has a known terminal state.** Sent
  successfully OR logged as failed. No silent drops.
- **inv-2: the round trip is atomic from the operator's POV.**
  Within 90s any message they send via either path reaches the
  audience via both paths. They don't need to know which path
  fired.
- **inv-3: install is the source of truth.** Hand-edits to
  `~/.hermes/cron/jobs.json` get overwritten on next install.
  Configuration lives in the install script + spec — not in
  state.

## Out of scope

- A separate "operator-only" channel filter (operator messages get
  priority handling) — covered in a future spec if the message
  volume warrants.
- Migrating off Discord to a different transport — not on the
  table; Discord is the agreed surface.
- A general "MCP servers should re-read config on a file watcher"
  framework — if other servers have similar bugs, file separately.
- Cross-instance bus state (multiple operator boxes) — single-box
  scope.

## Why this spec exists

Tonight, twice, the bus failed to deliver in ways that cost
operator time + missed coordination:

1. Outbound: bus_reply was supposed to push to Discord. It
   silently no-op'd. Operator missed the spec-020 RFC. Spec 021
   documented the bug but the implementation hadn't shipped.
2. Inbound: webhook-posted RFC reached Discord. Ares + Clawta
   didn't read it because the inbound poll cron doesn't exist.
   3-way coordination broke; the overnight goal stalled.

The operator's response: *"get the discord chat working as phase
0. im sick of it not working as expected."* Both halves get fixed
in one PR, with an e2e test that asserts the round trip — so the
next "the bus is broken" surprise is caught in CI, not by the
operator at 9pm.
