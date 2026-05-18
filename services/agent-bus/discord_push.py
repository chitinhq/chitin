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
"""
from __future__ import annotations

import json
import os
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


def _post_via_webhook(webhook_url: str, *, author: str, body: str) -> bool:
    payload = json.dumps({
        "username": author[:32],  # Discord limit
        "content": _truncate(body),
    }).encode("utf-8")
    req = urllib.request.Request(
        webhook_url,
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
        return 200 <= resp.status < 300


def _post_via_bot(channel_id: str, *, author: str, body: str) -> bool:
    content = _truncate(f"**{author}**: {body}", limit=1980)
    payload = json.dumps({"content": content}).encode("utf-8")
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
        return 200 <= resp.status < 300


def try_push(*, channel_id: str | None, author: str, body: str) -> None:
    """Best-effort push of a bus message to its mirrored Discord channel.

    Never raises. Logs to stderr AND to the spec-023 failure JSONL on
    failure. The bus write has already happened by the time this runs;
    Discord is a downstream projection.
    """
    if not channel_id:
        return
    _load_env_if_changed()
    body_len = len(body)

    webhook = _WEBHOOKS_BY_ID.get(channel_id)
    if webhook:
        try:
            _post_via_webhook(webhook, author=author, body=body)
            return
        except Exception as exc:
            err = f"{type(exc).__name__}: {exc}"
            print(f"[discord_push] webhook POST failed for channel {channel_id}: {err}",
                  file=sys.stderr)
            _log_failure(channel_id=channel_id, path="webhook", error=err, body_len=body_len)

    if _BOT_TOKEN:
        try:
            _post_via_bot(channel_id, author=author, body=body)
            return
        except Exception as exc:
            err = f"{type(exc).__name__}: {exc}"
            print(f"[discord_push] bot POST failed for channel {channel_id}: {err}",
                  file=sys.stderr)
            _log_failure(channel_id=channel_id, path="bot", error=err, body_len=body_len)
    else:
        msg = "no DISCORD_BOT_TOKEN; no webhook for channel"
        print(f"[discord_push] {msg} {channel_id}", file=sys.stderr)
        _log_failure(channel_id=channel_id, path="no-auth", error=msg, body_len=body_len)
