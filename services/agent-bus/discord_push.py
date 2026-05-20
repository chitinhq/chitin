"""Best-effort Discord push for the agent-bus MCP server.

When a bus thread is mirrored to a Discord channel (the threads row has
`discord_thread_id` set to the channel id), this module POSTs the new
message to that channel so operator-visible surfaces stay in sync.

Configuration is read from `~/.hermes/.env` ON EVERY PUSH (re-read when
the file's mtime changes; cached otherwise):
  DISCORD_BOT_TOKEN              # required for bot-token POSTs
  DISCORD_WEBHOOK_URLS           # optional, name-keyed map preferred
                                   format: "name1=https://...,name2=https://..."
  DISCORD_WEBHOOK_URL_<channel>  # optional, per-channel-id override

Path priority per call:
  1. Per-channel-id webhook (cleanest, appears as author username)
  2. Bot-token POST (always works if bot is in the guild; appears as bot)

Failures are logged to ~/.hermes/logs/discord-push-failures.jsonl
(spec 023 R5) so operators can see them — but never raised, because
Discord is a downstream projection of the bus write that already
happened.

History:
  * spec 021 first noticed the bug: _load_env_once() cached the env
    map at module import, which trapped daemons that started before
    operator added webhook URLs.
  * spec 023 ships the fix (mtime-based refresh) + inbound poll cron
    + bidirectional e2e tests.
  * ticket t_a6df2cdc: resolve @AgentName text mentions to Discord
    <@user_id> mention entities and set allowed_mentions so Discord
    creates structural mention objects that gateway listeners detect.
"""
from __future__ import annotations

import json
import os
import re
import sys
import time
import urllib.parse
import urllib.request
from pathlib import Path

# spec: 023-agent-bus-bidirectional-liveness
ENV_PATH = Path.home() / ".hermes" / ".env"
FAILURE_LOG_PATH = Path.home() / ".hermes" / "logs" / "discord-push-failures.jsonl"

_ENV_MTIME: float = 0.0
_BOT_TOKEN = ""
_WEBHOOKS_BY_ID: dict[str, str] = {}
_WEBHOOKS_BY_NAME: dict[str, str] = {}

# ---------------------------------------------------------------------------
# Mention resolution: @AgentName → Discord <@user_id> mention entities
# ticket t_a6df2cdc: causes 2-3 of parent ticket t_241ddc44
#
# Discord only fires user-mention notifications when the message content
# contains a structural <@user_id> mention. Plain-text @AgentName does
# NOT create a mention entity, so gateway listeners that filter on
# message.mentions never see it. We resolve @AgentName tokens to the
# native <@id> form and set allowed_mentions so Discord parses them.
#
# To add a new agent: append an entry below with the agent's Discord
# user ID (obtain via Discord dev tools / right-click → Copy User ID).
# The _MENTION_RESOLVE_RE regex is built from this dict at module load.
# ---------------------------------------------------------------------------
_AGENT_DISCORD_IDS: dict[str, str] = {
    "clawta": "1503438472801685565",
    # Ares/hermes share the same bot identity (runtime id = hermes).
    # The bot token prefix decodes to this user ID.
    "ares": "150343848646685258",
    "hermes": "150343848646685258",
    # Agents without a Discord user ID yet — text @mentions resolve to
    # nothing (left as plain text). Add the ID when the operator wires
    # up a Discord identity for these agents.
    # "icarus": "<pending>",
    # "red": "<pending>",
    # "copilot": "<pending>",
}

# Pre-compile regex matching @<agentname> tokens (case-insensitive).
# Word-boundary lookarounds avoid matching email@clawta.com or
# @clawta-poller.
_MENTION_RESOLVE_NAMES = "|".join(_AGENT_DISCORD_IDS)
_MENTION_RESOLVE_RE = re.compile(
    r"(?<![A-Za-z0-9_])@(" + _MENTION_RESOLVE_NAMES + r")(?![A-Za-z0-9_-])",
    re.IGNORECASE,
)


