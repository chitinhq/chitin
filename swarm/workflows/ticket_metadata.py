"""Ticket metadata helpers for the swarm dispatch workflow."""
from __future__ import annotations

import json
import re
import sys
from typing import Any


ROLE_RE = re.compile(r"(?im)^\s*role\s*:\s*([a-z0-9][a-z0-9_-]*)\s*$")
KNOWN_ROLES = {
    "programmer",
    "researcher",
    "reviewer",
    "sentinel",
}


def _ticket_body(ticket: dict[str, Any]) -> str:
    task = ticket.get("task")
    if isinstance(task, dict):
        body = task.get("body")
        if isinstance(body, str):
            return body
    body = ticket.get("body")
    return body if isinstance(body, str) else ""


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
    return parse_role(_ticket_body(ticket), default=default)


def main(argv: list[str] | None = None) -> int:
    argv = argv or sys.argv[1:]
    default = argv[0] if argv else "programmer"
    data = json.load(sys.stdin)
    print(resolve_role(data, default=default))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
