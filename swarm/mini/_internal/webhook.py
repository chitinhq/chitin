"""Discord webhook posting for filtered event tailer.

Webhook URL resolution order:
  1. OCTI_DISCORD_WEBHOOK_URL env var (operator's explicit override)
  2. MINI_DISCORD_CHANNEL_ID -> DISCORD_WEBHOOK_URL_<id> (hermes convention)
  3. <state_dir>/webhook.url file (per-session override)
  4. None (silently no-op)

Discord webhooks REJECT bare Python-urllib User-Agent with 403. We must
send a Discord-compatible UA on every POST.

Never reads webhook URL from any tracked source.

Slice 2 additions:
  - post_to_thread(): post into an existing Discord thread via ?thread_id=
  - create_and_post_to_thread(): create a per-session thread and return
    the thread_id. Tries thread_name in the JSON body first; if that
    fails (non-forum channel), posts the opening message then creates a
    thread from it via the Discord API. Falls back gracefully (S2-R4).
"""

from __future__ import annotations

import json
import logging
import os
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

PRIMARY_ENV = "OCTI_DISCORD_WEBHOOK_URL"
SWARM_ENV = "OCTI_DISCORD_SWARM_WEBHOOK_URL"
CHANNEL_ID_ENV = "MINI_DISCORD_CHANNEL_ID"
USER_AGENT = "DiscordBot (https://github.com/chitinhq/chitin, 1.0) Mini"

_log = logging.getLogger("swarm.mini.webhook")

# Path to the hermes dotenv. Overridable via MINI_HERMES_ENV so tests can
# point at an isolated (or absent) file and not pick up the operator's
# real secrets.
HERMES_ENV_PATH_ENV = "MINI_HERMES_ENV"
_hermes_env_cache: tuple[str, float] | None = None


def _hermes_env_path() -> Path:
    return Path(os.environ.get(HERMES_ENV_PATH_ENV)
                or (Path.home() / ".hermes" / ".env"))


def _load_hermes_env_if_changed() -> None:
    """Merge OCTI_*/DISCORD_*/MINI_* keys from the hermes dotenv into os.environ.

    Re-reads when the file's (path, mtime) changes; cheap when unchanged.
    Mirrors the pattern in services/agent-bus/discord_push.py — kitty-
    spawned sessions and cron-driven invocations don't inherit the
    operator's interactive env, so the secret has to come from disk.
    """
    global _hermes_env_cache
    path = _hermes_env_path()
    try:
        mtime = path.stat().st_mtime
    except FileNotFoundError:
        return
    key = (str(path), mtime)
    if _hermes_env_cache == key:
        return
    _hermes_env_cache = key
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, _, v = line.partition("=")
        k = k.strip()
        if not (k.startswith("OCTI_") or k.startswith("DISCORD_") or k.startswith("MINI_")):
            continue
        v = v.strip().strip('"').strip("'")
        # Don't clobber a value the operator explicitly set in their shell.
        os.environ.setdefault(k, v)


def _channel_webhook_from_env() -> str | None:
    cid = os.environ.get(CHANNEL_ID_ENV, "").strip()
    if not cid:
        return None
    return os.environ.get(f"DISCORD_WEBHOOK_URL_{cid}", "").strip() or None


def resolve_webhook_url(state_dir: Path | None = None) -> str | None:
    _load_hermes_env_if_changed()
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


# ---------------------------------------------------------------------------
# Slice 2 — per-session Discord thread support (S2-R2, S2-R3, S2-R4)
# ---------------------------------------------------------------------------


def _discord_post_raw(url: str, body: dict, *, timeout: int = 5,
                      opener=None) -> dict | None:
    """Low-level Discord POST that returns the parsed JSON response or None.

    Returns None on any network/HTTP error (graceful degradation, S2-R4).
    """
    payload = json.dumps(body).encode("utf-8")
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
            raw = resp.read()
            return json.loads(raw)
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as exc:
        _log.debug("Discord POST to %s failed: %s", url, exc)
        return None


def post_to_thread(url: str | None, thread_id: str | None, content: str,
                   *, timeout: int = 5, opener=None) -> bool:
    """Post a message into an existing Discord thread.

    Uses the webhook URL with ``?thread_id=<id>`` query parameter.
    If thread_id is None/empty, falls back to a channel-level ``post()``.
    Returns True on success. Swallows errors per S2-R4.
    """
    if not url:
        return False
    if not thread_id:
        return post(url, content, timeout=timeout, opener=opener)
    # Append thread_id as a query parameter. The webhook URL may already
    # have query params (e.g. ?wait=true), so use urllib.parse.
    parsed = urllib.parse.urlparse(url)
    qs = urllib.parse.parse_qs(parsed.query)
    qs["thread_id"] = [thread_id]
    new_query = urllib.parse.urlencode(qs, doseq=True)
    thread_url = urllib.parse.urlunparse(parsed._replace(query=new_query))
    return post(thread_url, content, timeout=timeout, opener=opener)


