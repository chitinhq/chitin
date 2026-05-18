# 021 — agent-bus Discord mirror: re-read .env on every push

> Spec-kit entry for the bug discovered 2026-05-17 while drafting the
> spec 020 RFC: operator missed the RFC in #hermes Discord because
> the MCP bus_reply call's Discord-mirror leg silently no-op'd. Root
> cause: `discord_push._load_env_once()` caches the webhook map at
> module import; if the MCP server starts before
> `DISCORD_WEBHOOK_URL_*` lines are added to `~/.hermes/.env`, the
> map stays empty for the daemon's lifetime.

## Ticket refs

- Workspace chitin task #69 — operator log.

## File-system scope

- `services/agent-bus/discord_push.py`
- `services/agent-bus/tests/test_discord_mirror.py` (extend)
- `services/agent-bus/tests/test_discord_push_env_refresh.py` (new)
- `.specify/specs/021-bus-discord-mirror-stale-env/**`

Worker MUST NOT touch `server.py`, `inbox.py`, `db.py`,
`schema.sql`, or any other file under `services/agent-bus/` — the
bug surface is confined to `discord_push.py` and its tests.

## Goal

The bus→Discord mirror works without operator intervention even
when `~/.hermes/.env` changes mid-session.

## Problem (current code, ~line 35-65 of `discord_push.py`)

```python
def _load_env_once() -> None:
    global _ENV_LOADED, _BOT_TOKEN, _WEBHOOKS_BY_ID, _WEBHOOKS_BY_NAME
    if _ENV_LOADED:
        return
    _ENV_LOADED = True
    # … reads ~/.hermes/.env, populates the module globals …
```

Single-shot load means:
- MCP server started at 18:39 today (per `ps`)
- Webhook URL added to `.env` at some later point in the session
- Every `bus_reply` thereafter sees `_WEBHOOKS_BY_ID = {}` → falls
  back to bot-token POST (which appears as bot, not as the agent —
  exactly the UX the operator earlier rejected)
- Operator manually restarts MCP server, or uses direct webhook
  workaround. Both are unnecessary.

## Fix shape

Replace `_load_env_once` with `_load_env_if_changed` keyed by the
`.env` file's mtime. On every `try_push`:

1. `mtime = os.stat(env_path).st_mtime` (best-effort; if file
   missing, keep existing in-memory state)
2. If `mtime != _ENV_MTIME`: re-parse + reassign the module
   globals; update `_ENV_MTIME`
3. Proceed with push as today

Cost: one `stat` syscall per push. Bus push rate is well under
1/sec — negligible.

Bot-token path is unaffected (it doesn't read from env on every
call; it reads it via the same cached globals, so it benefits from
the same refresh).

## Acceptance Criteria

- **AC1**: With MCP server already running and `~/.hermes/.env`
  empty of `DISCORD_WEBHOOK_URL_<channel>`, adding the line and
  immediately calling `bus_reply` to the matching thread results in
  the Discord channel receiving the message (via webhook path,
  appearing as the calling agent username).
- **AC2**: Removing the line from `.env` mid-session makes the next
  `bus_reply` to that thread fall back to bot-token push (or
  silently no-op if bot token also absent) — the cached webhook URL
  must NOT continue to be used after removal.
- **AC3**: `try_push` performance: the added `stat` adds <1ms in
  the steady state (file unchanged). Measured in the test, not
  asserted as a hard threshold (env-load is rare enough that it's
  irrelevant — but the test documents the expectation).
- **AC4**: If `~/.hermes/.env` doesn't exist at all, `try_push`
  must NOT raise — same as today's behavior.

## Test coverage

### Why integration not e2e for this spec

The "end-to-end" surface here is **bus_reply call → Discord webhook
POST**. There is no human-observable boundary beyond the HTTP POST
the webhook receives (Discord's rendering is Discord's, not ours).
The integration test posts via the real `bus_reply`, intercepts the
HTTP call via a local mock-server fixture (already used by
`test_discord_mirror.py`), and asserts the POST happened with the
right body + username. That IS the end-to-end for this surface.

A "real Discord" e2e would post to a throwaway Discord channel and
poll the message API — heavier, flakier, and doesn't catch anything
the local-mock test doesn't.

| Spec AC | Test case (in `services/agent-bus/tests/test_discord_push_env_refresh.py`) | What breaks if removed |
|---------|----------------------------------------------------------------------------|------------------------|
| AC1 | `test_env_addition_picked_up_without_restart` | The originating bug recurs |
| AC2 | `test_env_removal_invalidates_webhook` | Stale webhooks linger after operator rotates them |
| AC3 | `test_stat_cost_is_negligible_when_unchanged` | Performance regression sneaks in |
| AC4 | `test_missing_env_file_does_not_raise` | Today's "fail open" behavior breaks |

Existing `test_discord_mirror.py` is extended with a one-line
assertion that `try_push` reads `.env` mtime before each call —
catches the spec's invariant from the inside (caller perspective).

## Invariants

- **inv-1: discord push never breaks bus write.** The mtime check
  must be wrapped in the same exception-swallowing as the rest of
  `discord_push` — a permission-denied `stat` shouldn't kill a
  push attempt; it should fall through to whatever auth path the
  cached env still supports.
- **inv-2: cheap before clever.** No watcher thread, no inotify,
  no `pyinotify` dependency. A `stat` per push is the simplest
  correct thing.

## Out of scope

- Re-loading the bot token from a different source (Vault, etc).
- Watching for new webhook URLs added to channels we don't know
  about yet (the threads table already owns that mapping; this
  spec is only about the env-side refresh).
- A general "MCP servers should re-read their config" framework —
  if other servers have the same bug, file their own specs.
