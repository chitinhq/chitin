#!/usr/bin/env python3
"""mini-mcp — MCP stdio server wrapping the Mini CLI.

Why this exists: spec 039 wired Mini to Discord (#mini → bus → listener →
nudge). That path is fragile (bridge config, regex routing, kitty socket
discovery, agent shadowing). For operator-and-agent direct invocation,
this MCP server is a 10× simpler control surface: any Claude Code session
(or any MCP-aware client) calls `mini_open`/`mini_nudge`/etc. natively,
no Discord round-trip.

Implements the same plain JSON-RPC 2.0 over stdio pattern as
services/agent-bus/server.py — no external dependencies.

Tools:
  mini_open      — spawn a new Mini session (kitty window + Claude Code)
  mini_nudge     — send a message to an existing session's prompt
  mini_status    — read status.json for a goal_id
  mini_stop      — terminate a session (closes kitty window, status=failed)
  mini_list      — list all session state dirs with current status

Each tool shells out to `swarm/bin/mini`. Standard CLI args, standard
JSON stdout, errors propagated as JSON-RPC -32603.

Register with Claude Code:
  claude mcp add mini python3 services/mini-mcp/server.py
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from pathlib import Path
from typing import Any, Callable


PROTOCOL_VERSION = "2025-06-18"
SERVER_NAME = "mini"
SERVER_VERSION = "0.2.0"

# Locate the mini CLI relative to this file: services/mini-mcp/ -> repo root -> swarm/bin/mini
REPO_ROOT = Path(__file__).resolve().parents[2]
MINI_CLI = REPO_ROOT / "swarm" / "bin" / "mini"
SPECS_DIR = REPO_ROOT / ".specify" / "specs"
STATE_ROOT = Path(os.environ.get("MINI_STATE_ROOT", str(Path.home() / ".swarm" / "octi")))


# ---------------------------------------------------------------------------
# CLI shell-out helpers
# ---------------------------------------------------------------------------


def _run_mini(*args: str, timeout: int = 30) -> dict:
    """Invoke `mini <args>` and return parsed JSON stdout.

    Raises ValueError on non-zero exit, with stderr in the message.
    Raises ValueError on non-JSON stdout. The Mini CLI is supposed to
    emit one JSON object on stdout for every subcommand; if it doesn't,
    treat that as a contract violation we surface to the caller.
    """
    if not MINI_CLI.is_file():
        raise ValueError(f"mini CLI not found at {MINI_CLI}")
    proc = subprocess.run(
        [str(MINI_CLI), *args],
        capture_output=True, text=True, timeout=timeout, check=False,
    )
    if proc.returncode != 0:
        raise ValueError(
            f"mini {' '.join(args)} failed (rc={proc.returncode}): "
            f"{proc.stderr.strip() or proc.stdout.strip()}"
        )
    stdout = proc.stdout.strip()
    if not stdout:
        return {}
    try:
        return json.loads(stdout)
    except json.JSONDecodeError as e:
        # Some mini subcommands (stop) emit plain text. Wrap it.
        return {"output": stdout, "_note": f"non-json output: {e.msg}"}


# ---------------------------------------------------------------------------
# Tool implementations
# ---------------------------------------------------------------------------


# --- spec-reference resolution (spec 050 R1/R3) -----------------------------


def _spec_title(spec_md: Path) -> str:
    """First `# ` heading of a spec.md, or the dir name as a fallback."""
    for line in spec_md.read_text().splitlines():
        if line.startswith("# "):
            return line[2:].strip()
    return spec_md.parent.name


def _resolve_one(ref: str) -> list[Path]:
    """Resolve a single non-range spec reference to spec dir(s).

    Accepts an exact directory name (full slug) or a bare 3-digit number.
    No fuzzy/substring matching (spec 050 Q1 — exact only). Raises
    ValueError on a missing or ambiguous reference.
    """
    exact = SPECS_DIR / ref
    if exact.is_dir():
        return [exact]
    if re.fullmatch(r"\d{3}", ref):
        matches = sorted(d for d in SPECS_DIR.glob(f"{ref}-*") if d.is_dir())
        if len(matches) == 1:
            return matches
        if not matches:
            raise ValueError(f"no spec directory matches number {ref!r}")
        raise ValueError(
            f"spec number {ref!r} is ambiguous: {[m.name for m in matches]}"
        )
    raise ValueError(
        f"spec reference {ref!r} not found — expected a 3-digit number, "
        f"an ascending NNN-NNN range, or an exact spec directory name"
    )


def _resolve_spec_ref(ref: str) -> list[Path]:
    """Resolve one spec reference, expanding an ascending NNN-NNN range."""
    ref = ref.strip()
    if SPECS_DIR.joinpath(ref).is_dir():
        return [SPECS_DIR / ref]  # exact dir wins even if it looks range-y
    m = re.fullmatch(r"(\d{3})-(\d{3})", ref)
    if m:
        lo, hi = int(m.group(1)), int(m.group(2))
        if hi < lo:
            raise ValueError(
                f"range {ref!r} is descending; ranges must be ascending"
            )
        out: list[Path] = []
        for n in range(lo, hi + 1):
            out.extend(_resolve_one(f"{n:03d}"))
        return out
    return _resolve_one(ref)


def mini_open(*, specs: list[str], invoked_by: str | None = None,
              ticket: str | None = None) -> dict:
    """Spawn a new Mini session against one or more ratified specs.

    `specs` is a non-empty list of references — 3-digit numbers, exact
    spec directory names, or ascending NNN-NNN ranges. Every reference
    is resolved BEFORE any session is created; a missing or ambiguous
    reference is a hard error and spawns nothing (spec 050 R3).

    The session's /goal is composed from the resolved specs (R2): Mini
    is told to implement them in order, honoring each spec's acceptance
    criteria.
    """
    if not specs:
        raise ValueError("specs list cannot be empty — pass at least one spec reference")

    resolved: list[Path] = []
    for ref in specs:
        resolved.extend(_resolve_spec_ref(ref))

    # Dedupe, preserving first-seen order.
    seen: set[Path] = set()
    unique: list[Path] = []
    for d in resolved:
        if d not in seen:
            seen.add(d)
            unique.append(d)

    # Every resolved dir must carry a spec.md (boundary case 4).
    for d in unique:
        if not (d / "spec.md").is_file():
            raise ValueError(f"spec directory {d.name!r} has no spec.md")

    lines = []
    for i, d in enumerate(unique, 1):
        rel = d.relative_to(REPO_ROOT)
        lines.append(f"  {i}. {rel}/spec.md — {_spec_title(d / 'spec.md')}")
    goal = (
        "Implement the following ratified specs in one shot, in order:\n\n"
        + "\n".join(lines)
        + "\n\nRead each spec.md fully before starting. Honor every "
        "acceptance criterion. Write status.json transitions per the "
        "standard Mini contract. Do not start spec N+1 until spec N's "
        "`verify` passes."
    )

    args = ["open", "--goal", goal]
    if ticket:
        args += ["--ticket", ticket]
    result = _run_mini(*args, timeout=90)
    result["specs"] = [d.name for d in unique]
    result["invoked_by"] = invoked_by or os.environ.get("OCTI_OPERATOR") or "mcp"
    return result


def mini_nudge(*, goal_id: str, message: str,
               holder: str | None = None,
               lease_seconds: int | None = None) -> dict:
    """Send a message into an existing Mini session's prompt.
    Lease-locked: only one nudge holder at a time."""
    args = ["nudge", "--goal-id", goal_id, "--message", message]
    if holder:
        args += ["--holder", holder]
    if lease_seconds is not None:
        args += ["--lease-seconds", str(lease_seconds)]
    return _run_mini(*args)


def mini_status(*, goal_id: str) -> dict:
    """Read status.json for a session. Returns {goal_id, state, summary, ...}."""
    return _run_mini("status", "--goal-id", goal_id)


def mini_stop(*, goal_id: str, reason: str | None = None) -> dict:
    """Terminate a session (closes kitty window, sets status=failed).
    Idempotent — calling on an already-stopped session is fine."""
    args = ["stop", "--goal-id", goal_id]
    if reason:
        args += ["--reason", reason]
    return _run_mini(*args)


def mini_list() -> dict:
    """List all Mini sessions on this box with their goal_id + status snapshot.

    Walks $MINI_STATE_ROOT (default ~/.swarm/octi) looking for goal dirs.
    For each, reads goal.txt + status.json + thread_id (if bound).
    Returns {sessions: [{goal_id, state, summary, thread_id, ...}, ...]}.
    """
    if not STATE_ROOT.is_dir():
        return {"sessions": [], "state_root": str(STATE_ROOT), "note": "state_root does not exist"}
    sessions: list[dict] = []
    for entry in sorted(STATE_ROOT.iterdir()):
        if not entry.is_dir() or entry.name.startswith("."):
            continue
        info: dict[str, Any] = {"goal_id": entry.name}
        goal_file = entry / "goal.txt"
        if goal_file.is_file():
            info["goal"] = goal_file.read_text().strip()
        thread_file = entry / "thread_id"
        if thread_file.is_file():
            info["thread_id"] = thread_file.read_text().strip()
        status_file = entry / "status.json"
        if status_file.is_file():
            try:
                status = json.loads(status_file.read_text())
                info["state"] = status.get("state", "unknown")
                info["summary"] = status.get("summary", "")
                info["updated_at"] = status.get("updated_at")
            except json.JSONDecodeError:
                info["state"] = "corrupt-status"
        else:
            info["state"] = "unknown"
            info["summary"] = "no status.json yet"
        worktree_file = entry / "worktree"
        if worktree_file.is_file():
            info["worktree"] = worktree_file.read_text().strip()
        sessions.append(info)
    return {"sessions": sessions, "state_root": str(STATE_ROOT)}


# ---------------------------------------------------------------------------
# Tool catalog (returned by tools/list)
# ---------------------------------------------------------------------------


TOOLS: list[dict] = [
    {
        "name": "mini_open",
        "description": (
            "Spawn a new Mini session against one or more ratified specs. "
            "Creates a worktree under ~/workspace/chitin-octi-<slug>/, opens "
            "a kitty window with Claude Code, and composes the session goal "
            "from the named specs. Mini only runs specced work — there is no "
            "free-form goal (constitution §1: spec before dispatch)."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "specs": {
                    "type": "array",
                    "items": {"type": "string"},
                    "minItems": 1,
                    "description": (
                        "Spec references. Each is a 3-digit number (\"050\"), "
                        "an exact spec directory name (\"050-mini-mcp-spec-dispatch\"), "
                        "or an ascending range (\"039-042\"). Multiple entries are "
                        "implemented in one session, in order. Missing or ambiguous "
                        "references are a hard error — nothing is spawned."
                    ),
                },
                "invoked_by": {
                    "type": ["string", "null"],
                    "description": "Identity of the invoking agent/operator. Falls back to $OCTI_OPERATOR, then 'mcp'.",
                },
                "ticket": {"type": ["string", "null"], "description": "Optional kanban ticket id; uses agent/octi-<ticket> branch."},
            },
            "required": ["specs"],
        },
    },
    {
        "name": "mini_nudge",
        "description": (
            "Send a message into an existing Mini session's prompt. "
            "Lease-locked — only one nudge holder at a time so messages "
            "don't interleave. Use to redirect a stuck session, ask a "
            "question, or hand off context."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "goal_id": {"type": "string", "description": "Target session's goal_id."},
                "message": {"type": "string", "description": "Nudge body (becomes a new prompt to Claude)."},
                "holder": {"type": ["string", "null"], "description": "Operator identity (defaults to $OCTI_OPERATOR or current user)."},
                "lease_seconds": {"type": ["integer", "null"], "description": "Override default lease length."},
            },
            "required": ["goal_id", "message"],
        },
    },
    {
        "name": "mini_status",
        "description": (
            "Read status.json for a Mini session. Returns the current "
            "state (running/failed/paused/...), summary, and last-update "
            "timestamp. Use to check session health before nudging."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {"goal_id": {"type": "string"}},
            "required": ["goal_id"],
        },
    },
    {
        "name": "mini_stop",
        "description": (
            "Terminate a Mini session: closes the kitty window, marks "
            "status=failed. Idempotent — calling on an already-stopped "
            "session is fine. Use to clean up before respawning."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "goal_id": {"type": "string"},
                "reason": {"type": ["string", "null"], "description": "Free-text reason recorded in status.json."},
            },
            "required": ["goal_id"],
        },
    },
    {
        "name": "mini_list",
        "description": (
            "List all Mini sessions on this box with their goal_id, "
            "state, and (if bound) thread_id. Use to find a session "
            "without remembering the 40-char goal_id."
        ),
        "inputSchema": {"type": "object", "properties": {}},
    },
]


TOOL_DISPATCH: dict[str, Callable[..., dict]] = {
    "mini_open":   mini_open,
    "mini_nudge":  mini_nudge,
    "mini_status": mini_status,
    "mini_stop":   mini_stop,
    "mini_list":   mini_list,
}


# ---------------------------------------------------------------------------
# JSON-RPC dispatcher
# ---------------------------------------------------------------------------


def handle_request(req: dict) -> dict | None:
    rpc_id = req.get("id")
    method = req.get("method")
    params = req.get("params") or {}
    is_notification = "id" not in req

    def err(code: int, message: str) -> dict:
        return {"jsonrpc": "2.0", "id": rpc_id, "error": {"code": code, "message": message}}

    def ok(result: Any) -> dict:
        return {"jsonrpc": "2.0", "id": rpc_id, "result": result}

    if method == "initialize":
        return ok({
            "protocolVersion": PROTOCOL_VERSION,
            "capabilities": {"tools": {}},
            "serverInfo": {"name": SERVER_NAME, "version": SERVER_VERSION},
        })
    if method == "notifications/initialized":
        return None
    if method == "ping":
        return ok({})
    if method == "tools/list":
        return ok({"tools": TOOLS})
    if method == "tools/call":
        name = params.get("name")
        args = params.get("arguments") or {}
        fn = TOOL_DISPATCH.get(name)
        if not fn:
            return err(-32601, f"unknown tool: {name}")
        try:
            result = fn(**args)
        except TypeError as e:
            return err(-32602, f"invalid params for {name}: {e}")
        except ValueError as e:
            return err(-32602, str(e))
        except Exception as e:  # pragma: no cover
            return err(-32603, f"internal error: {e!r}")
        return ok({"content": [{"type": "text", "text": json.dumps(result)}]})

    if is_notification:
        return None
    return err(-32601, f"unknown method: {method}")


def serve_stdio() -> None:  # pragma: no cover
    for raw in sys.stdin:
        raw = raw.strip()
        if not raw:
            continue
        try:
            req = json.loads(raw)
        except json.JSONDecodeError:
            sys.stdout.write(json.dumps({
                "jsonrpc": "2.0", "id": None,
                "error": {"code": -32700, "message": "parse error"},
            }) + "\n")
            sys.stdout.flush()
            continue
        resp = handle_request(req)
        if resp is not None:
            sys.stdout.write(json.dumps(resp) + "\n")
            sys.stdout.flush()


if __name__ == "__main__":
    serve_stdio()
