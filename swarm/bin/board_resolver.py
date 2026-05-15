"""Shared board resolution for multi-board kanban.

Every script in swarm/bin/ should use resolve_db() instead of
hardcoding ~/.hermes/kanban/boards/chitin/kanban.db.

Resolution order:
  1. KANBAN_DB env var (explicit path, highest priority)
  2. KANBAN_BOARD env var → ~/.hermes/kanban/boards/<board>/kanban.db
  3. Default: KANBAN_BOARD=chitin
"""

import os
from pathlib import Path

BOARDS_DIR = Path(os.environ.get(
    "KANBAN_BOARDS_DIR",
    str(Path.home() / ".hermes" / "kanban" / "boards"),
))


def resolve_db(board: str | None = None) -> Path:
    """Return the kanban DB path for the given board.

    If board is None, uses KANBAN_DB (if set) or derives from KANBAN_BOARD.
    """
    explicit = os.environ.get("KANBAN_DB")
    if explicit:
        return Path(explicit)
    slug = board or os.environ.get("KANBAN_BOARD", "chitin")
    return BOARDS_DIR / slug / "kanban.db"


def resolve_board() -> str:
    """Return the board slug (e.g. 'chitin', 'readybench')."""
    return os.environ.get("KANBAN_BOARD", "chitin")


def board_config(board: str | None = None) -> dict:
    """Load config.json for the given board."""
    import json
    slug = board or resolve_board()
    config_path = BOARDS_DIR / slug / "config.json"
    if config_path.exists():
        return json.loads(config_path.read_text())
    return {}


def board_workspace(board: str | None = None) -> Path:
    """Return the workspace root for the given board."""
    cfg = board_config(board)
    ws = cfg.get("workspace_root")
    if ws:
        return Path(ws)
    slug = board or resolve_board()
    return Path.home() / "workspace" / slug