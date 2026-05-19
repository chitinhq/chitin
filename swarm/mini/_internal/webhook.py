"""Discord webhook posting for filtered event tailer.

Webhook URL resolution order:
  1. OCTI_DISCORD_WEBHOOK_URL env var (operator's explicit override)
  2. MINI_DISCORD_CHANNEL_ID -> DISCORD_WEBHOOK_URL_<id> (hermes convention)
  3. <state_dir>/webhook.url file (per-session override)
  4. None (silently no-op)

Discord webhooks REJECT bare Python-urllib User-Agent with 403. We must
send a Discord-compatible UA on every POST.

Never reads webhook URL from any tracked source.
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from pathlib import Path

PRIMARY_ENV = "OCTI_DISCORD_WEBHOOK_URL"
SWARM_ENV = "OCTI_DISCORD_SWARM_WEBHOOK_URL"
CHANNEL_ID_ENV = "MINI_DISCORD_CHANNEL_ID"
USER_AGENT = "DiscordBot (https://github.com/chitinhq/chitin, 1.0) Mini"


def _channel_webhook_from_env() -> str | None:
    cid = os.environ.get(CHANNEL_ID_ENV, "").strip()
    if not cid:
        return None
    return os.environ.get(f"DISCORD_WEBHOOK_URL_{cid}", "").strip() or None


def resolve_webhook_url(state_dir: Path | None = None) -> str | None:
    env_url = os.environ.get(PRIMARY_ENV, "").strip()
    if env_url:
        return env_url
    chan = _channel_webhook_from_env()
    if chan:
        return chan
    if state_dir is not None:
        wh_file = state_dir / "webhook.url"
        if wh_file.is_file():
            return wh_file.read_text().strip() or None
    return None


def resolve_swarm_webhook_url() -> str | None:
    return os.environ.get(SWARM_ENV, "").strip() or None


def post(url: str | None, content: str, *, timeout: int = 5, opener=None) -> bool:
    """POST {"content": <content>} to the Discord webhook. Returns True on success.

    No-op (returns False) if url is empty/None. Swallows network errors.
    Sets a Discord-compatible User-Agent — Discord rejects the default
    Python-urllib UA with HTTP 403.
    """
    if not url:
        return False
    payload = json.dumps({"content": content[:1900]}).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=payload,
        headers={
            "Content-Type": "application/json",
            "User-Agent": USER_AGENT,
        },
        method="POST",
    )
    open_fn = opener or urllib.request.urlopen
    try:
        with open_fn(req, timeout=timeout) as resp:
            return 200 <= resp.status < 300
    except (urllib.error.URLError, OSError):
        return False
