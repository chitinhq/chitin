"""chitin-escalate-replies — hermes plugin that intercepts incoming messaging-
platform replies matching approve/deny patterns and resolves the matching
chitin pending_approvals row.

Closes the inbound-side gap of chitin's operator-approval escalate flow:
hermes-gateway drains the whatsapp bridge's /messages queue exclusively to
the LLM, so chitin-pending-watch can't see operator replies. This plugin
runs INSIDE the gateway's dispatch pipeline (pre_gateway_dispatch hook),
sees every incoming message before the LLM, and intercepts the ones that
look like chitin escalate replies.

Wire shape per ~/.hermes/hermes-agent/hermes_cli/plugins.py:78 :
  - Hook: pre_gateway_dispatch (fired BEFORE LLM dispatch)
  - Kwargs: event: MessageEvent (.text, .source.chat_id, .source.platform)
  - Return: {"action": "skip", "reason": "..."} to suppress LLM dispatch
            None to allow normal dispatch

Sender verification: only acts when event.source.chat_id matches the
operator's notify_chat_id from ~/.chitin/operator.yaml. Anyone else's
"approve" message in some other chat falls through to normal dispatch.

Row matching: newest-unresolved (one operator + one active escalation
at a time is the common case). On no-match, falls through silently so
a stray "approve" word in conversation doesn't suppress the LLM.

Test plan:
  1. Trigger a chitin gov-file edit -> escalate fires -> whatsapp ping arrives
  2. Reply "approve 30m" in the same chat
  3. Plugin parses, looks up newest-unresolved row, calls
     `chitin-kernel pending approve <id> --window 30m`
  4. Returns {"action":"skip"} so the LLM doesn't also reply
  5. Bug L (PR #395) auto-inserts the grant
  6. Operator-side log shows row resolved + grant inserted
"""
from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
from typing import Any, Dict, Optional

import yaml  # hermes ships pyyaml

# ---------------------------------------------------------------------------
# Configuration discovery
# ---------------------------------------------------------------------------

_CHITIN_KERNEL = (
    os.environ.get("CHITIN_KERNEL_PATH")
    or shutil.which("chitin-kernel")
    or os.path.expanduser("~/.local/bin/chitin-kernel")
)
_OPERATOR_YAML = os.path.expanduser("~/.chitin/operator.yaml")
_CHITIN_TIMEOUT_SEC = 5

# Match shapes the chitin notify template advertises as the reply grammar:
#
#   approve              -> single-call grant (no remember-window)
#   approve 30m          -> approve + grant 30 min for this rule
#   deny                 -> deny (no reason)
#   deny <reason>        -> deny with operator's reason
#
# Anchored on start-of-string so a paragraph that happens to contain the
# word "approve" doesn't trigger. Trailing whitespace tolerated. Case-
# insensitive because phone keyboards capitalize unpredictably.
_APPROVE_RE = re.compile(r"^\s*approve(?:\s+([0-9]+[smhd]?))?\s*$", re.IGNORECASE)
_DENY_RE = re.compile(r"^\s*deny(?:\s+(.+?))?\s*$", re.IGNORECASE)


def _load_operator_chat_id() -> Optional[str]:
    """Read ~/.chitin/operator.yaml and return notify_chat_id, or None
    when the file is missing/malformed. Re-read on every message so an
    operator config rewrite takes effect without restarting the gateway.
    """
    try:
        with open(_OPERATOR_YAML, "r", encoding="utf-8") as f:
            cfg = yaml.safe_load(f) or {}
        return cfg.get("notify_chat_id") or None
    except (FileNotFoundError, yaml.YAMLError, OSError):
        return None


