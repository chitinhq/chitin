"""Shared board resolution for multi-board kanban.

Every script in swarm/bin/ should use resolve_db() instead of
hardcoding ~/.hermes/kanban/boards/chitin/kanban.db.

Resolution order:
  1. KANBAN_DB env var (explicit path, highest priority)
  2. KANBAN_BOARD env var → ~/.hermes/kanban/boards/<board>/kanban.db
  3. Default: KANBAN_BOARD=chitin
"""

import argparse
import os
import sys
from pathlib import Path
from typing import Iterable, Sequence

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
        return Path(os.path.expandvars(os.path.expanduser(ws)))
    slug = board or resolve_board()
    return Path.home() / "workspace" / slug


def board_repo(board: str | None = None) -> str:
    """Return the owner/name repository configured for the given board."""
    cfg = board_config(board)
    repo = cfg.get("repo")
    return str(repo or "")


def _split_csv(value: str | Iterable[str] | None) -> list[str]:
    if value is None:
        return []
    if isinstance(value, str):
        raw = value.split(",")
    else:
        raw = list(value)
    return [str(item).strip() for item in raw if str(item).strip()]


def owned_orgs(board: str | None = None) -> set[str]:
    """Return org/user names whose repos keep swarm specs repo-locally.

    Defaults are intentionally small and can be extended either in board
    config (`owned_orgs`) or via `CLAWTA_OWNED_ORGS=org,user`.
    """
    cfg = board_config(board)
    orgs = {"chitinhq", "red"}
    orgs.update(_split_csv(cfg.get("owned_orgs")))
    orgs.update(_split_csv(os.environ.get("CLAWTA_OWNED_ORGS")))
    return {org.lower() for org in orgs}


def is_owned_repo(board: str | None = None) -> bool:
    repo = board_repo(board)
    owner = repo.split("/", 1)[0].lower() if "/" in repo else ""
    return bool(owner and owner in owned_orgs(board))


def workspace_spec_root() -> Path:
    """Return the workspace-level spec-kit root for shared/team repos."""
    root = os.environ.get("CLAWTA_WORKSPACE_ROOT") or os.environ.get("WORKSPACE_ROOT")
    if root:
        return Path(os.path.expandvars(os.path.expanduser(root))) / ".specify" / "specs"
    return Path.home() / "workspace" / ".specify" / "specs"


def spec_dir_for_board(board: str | None = None) -> Path:
    """Return the directory containing spec-kit entries for this board.

    Resolution uses the explicit `spec_source` field from board config.
    The legacy `owned_orgs` default-set is no longer used as an implicit
    fallback — boards MUST declare `spec_source`.

    Valid spec_source values:
      "repo"              → <workspace_root>/.specify/specs
      "workspace_overlay"  → ~/workspace/.specify/specs
      "owned_orgs"         → legacy: same as repo but derived from owned_orgs
    """
    cfg = board_config(board)
    source = cfg.get("spec_source", "")
    if source == "repo":
        return board_workspace(board) / ".specify" / "specs"
    if source == "workspace_overlay":
        return workspace_spec_root()
    if source == "owned_orgs":
        # Legacy opt-in: derive from owned_orgs set.
        if is_owned_repo(board):
            return board_workspace(board) / ".specify" / "specs"
        return workspace_spec_root()
    # No spec_source declared — fall back to workspace overlay.
    # This is intentional: new boards without config default to the
    # shared overlay rather than silently deriving from owned_orgs.
    return workspace_spec_root()


def board_flag(argv: Sequence[str] | None = None) -> str | None:
    """Extract a --board override from argv without mutating parser state."""
    args = list(sys.argv[1:] if argv is None else argv)
    for i, arg in enumerate(args):
        if arg == "--board" and i + 1 < len(args):
            value = args[i + 1].strip()
            return value or None
        if arg.startswith("--board="):
            value = arg.split("=", 1)[1].strip()
            return value or None
    return None


def using_implicit_default_board(argv: Sequence[str] | None = None) -> bool:
    """True when the caller is falling back to the legacy implicit `chitin` board."""
    return (
        board_flag(argv) is None
        and not os.environ.get("KANBAN_BOARD")
        and not os.environ.get("KANBAN_DB")
    )


def apply_board_cli_override(argv: Sequence[str] | None = None) -> str:
    """Apply a --board override into KANBAN_BOARD and return the effective slug."""
    slug = board_flag(argv)
    if slug:
        os.environ["KANBAN_BOARD"] = slug
        return slug
    return resolve_board()


def warn_implicit_default_board(script_name: str, argv: Sequence[str] | None = None) -> None:
    """Emit the board-default deprecation warning once per invocation."""
    if using_implicit_default_board(argv):
        print(
            f"{script_name}: defaulting to board 'chitin' because neither --board nor "
            "KANBAN_BOARD was set. This legacy fallback is deprecated; set one explicitly.",
            file=sys.stderr,
        )


def spec_source_resolution(board: str | None = None) -> tuple[Path, str]:
    """Return (spec_dir, source_tag) for telemetry.

    source_tag is one of "repo", "workspace_overlay", "owned_orgs", or
    "default" (no spec_source declared).
    """
    cfg = board_config(board)
    source = cfg.get("spec_source", "")
    tag = source if source in ("repo", "workspace_overlay", "owned_orgs") else "default"
    return spec_dir_for_board(board), tag


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Resolve board-config fields for shell callers.")
    parser.add_argument("--board", help="board slug override (defaults to KANBAN_BOARD or chitin)")
    parser.add_argument(
        "field",
        choices=("board", "db", "repo", "workspace", "spec-dir", "spec-source", "config"),
        help="field to print",
    )
    args = parser.parse_args(list(argv) if argv is not None else None)
    if args.board:
        os.environ["KANBAN_BOARD"] = args.board

    field_map = {
        "board": lambda: resolve_board(),
        "db": lambda: str(resolve_db()),
        "repo": lambda: board_repo(),
        "workspace": lambda: str(board_workspace()),
        "spec-dir": lambda: str(spec_dir_for_board()),
        "spec-source": lambda: spec_source_resolution()[1],
        "config": lambda: str(BOARDS_DIR / resolve_board() / "config.json"),
    }
    print(field_map[args.field]())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
