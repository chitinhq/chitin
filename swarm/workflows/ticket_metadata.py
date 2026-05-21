"""Ticket metadata helpers for the swarm dispatch workflow."""
from __future__ import annotations

import json
import re
import sys
from typing import Any


ROLE_RE = re.compile(r"(?im)^\s*role\s*:\s*([a-z0-9][a-z0-9_-]*)\s*$")
SENTINEL_ROUTE_RE = re.compile(r"(?i)\b(telemetry|invariant(?:s)?|chain-min(?:e|ing)|policy mining)\b")
KNOWN_ROLES = {
    "programmer",
    "researcher",
    "reviewer",
    "telemetry",
}


def _ticket_body(ticket: dict[str, Any]) -> str:
    task = ticket.get("task")
    if isinstance(task, dict):
        body = task.get("body")
        if isinstance(body, str):
            return body
    body = ticket.get("body")
    return body if isinstance(body, str) else ""


def _ticket_title(ticket: dict[str, Any]) -> str:
    task = ticket.get("task")
    if isinstance(task, dict):
        title = task.get("title")
        if isinstance(title, str):
            return title
    title = ticket.get("title")
    return title if isinstance(title, str) else ""


def parse_role(body: str | None, default: str = "programmer") -> str:
    """Parse `role: <name>` from a ticket body, with a safe fallback."""
    if not body:
        return default
    match = ROLE_RE.search(body)
    if not match:
        return default
    role = match.group(1).strip().lower()
    if role not in KNOWN_ROLES:
        return default
    return role


def resolve_role(ticket: dict[str, Any], default: str = "programmer") -> str:
    body = _ticket_body(ticket)
    explicit = parse_role(body, default="")
    if explicit:
        return explicit
    title = _ticket_title(ticket)
    if SENTINEL_ROUTE_RE.search(title) or SENTINEL_ROUTE_RE.search(body):
        return "telemetry"
    return default


def main(argv: list[str] | None = None) -> int:
    argv = argv or sys.argv[1:]
    default = argv[0] if argv else "programmer"
    data = json.load(sys.stdin)
    print(resolve_role(data, default=default))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