def _newest_unresolved_pending_id() -> Optional[str]:
    """Return the id of the newest unresolved pending_approvals row, or
    None if the queue is empty / chitin-kernel is unavailable.

    Uses `chitin-kernel pending list --json` to avoid coupling this
    plugin to the sqlite schema.
    """
    try:
        result = subprocess.run(
            [_CHITIN_KERNEL, "pending", "list", "--json"],
            capture_output=True,
            text=True,
            timeout=_CHITIN_TIMEOUT_SEC,
        )
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        return None
    if result.returncode != 0:
        return None
    try:
        rows = json.loads(result.stdout or "[]")
    except json.JSONDecodeError:
        return None
    if not rows:
        return None
    # `pending list` orders oldest-first per pendingList in pending.go.
    # For "most recent unresolved" the operator most likely meant, take
    # the LAST row (newest by created_ts).
    return rows[-1].get("id")


def _resolve_approve(pending_id: str, window: str) -> bool:
    """Call `chitin-kernel pending approve <id> [--window <duration>]`.
    Returns True on success.
    """
    cmd = [_CHITIN_KERNEL, "pending", "approve", pending_id]
    if window:
        cmd += ["--window", window]
    return _run_chitin(cmd)


def _resolve_deny(pending_id: str, reason: str) -> bool:
    """Call `chitin-kernel pending deny <id> --reason <text>`. Returns
    True on success. Empty reason falls back to a default string so the
    audit row is never blank.
    """
    cmd = [
        _CHITIN_KERNEL,
        "pending",
        "deny",
        pending_id,
        "--reason",
        reason or "operator denied via messaging-platform reply",
    ]
    return _run_chitin(cmd)


def _run_chitin(cmd: list) -> bool:
    try:
        result = subprocess.run(
            cmd, capture_output=True, text=True, timeout=_CHITIN_TIMEOUT_SEC
        )
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        return False
    return result.returncode == 0


# ---------------------------------------------------------------------------
# Hook
# ---------------------------------------------------------------------------


def _pre_gateway_dispatch(event, gateway=None, session_store=None, **_kwargs):
    """Intercept approve/deny replies before the LLM sees them.

    Order of checks (cheapest first):
      1. Text matches one of the patterns. Otherwise None (allow).
      2. Operator config has a notify_chat_id. Otherwise None.
      3. Sender's chat_id matches the operator's notify_chat_id.
         Otherwise None — defense against a different chat-mate
         "approving" things they shouldn't.
      4. There's at least one unresolved pending row. Otherwise None
         (no-match: silent fall-through so a stray "approve" doesn't
         suppress the LLM).
      5. Resolve the row. On success, return skip. On failure, return
         None so the operator at least gets the LLM's normal response
         and can debug from there.
    """
    text = getattr(event, "text", "") or ""
    if not text.strip():
        return None

    approve_match = _APPROVE_RE.match(text)
    deny_match = _DENY_RE.match(text)
    if not (approve_match or deny_match):
        return None

    operator_chat_id = _load_operator_chat_id()
    if not operator_chat_id:
        return None  # operator hasn't wired hermes channel yet

    source = getattr(event, "source", None)
    sender_chat_id = getattr(source, "chat_id", None) if source else None
    if not sender_chat_id or sender_chat_id != operator_chat_id:
        return None  # someone other than the configured operator

    pending_id = _newest_unresolved_pending_id()
    if not pending_id:
        return None  # nothing queued; let LLM respond normally

    if approve_match:
        window = approve_match.group(1) or ""
        ok = _resolve_approve(pending_id, window)
        if not ok:
            return None
        return {
            "action": "skip",
            "reason": (
                f"chitin escalate approved {pending_id}"
                + (f" with window={window}" if window else "")
            ),
        }

    # deny path
    reason = (deny_match.group(1) or "").strip() if deny_match else ""
    ok = _resolve_deny(pending_id, reason)
    if not ok:
        return None
    return {
        "action": "skip",
        "reason": f"chitin escalate denied {pending_id}",
    }


def register(ctx) -> None:
    """Hermes plugin entry point."""
    ctx.register_hook("pre_gateway_dispatch", _pre_gateway_dispatch)