def create_and_post_to_thread(url: str | None, goal_id: str, content: str,
                               *, timeout: int = 5,
                               opener=None) -> str | None:
    """Create a Discord thread for a session and post the opening message.

    Returns the thread_id on success, or None if thread creation fails
    (S2-R4: graceful degradation — the message is still posted to the
    channel as a fallback).

    Strategy (per S2-Q1 resolution):
      1. Try ``thread_name`` in the JSON body — this works on forum
         channels.
      2. If that fails (typical for regular text channels), post the
         opening message normally, then use the returned message_id
         to start a thread via the Discord API
         (POST /channels/{channel_id}/messages/{message_id}/threads).
         Channel ID is derived from the webhook ID via the Discord API
         (GET /webhooks/{webhook_id}).
      3. If anything fails, just post the message to the channel and
         return None — events will fall back to channel-level posts.
    """
    if not url:
        return None

    thread_name = f"session-{goal_id[:24]}"

    # --- Strategy 1: thread_name in body (forum channels) ---
    body = {"content": content[:1900], "thread_name": thread_name}
    result = _discord_post_raw(url, body, timeout=timeout, opener=opener)
    if result and isinstance(result, dict):
        # Discord returns the channel object for the created thread,
        # which includes id (the thread channel id).
        tid = result.get("id") or result.get("channel_id")
        if tid:
            _log.debug("Thread created via thread_name: %s", tid)
            return str(tid)

    # --- Strategy 2: post message, then start thread from it ---
    # Post the opening message without thread_name
    body = {"content": content[:1900]}
    msg_result = _discord_post_raw(url, body, timeout=timeout, opener=opener)
    if not msg_result or not isinstance(msg_result, dict):
        # Even the basic post failed; return None (the message is lost,
        # but we don't block the session per S2-R4).
        return None

    message_id = msg_result.get("id")
    channel_id = msg_result.get("channel_id")
    if not message_id:
        return None

    # If channel_id wasn't in the response, try to derive it from the
    # webhook URL. The webhook URL format is:
    #   https://discord.com/api/webhooks/{webhook_id}/{token}
    if not channel_id:
        channel_id = _channel_id_from_webhook_url(url)

    if not channel_id:
        _log.debug("Cannot resolve channel_id to create thread; falling back")
        return None

    # Create a thread on the posted message via the Discord API.
    # POST /channels/{channel.id}/messages/{message.id}/threads
    thread_url = (
        f"https://discord.com/api/v10/channels/{channel_id}"
        f"/messages/{message_id}/threads"
    )
    bot_token = os.environ.get("DISCORD_BOT_TOKEN", "").strip()
    thread_body = {"name": thread_name, "type": 11}  # 11 = public thread
    headers = {
        "Content-Type": "application/json",
        "User-Agent": USER_AGENT,
    }
    if bot_token:
        headers["Authorization"] = f"Bot {bot_token}"

    payload = json.dumps(thread_body).encode("utf-8")
    req = urllib.request.Request(thread_url, data=payload, headers=headers,
                                 method="POST")
    open_fn = opener or urllib.request.urlopen
    try:
        with open_fn(req, timeout=timeout) as resp:
            thread_data = json.loads(resp.read())
            tid = thread_data.get("id")
            if tid:
                _log.debug("Thread created via message endpoint: %s", tid)
                return str(tid)
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as exc:
        _log.debug("Thread creation from message failed: %s", exc)

    # S2-R4: thread creation failed, message already posted to channel
    return None


def _channel_id_from_webhook_url(webhook_url: str) -> str | None:
    """Derive channel_id from a webhook URL by querying the Discord API.

    GET /webhooks/{webhook_id} returns the webhook object including
    channel_id. Requires DISCORD_BOT_TOKEN.
    """
    parsed = urllib.parse.urlparse(webhook_url)
    # Expect path like /api/webhooks/{id}/{token}
    parts = parsed.path.strip("/").split("/")
    webhook_id = None
    for i, seg in enumerate(parts):
        if seg == "webhooks" and i + 1 < len(parts):
            webhook_id = parts[i + 1]
            break
    if not webhook_id:
        return None

    bot_token = os.environ.get("DISCORD_BOT_TOKEN", "").strip()
    if not bot_token:
        return None

    info_url = f"https://discord.com/api/v10/webhooks/{webhook_id}"
    req = urllib.request.Request(
        info_url,
        headers={
            "User-Agent": USER_AGENT,
            "Authorization": f"Bot {bot_token}",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            data = json.loads(resp.read())
            return data.get("channel_id")
    except (urllib.error.URLError, OSError, json.JSONDecodeError):
        return None