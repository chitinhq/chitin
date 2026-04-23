#!/usr/bin/env python3
"""Apply Stage 2's SEARCH/REPLACE output to repo files, emit a unified diff.

The driver runs this in place of asking the model for a unified diff
directly — models are reliable at writing code but unreliable at the
line-number math unified diffs require. This helper never asks the
model for line numbers; it finds SEARCH text by exact string match,
substitutes, and produces the diff deterministically via difflib.

Input:
  stdin          : Stage 2's raw output (SEARCH/REPLACE blocks, possibly
                   wrapped in stray text / fences).
  --repo-root    : where the original files live (read-only).
  --plan         : path to plan.json; we only accept blocks whose path
                   is in plan.diff_request.files.
  --out-diff     : where to write the concatenated unified diff.

Exit codes:
  0  valid non-empty diff written to --out-diff.
  1  parse error, off-target path, or SEARCH not found in file.
  2  parse was clean but the resulting diff is empty (no-op edits).
"""
from __future__ import annotations

import argparse
import difflib
import json
import re
import sys
from pathlib import Path

FILE_MARKER = re.compile(r"^=== FILE: (.+?) ===\s*$")
SR_BLOCK = re.compile(
    r"<<<<<<<\s*SEARCH\s*\n(.*?)\n=======\s*\n(.*?)\n>>>>>>>\s*REPLACE",
    re.DOTALL,
)


def parse_blocks(text: str) -> list[tuple[str, str, str]]:
    """Return [(path, search, replace), ...] in emission order.

    Tolerates stray whitespace / markdown fences around the block
    boundaries. A block is bound to the most recent FILE marker.
    """
    out: list[tuple[str, str, str]] = []
    current_path: str | None = None
    current_body: list[str] = []

    def flush() -> None:
        nonlocal current_path, current_body
        if current_path is not None:
            body = "\n".join(current_body)
            for m in SR_BLOCK.finditer(body):
                out.append((current_path, m.group(1), m.group(2)))
        current_body = []

    for line in text.splitlines():
        m = FILE_MARKER.match(line)
        if m:
            flush()
            current_path = m.group(1).strip()
        else:
            current_body.append(line)
    flush()
    return out


def apply_blocks(original: str, blocks: list[tuple[str, str]]) -> str:
    """Apply SEARCH/REPLACE pairs sequentially. SEARCH must match verbatim."""
    text = original
    for i, (search, replace) in enumerate(blocks):
        idx = text.find(search)
        if idx == -1:
            head = search[:200]
            raise ValueError(
                f"block {i}: SEARCH not found in file (first 200 chars below)\n"
                f"--- SEARCH ---\n{head}\n--- END SEARCH ---"
            )
        text = text[:idx] + replace + text[idx + len(search):]
    return text


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo-root", required=True, type=Path)
    ap.add_argument("--plan", required=True, type=Path)
    ap.add_argument("--out-diff", required=True, type=Path)
    args = ap.parse_args()

    plan = json.loads(args.plan.read_text())
    allowed = set((plan.get("diff_request") or {}).get("files") or [])
    if not allowed:
        print("plan has no diff_request.files; nothing to accept", file=sys.stderr)
        return 1

    raw = sys.stdin.read()
    blocks = parse_blocks(raw)
    if not blocks:
        print("no SEARCH/REPLACE blocks found in Stage 2 output", file=sys.stderr)
        return 1

    grouped: dict[str, list[tuple[str, str]]] = {}
    for path, search, replace in blocks:
        if path not in allowed:
            print(f"block targets path outside plan.diff_request.files: {path}",
                  file=sys.stderr)
            return 1
        grouped.setdefault(path, []).append((search, replace))

    diff_parts: list[str] = []
    for path, pairs in grouped.items():
        src = args.repo_root / path
        if not src.exists():
            print(f"file not found under repo root: {path}", file=sys.stderr)
            return 1
        original = src.read_text()
        try:
            modified = apply_blocks(original, pairs)
        except ValueError as e:
            print(f"apply failed for {path}: {e}", file=sys.stderr)
            return 1
        if original == modified:
            continue
        diff_parts.extend(difflib.unified_diff(
            original.splitlines(keepends=True),
            modified.splitlines(keepends=True),
            fromfile=f"a/{path}",
            tofile=f"b/{path}",
        ))

    if not diff_parts:
        return 2

    args.out_diff.write_text("".join(diff_parts))
    return 0


if __name__ == "__main__":
    sys.exit(main())
