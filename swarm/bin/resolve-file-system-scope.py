#!/usr/bin/env python3
"""Resolve a ticket's file-system scope from its bound spec-kit entry.

Per Constitution §1.1 (added 2026-05-17 after portal MVP day-0 retro),
every spec MUST contain a "## File-system scope" section declaring
which path globs the worker MAY write to, which it MUST NOT.

This helper parses that section out of the bound spec.md and emits
JSON for the lobster dispatcher to embed in SPAWN_CONFIG as
`file_system_scope: {may: [...], must_not: [...]}`. The post-spawn
validator in spawn_worker_subprocess.py reads it and enforces.

If no spec is found, or the spec has no scope section, emits empty
arrays. This is back-compat: legacy specs without scope sections
continue to dispatch + finalize unchanged (no enforcement applied).

Usage:
  resolve-file-system-scope.py --board <board> --ticket-id <t_xxx>
  resolve-file-system-scope.py --spec-file <path>

Output (always JSON):
  {"may": ["apps/portal/**", ...], "must_not": ["frontend/**", ...], "source": "<spec path>"}

Exit codes:
  0 — succeeded, JSON on stdout (may be empty arrays)
  2 — argument error
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
from pathlib import Path


def _spec_root_for_board(board: str) -> Path:
    """Match clawta-poller's spec_dir_for_board logic, simplified.

    For chitin-owned boards: <chitin-repo>/.specify/specs/
    For team boards (readybench): <workspace>/.specify/specs/ AND
    <bench-devs-platform>/.specify/specs/ (we check both; in-repo wins)
    """
    home = Path.home()
    candidates: list[Path] = []
    if board == "chitin":
        chitin_repo = Path(os.environ.get("CHITIN_REPO", home / "workspace" / "chitin"))
        candidates.append(chitin_repo / ".specify" / "specs")
    elif board == "readybench":
        # In-repo first (workers find it in their checkout)
        bdp = Path(os.environ.get("BENCH_DEVS_PLATFORM_REPO",
                                  home / "workspace" / "bench-devs-platform"))
        candidates.append(bdp / ".specify" / "specs")
        # Workspace overlay as fallback
        workspace = Path(os.environ.get("WORKSPACE_ROOT", home / "workspace"))
        candidates.append(workspace / ".specify" / "specs")
    else:
        # Default to workspace overlay for unknown boards
        candidates.append(Path(home / "workspace" / ".specify" / "specs"))
    for c in candidates:
        if c.is_dir():
            return c
    return candidates[0]


_TICKET_RE_TEMPLATE = r"(?<![A-Za-z0-9_]){tid}(?![A-Za-z0-9_])"


def _find_spec_for_ticket(spec_root: Path, ticket_id: str) -> Path | None:
    """Return the first spec.md under spec_root that mentions ticket_id."""
    if not spec_root.is_dir():
        return None
    pat = re.compile(_TICKET_RE_TEMPLATE.format(tid=re.escape(ticket_id)))
    for spec_path in sorted(spec_root.glob("*/spec.md")):
        try:
            if pat.search(spec_path.read_text(errors="ignore")):
                return spec_path
        except OSError:
            continue
    return None


def parse_scope_section(spec_md: str) -> tuple[list[str], list[str]]:
    """Extract MAY-write and MUST-NOT-write globs from the
    `## File-system scope` section of a spec.md.

    Looks for two sublists under the section, headed by lines starting
    with 'Worker MAY write under:' and 'Worker MUST NOT write under:'.
    List items are markdown bullets starting with `- ` (single backtick
    around the glob optional).

    Returns ([], []) if section absent or malformed.
    """
    lines = spec_md.splitlines()
    in_section = False
    in_may = False
    in_must_not = False
    may: list[str] = []
    must_not: list[str] = []

    for line in lines:
        stripped = line.strip()
        # Section header
        if stripped.startswith("## "):
            heading = stripped[3:].strip().lower()
            if "file-system scope" in heading or "file system scope" in heading:
                in_section = True
                in_may = False
                in_must_not = False
                continue
            # Any other h2 ends the section
            if in_section:
                break
            continue
        if not in_section:
            continue
        # Sub-heading lines inside the section
        low = stripped.lower()
        if low.startswith("worker may write") or low.startswith("may write"):
            in_may, in_must_not = True, False
            continue
        if low.startswith("worker must not write") or low.startswith("must not write"):
            in_may, in_must_not = False, True
            continue
        # List item
        if stripped.startswith("- "):
            item = stripped[2:].strip()
            # Strip backticks
            if item.startswith("`"):
                end = item.find("`", 1)
                if end > 0:
                    item = item[1:end]
                else:
                    item = item.lstrip("`")
            # Strip trailing inline comments (e.g. "apps/portal/**  -- the portal app")
            for sep in ("  --", "  —", "  #"):
                idx = item.find(sep)
                if idx >= 0:
                    item = item[:idx].strip()
            # Strip parenthetical annotations
            paren_idx = item.find(" (")
            if paren_idx > 0:
                item = item[:paren_idx].strip()
            if not item:
                continue
            if in_may:
                may.append(item)
            elif in_must_not:
                must_not.append(item)
    return may, must_not


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--board", help="board slug (chitin, readybench, …)")
    ap.add_argument("--ticket-id", help="ticket id, e.g. t_0e6027fb")
    ap.add_argument("--spec-file", help="absolute path to spec.md (skip resolution)")
    args = ap.parse_args()

    spec_file: Path | None = None
    if args.spec_file:
        spec_file = Path(args.spec_file).expanduser()
    elif args.board and args.ticket_id:
        spec_root = _spec_root_for_board(args.board)
        spec_file = _find_spec_for_ticket(spec_root, args.ticket_id)
    else:
        print("error: pass either --spec-file OR (--board + --ticket-id)",
              file=sys.stderr)
        return 2

    out = {"may": [], "must_not": [], "source": None}
    if spec_file and spec_file.is_file():
        try:
            content = spec_file.read_text(errors="ignore")
            may, must_not = parse_scope_section(content)
            out["may"] = may
            out["must_not"] = must_not
            out["source"] = str(spec_file)
        except OSError as e:
            print(f"warn: read spec failed: {e}", file=sys.stderr)
    # Always emit JSON on stdout. Empty arrays = no scope = back-compat.
    print(json.dumps(out))
    return 0


if __name__ == "__main__":
    sys.exit(main())