def resolve_mentions(body: str) -> tuple[str, list[str]]:
    """Replace @AgentName tokens with Discord <@user_id> mention syntax.

    Returns (resolved_body, user_ids) where resolved_body has every
    known @AgentName replaced with ``<@discord_user_id>``, and user_ids
    is the list of distinct Discord user IDs that were resolved (for
    use in the ``allowed_mentions`` payload).

    Unknown @mentions (not in _AGENT_DISCORD_IDS) pass through unchanged.
    """
    user_ids: list[str] = []
    seen: set[str] = set()

    def _replace(m: re.Match) -> str:
        name = m.group(1).lower()
        uid = _AGENT_DISCORD_IDS.get(name)
        if not uid:
            return m.group(0)  # leave unknown @mentions as plain text
        if uid not in seen:
            user_ids.append(uid)
            seen.add(uid)
        return f"<@{uid}>"

    resolved = _MENTION_RESOLVE_RE.sub(_replace, body)
    return resolved, user_ids


def _load_env_if_changed() -> None:
    """Parse ~/.hermes/.env when its mtime changes. Cheap (one stat per call).

    Spec 023 R1: replaces the previous _load_env_once() which trapped
    daemons started before the env file had its full webhook map.

    Fail-open: if the file is missing or unreadable, keep existing
    in-memory state (covered by AC4 / test_missing_env_file_does_not_raise).
    """
    global _ENV_MTIME, _BOT_TOKEN, _WEBHOOKS_BY_ID, _WEBHOOKS_BY_NAME
    try:
        mtime = ENV_PATH.stat().st_mtime
    except OSError:
        return  # file missing → keep cached state (fail open per spec 023)

    if mtime == _ENV_MTIME:
        return  # unchanged since last read

    _ENV_MTIME = mtime
    file_env: dict[str, str] = {}
    try:
        for line in ENV_PATH.read_text().splitlines():
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            k, _, v = line.partition("=")
            file_env[k.strip()] = v.strip().strip('"').strip("'")
    except OSError:
        return  # can't read after stat → keep cached state

    _BOT_TOKEN = os.environ.get("DISCORD_BOT_TOKEN") or file_env.get("DISCORD_BOT_TOKEN", "")

    # Webhook maps are fully rebuilt on each load — removals matter
    # (spec 023 R1 / AC3: env removal must invalidate cached webhook).
    new_webhooks_by_id: dict[str, str] = {}
    new_webhooks_by_name: dict[str, str] = {}

    name_map = os.environ.get("DISCORD_WEBHOOK_URLS") or file_env.get("DISCORD_WEBHOOK_URLS", "")
    for entry in name_map.split(","):
        if "=" in entry:
            name, _, url = entry.partition("=")
            name = name.strip().lower()
            url = url.strip()
            if name and url:
                new_webhooks_by_name[name] = url

    for key in {**os.environ, **file_env}:
        if key.startswith("DISCORD_WEBHOOK_URL_"):
            channel_id = key[len("DISCORD_WEBHOOK_URL_"):]
            url = os.environ.get(key) or file_env.get(key, "")
            if channel_id and url:
                new_webhooks_by_id[channel_id] = url

    _WEBHOOKS_BY_ID = new_webhooks_by_id
    _WEBHOOKS_BY_NAME = new_webhooks_by_name


def _truncate(text: str, limit: int = 1900) -> str:
    """Discord max content is 2000; leave room for author prefix + ellipsis."""
    if len(text) <= limit:
        return text
    return text[: limit - 14] + "\n…(truncated)"


def _log_failure(*, channel_id: str | None, path: str, error: str, body_len: int) -> None:
    """Spec 023 R5: write failures to a known JSONL so operators see them."""
    try:
        FAILURE_LOG_PATH.parent.mkdir(parents=True, exist_ok=True)
        entry = {
            "ts": int(time.time()),
            "channel_id": channel_id,
            "path": path,        # "webhook" | "bot" | "no-auth"
            "error": error,
            "body_len": body_len,
        }
        with FAILURE_LOG_PATH.open("a") as f:
            f.write(json.dumps(entry) + "\n")
    except Exception:
        # Failure-logging failure must not raise; we already swallow push errors.
        pass


