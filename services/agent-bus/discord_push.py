"""Best-effort Discord push for the agent-bus MCP server.

When a bus thread is mirrored to a Discord channel (the threads row has
`discord_thread_id` set to the channel id), this module POSTs the new
message to that channel so operator-visible surfaces stay in sync.

Configuration is read from `~/.hermes/.env` at module load:
  DISCORD_BOT_TOKEN              # required for POSTs
  DISCORD_WEBHOOK_URLS           # optional, name-keyed map preferred
                                   format: "name1=https://...,name2=https://..."
  DISCORD_WEBHOOK_URL_<channel>  # optional, per-channel-id override

Path priority per call:
  1. Per-channel-id webhook (cleanest, appears as author username)
  2. Bot-token POST (always works if bot is in the guild; appears as bot)

All failures are swallowed — Discord push must never break a bus write.
"""
from __future__ import annotations

import json
import os
import sys
import urllib.parse
import urllib.request
from pathlib import Path

_ENV_LOADED = False
_BOT_TOKEN = ""
_WEBHOOKS_BY_ID: dict[str, str] = {}
_WEBHOOKS_BY_NAME: dict[str, str] = {}


def _load_env_once() -> None:
    """Parse ~/.hermes/.env once at first use. Idempotent."""
    global _ENV_LOADED, _BOT_TOKEN, _WEBHOOKS_BY_ID, _WEBHOOKS_BY_NAME
    if _ENV_LOADED:
        return
    _ENV_LOADED = True

    env_path = Path.home() / ".hermes" / ".env"
    file_env: dict[str, str] = {}
    if env_path.is_file():
        for line in env_path.read_text().splitlines():
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            k, _, v = line.partition("=")
            file_env[k.strip()] = v.strip().strip('"').strip("'")

    _BOT_TOKEN = os.environ.get("DISCORD_BOT_TOKEN") or file_env.get("DISCORD_BOT_TOKEN", "")

    name_map = os.environ.get("DISCORD_WEBHOOK_URLS") or file_env.get("DISCORD_WEBHOOK_URLS", "")
    for entry in name_map.split(","):
        if "=" in entry:
            name, _, url = entry.partition("=")
            name = name.strip().lower()
            url = url.strip()
            if name and url:
                _WEBHOOKS_BY_NAME[name] = url

    for key in {**os.environ, **file_env}:
        if key.startswith("DISCORD_WEBHOOK_URL_"):
            channel_id = key[len("DISCORD_WEBHOOK_URL_"):]
            url = os.environ.get(key) or file_env.get(key, "")
            if channel_id and url:
                _WEBHOOKS_BY_ID[channel_id] = url


def _truncate(text: str, limit: int = 1900) -> str:
    """Discord max content is 2000; leave room for author prefix + ellipsis."""
    if len(text) <= limit:
        return text
    return text[: limit - 14] + "\n…(truncated)"


def _post_via_webhook(webhook_url: str, *, author: str, body: str) -> bool:
    payload = json.dumps({
        "username": author[:32],  # Discord limit
        "content": _truncate(body),
    }).encode("utf-8")
    req = urllib.request.Request(
        webhook_url,
        data=payload,
        method="POST",
        headers={"Content-Type": "application/json"},
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

    Never raises. Logs to stderr on failure for diagnostics. The bus write
    has already happened by the time this runs; Discord is a downstream
    projection.
    """
    if not channel_id:
        return
    _load_env_once()

    webhook = _WEBHOOKS_BY_ID.get(channel_id)
    if webhook:
        try:
            _post_via_webhook(webhook, author=author, body=body)
            return
        except Exception as exc:
            print(f"[discord_push] webhook POST failed for channel {channel_id}: {exc}",
                  file=sys.stderr)

    if _BOT_TOKEN:
        try:
            _post_via_bot(channel_id, author=author, body=body)
            return
        except Exception as exc:
            print(f"[discord_push] bot POST failed for channel {channel_id}: {exc}",
                  file=sys.stderr)
    else:
        print(f"[discord_push] no DISCORD_BOT_TOKEN; skipping push for channel {channel_id}",
              file=sys.stderr)
