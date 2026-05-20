"""Thread_id <-> goal_id routing for the Mini mention listener.

Spec 039 slice 1 binds:
- R1: per-session thread binding. Each Mini state dir owns at most one
  `thread_id` file, written atomically.
- B2: first inbound message on an unbound thread binds the mapping if
  the message body unambiguously names exactly one live goal_id.
- AC2: thread_id -> goal_id is 1:1, enforced at write time.

This module is pure routing — it never imports MiniSession, never calls
nudge, never posts to Discord. The mention listener composes these
primitives with the inbound side.
"""

from __future__ import annotations

import os
import re
from dataclasses import dataclass
from pathlib import Path

# Token boundary: any run of chars that aren't whitespace, quotes, or
# common punctuation. Lets `setup-octi-slice-2-7490cc18` survive when
# wrapped in backticks, parens, commas, etc. — but does NOT match the
# substring "abc" inside the token "abc-extended".
_TOKEN_RE = re.compile(r"[^\s`'\"<>,;:!?()\[\]{}]+")


class BoundThreadMismatch(RuntimeError):
    """state_dir is already bound to a different thread_id."""


class ThreadAlreadyClaimed(RuntimeError):
    """Another live goal already claims this thread_id (AC2 violation)."""


@dataclass(frozen=True)
class RouteResult:
    decision: str
    goal_id: str | None
    state_dir: Path | None
    candidates: tuple[str, ...] = ()


def _read_thread_id(state_dir: Path) -> str | None:
    f = state_dir / "thread_id"
    if not f.is_file():
        return None
    return f.read_text().strip() or None


def _live_goal_dirs(state_root: Path) -> list[Path]:
    if not state_root.is_dir():
        return []
    return sorted(p for p in state_root.iterdir() if p.is_dir())


def _body_tokens(body: str) -> set[str]:
    return set(_TOKEN_RE.findall(body))


def route_message(
    *, state_root: Path, bus_thread_id: str, body: str,
) -> RouteResult:
    """Resolve a bus message to a Mini goal_id.

    Resolution order (R1 + B2 from spec 039, B3 sole-session added 2026-05-19):
      1. If any state dir's `thread_id` matches bus_thread_id exactly:
         - one match  -> "bound"
         - 2+ matches -> "collision" (defensive; bind_thread normally
           prevents this)
      2. Otherwise scan live goal_ids for token-exact occurrence in body:
         - exactly one -> "first_inbound_bind"
         - 2+          -> "ambiguous"
      3. (B3) No goal_id named in body, but exactly one *unbound* live
         session exists -> "first_inbound_bind" against the sole session.
         The UX win: operator types `@mini ping` without remembering a
         40-char goal_id, and the listener routes to the only Mini that
         doesn't already own a thread.
      4. Otherwise -> "no_match" (no live session, or 2+ unbound and
         no disambiguation in body).
    """
    goal_dirs = _live_goal_dirs(state_root)

    bound_matches: list[Path] = []
    for d in goal_dirs:
        if _read_thread_id(d) == bus_thread_id:
            bound_matches.append(d)

    if len(bound_matches) == 1:
        d = bound_matches[0]
        return RouteResult(decision="bound", goal_id=d.name, state_dir=d)
    if len(bound_matches) > 1:
        names = tuple(sorted(d.name for d in bound_matches))
        return RouteResult(
            decision="collision", goal_id=None, state_dir=None, candidates=names,
        )

    tokens = _body_tokens(body)
    named = [d for d in goal_dirs if d.name in tokens]
    if len(named) == 1:
        d = named[0]
        return RouteResult(decision="first_inbound_bind", goal_id=d.name, state_dir=d)
    if len(named) > 1:
        names = tuple(sorted(d.name for d in named))
        return RouteResult(
            decision="ambiguous", goal_id=None, state_dir=None, candidates=names,
        )

    # B3: sole-session auto-bind. Only fires when there's exactly one
    # *unbound* live session — multiple unbound sessions stay ambiguous
    # (no_match), and an already-bound sole session won't poach another
    # thread.
    unbound = [d for d in goal_dirs if _read_thread_id(d) is None]
    if len(unbound) == 1:
        d = unbound[0]
        return RouteResult(decision="first_inbound_bind", goal_id=d.name, state_dir=d)

    return RouteResult(decision="no_match", goal_id=None, state_dir=None)


def bind_thread(
    *, state_root: Path, state_dir: Path, bus_thread_id: str,
) -> None:
    """Atomically write state_dir/thread_id = bus_thread_id.

    Enforces AC2 (1:1 mapping) before writing:
      - if this state_dir is already bound to bus_thread_id -> no-op
      - if this state_dir is already bound to a different id -> BoundThreadMismatch
      - if any *other* live state_dir already owns bus_thread_id -> ThreadAlreadyClaimed

    Write is tmp+rename with mode 600 on the final file.
    """
    if not bus_thread_id:
        raise ValueError("bus_thread_id cannot be empty")

    existing = _read_thread_id(state_dir)
    if existing == bus_thread_id:
        return  # idempotent
    if existing is not None:
        raise BoundThreadMismatch(
            f"{state_dir.name} already bound to {existing!r}, refused {bus_thread_id!r}"
        )

    # AC2: no other live goal may already own this thread_id.
    for d in _live_goal_dirs(state_root):
        if d == state_dir:
            continue
        if _read_thread_id(d) == bus_thread_id:
            raise ThreadAlreadyClaimed(
                f"thread {bus_thread_id!r} already claimed by goal {d.name!r}"
            )

    target = state_dir / "thread_id"
    tmp = state_dir / f"thread_id.tmp.{os.getpid()}"
    tmp.write_text(bus_thread_id + "\n")
    os.chmod(tmp, 0o600)
    os.replace(tmp, target)