def _post_via_webhook(webhook_url: str, *, author: str, body: str) -> str | None:
    """POST to a Discord webhook. Returns the created message snowflake on
    success, or None on failure / non-2xx. Per pos-002 AC5: returning
    the id lets `try_push` stamp it back onto the source bus row so the
    inbound mirror doesn't re-ingest the message as a duplicate.

    We use `?wait=true` so Discord returns the message object instead
    of 204 No Content (default).
    """
    resolved_body, user_ids = resolve_mentions(body)
    payload_dict: dict = {
        "username": author[:32],  # Discord limit
        "content": _truncate(resolved_body),
    }
    # ticket t_a6df2cdc: set allowed_mentions so Discord parses <@id>
    # mentions into structural mention entities that gateway listeners
    # detect via message.mentions. Without this, Discord silently
    # strips the mention entity even if <@id> appears in content.
    if user_ids:
        payload_dict["allowed_mentions"] = {
            "parse": ["users"],
            "users": user_ids,
        }
    payload = json.dumps(payload_dict).encode("utf-8")
    sep = "&" if "?" in webhook_url else "?"
    url = f"{webhook_url}{sep}wait=true"
    req = urllib.request.Request(
        url,
        data=payload,
        method="POST",
        headers={
            "Content-Type": "application/json",
            # Discord rejects Python's default urllib User-Agent with 403;
            # send a real UA so the webhook accepts the POST.
            "User-Agent": "agent-bus-mcp-push/0.1 (chitinhq)",
        },
    )
    with urllib.request.urlopen(req, timeout=5) as resp:
        if not (200 <= resp.status < 300):
            return None
        try:
            data = json.loads(resp.read().decode("utf-8"))
            return data.get("id")
        except (json.JSONDecodeError, UnicodeDecodeError):
            return None


def _post_via_bot(channel_id: str, *, author: str, body: str) -> str | None:
    """POST via bot token. Returns the message snowflake on success.

    Per pos-002 AC5: returning the id is required for self-echo dedupe.
    Bot endpoint returns the full message object by default.
    """
    resolved_body, user_ids = resolve_mentions(body)
    content = _truncate(f"**{author}**: {resolved_body}", limit=1980)
    payload_dict: dict = {"content": content}
    # ticket t_a6df2cdc: set allowed_mentions so Discord parses the
    # <@id> mentions we resolved above into structural entities.
    if user_ids:
        payload_dict["allowed_mentions"] = {
            "parse": ["users"],
            "users": user_ids,
        }
    payload = json.dumps(payload_dict).encode("utf-8")
    url = f"https://discord.com/api/v10/channels/{channel_id}/messages"
    req = urllib.request.Request(
        url,
        data=payload,
        method="POST",
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bot {_BOT_TOKEN}",
            "User-Agent": "agent-bus-mcp-push/0.1",
        },
    )
    with urllib.request.urlopen(req, timeout=5) as resp:
        if not (200 <= resp.status < 300):
            return None
        try:
            data = json.loads(resp.read().decode("utf-8"))
            return data.get("id")
        except (json.JSONDecodeError, UnicodeDecodeError):
            return None


def try_push(*, channel_id: str | None, author: str, body: str) -> str | None:
    """Best-effort push of a bus message to its mirrored Discord channel.

    Returns the created Discord message snowflake on success, or None
    on no-channel / failure. Per pos-002 AC5: callers (bus_reply,
    bus_post_thread) MUST stamp the returned id back onto the source
    `messages` row's `discord_message_id` BEFORE the next inbound poll
    can run — otherwise the mirror re-imports the message as a
    duplicate bus row (the canonical 3419→3420 echo bug).

    Never raises. Logs to stderr AND to the spec-023 failure JSONL on
    failure. The bus write has already happened by the time this runs;
    Discord is a downstream projection.
    """
    if not channel_id:
        return None
    _load_env_if_changed()
    body_len = len(body)

    webhook = _WEBHOOKS_BY_ID.get(channel_id)
    if webhook:
        try:
            return _post_via_webhook(webhook, author=author, body=body)
        except Exception as exc:
            err = f"{type(exc).__name__}: {exc}"
            print(f"[discord_push] webhook POST failed for channel {channel_id}: {err}",
                  file=sys.stderr)
            _log_failure(channel_id=channel_id, path="webhook", error=err, body_len=body_len)

    if _BOT_TOKEN:
        try:
            return _post_via_bot(channel_id, author=author, body=body)
        except Exception as exc:
            err = f"{type(exc).__name__}: {exc}"
            print(f"[discord_push] bot POST failed for channel {channel_id}: {err}",
                  file=sys.stderr)
            _log_failure(channel_id=channel_id, path="bot", error=err, body_len=body_len)
    else:
        msg = "no DISCORD_BOT_TOKEN; no webhook for channel"
        print(f"[discord_push] {msg} {channel_id}", file=sys.stderr)
        _log_failure(channel_id=channel_id, path="no-auth", error=msg, body_len=body_len)

    return None
