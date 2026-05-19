"""Deterministic (board, audience) → Discord channel resolver.

Background: the canonical bug (mem 3695) is that ``bus_post_thread``
historically inserted threads with ``discord_thread_id=NULL`` and called
``try_push(channel_id=None, ...)`` which silently no-ops. Every
bus-initiated thread was Discord-dark; only inbound-mirror-created
threads ever reached Discord. The resolver below replaces that silent
no-op with an explicit, table-driven routing decision.

**Determinism invariant (Knuth: state the invariant first)**: for a
given ``(board, audience)`` input, ``resolve_channel`` returns the same
result every time the routes table is unchanged. There is no implicit
fallback, no per-call randomness, no env-var precedence cascade — every
choice is a row in ``discord_routes``.

Resolution precedence:

  1. ``scope='audience'``, ``key=<agent_id>`` for each agent listed in
     the comma-separated ``audience`` argument. Within the audience,
     priority breaks ties: higher priority wins. (Operators can pin a
     specific agent's channel above the board default.)
  2. ``scope='board'``, ``key=<board>``.
  3. ``scope='global'``, ``key='*'``.

If a matching row has ``channel_id=NULL``, the thread is intentionally
muted — the resolver returns ``None`` without raising. If no row
matches AND no global default exists, ``UnroutableError`` is raised.

The resolver is called by ``bus_post_thread`` before INSERT so the
``discord_thread_id`` column carries the routed channel from the
moment the thread row exists.
"""

from __future__ import annotations

import sqlite3
import time
from dataclasses import dataclass


class UnroutableError(RuntimeError):
    """Raised when no route matches and there is no global default.

    The caller (typically ``bus_post_thread``) decides whether to:
      - propagate the error (strict mode — refuse to create a
        Discord-dark thread), or
      - catch it and insert with ``discord_thread_id=NULL`` (legacy
        compat mode — used during the transition before routes are
        seeded on every box).
    """


GLOBAL_KEY = "*"
_VALID_SCOPES = frozenset({"audience", "board", "global"})


@dataclass(frozen=True)
class Route:
    scope: str
    key: str
    channel_id: str | None
    priority: int
    updated_at: int

    def is_mute(self) -> bool:
        return self.channel_id is None


def _parse_audience(audience: str | None) -> list[str]:
    if not audience:
        return []
    return [a.strip() for a in audience.split(",") if a.strip()]


# ---------------------------- read API ----------------------------


def resolve_channel(
    conn: sqlite3.Connection,
    *,
    board: str | None,
    audience: str | None,
) -> str | None:
    """Return the Discord channel id for ``(board, audience)``, or None
    if a matching row exists with ``channel_id=NULL`` (explicit mute).

    Raises ``UnroutableError`` if no row matches and there is no
    ``scope='global', key='*'`` default.

    Determinism: if two audience-scope rows match (multi-agent
    audience), higher ``priority`` wins; ties broken by lower
    ``updated_at`` (older is more deterministic — operator-pinned
    routes survive new additions).
    """
    audience_agents = _parse_audience(audience)

    if audience_agents:
        placeholders = ",".join("?" for _ in audience_agents)
        rows = conn.execute(
            f"SELECT scope, key, channel_id, priority, updated_at "
            f"FROM discord_routes "
            f"WHERE scope='audience' AND key IN ({placeholders}) "
            f"ORDER BY priority DESC, updated_at ASC",
            audience_agents,
        ).fetchall()
        if rows:
            r = rows[0]
            return _to_route(r).channel_id  # may be None (explicit mute)

    if board is not None:
        row = conn.execute(
            "SELECT scope, key, channel_id, priority, updated_at "
            "FROM discord_routes WHERE scope='board' AND key=?",
            (board,),
        ).fetchone()
        if row:
            return _to_route(row).channel_id

    row = conn.execute(
        "SELECT scope, key, channel_id, priority, updated_at "
        "FROM discord_routes WHERE scope='global' AND key=?",
        (GLOBAL_KEY,),
    ).fetchone()
    if row:
        return _to_route(row).channel_id

    raise UnroutableError(
        f"no Discord route resolves (board={board!r}, audience={audience!r}); "
        f"add a row to discord_routes (audience/board/global) or set a "
        f"global default with scope='global' key='*' channel_id=<id>"
    )


def list_routes(conn: sqlite3.Connection) -> list[Route]:
    rows = conn.execute(
        "SELECT scope, key, channel_id, priority, updated_at "
        "FROM discord_routes ORDER BY scope, priority DESC, key"
    ).fetchall()
    return [_to_route(r) for r in rows]


# ---------------------------- write API ----------------------------


def set_route(
    conn: sqlite3.Connection,
    *,
    scope: str,
    key: str,
    channel_id: str | None,
    priority: int = 100,
) -> Route:
    """UPSERT a route. ``channel_id=None`` is a deliberate mute marker."""
    if scope not in _VALID_SCOPES:
        raise ValueError(f"scope must be one of {sorted(_VALID_SCOPES)}, got {scope!r}")
    if not key:
        raise ValueError("key cannot be empty (use '*' for global default)")
    if scope == "global" and key != GLOBAL_KEY:
        raise ValueError(f"global scope requires key={GLOBAL_KEY!r}, got {key!r}")
    now = int(time.time())
    conn.execute(
        "INSERT INTO discord_routes(scope, key, channel_id, priority, updated_at) "
        "VALUES(?,?,?,?,?) "
        "ON CONFLICT(scope, key) DO UPDATE SET "
        "  channel_id=excluded.channel_id, "
        "  priority=excluded.priority, "
        "  updated_at=excluded.updated_at",
        (scope, key, channel_id, priority, now),
    )
    conn.commit()
    return Route(scope=scope, key=key, channel_id=channel_id,
                 priority=priority, updated_at=now)


def unset_route(conn: sqlite3.Connection, *, scope: str, key: str) -> bool:
    """Remove a route. Returns True if a row was removed."""
    if scope not in _VALID_SCOPES:
        raise ValueError(f"scope must be one of {sorted(_VALID_SCOPES)}, got {scope!r}")
    cur = conn.execute(
        "DELETE FROM discord_routes WHERE scope=? AND key=?", (scope, key)
    )
    conn.commit()
    return cur.rowcount > 0


# ---------------------------- internals ----------------------------


def _to_route(row) -> Route:
    return Route(
        scope=row["scope"],
        key=row["key"],
        channel_id=row["channel_id"],
        priority=row["priority"],
        updated_at=row["updated_at"],
    )
