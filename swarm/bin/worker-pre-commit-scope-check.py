#!/usr/bin/env python3
"""Path-scope check for worker pre-commit hook.

Usage:
  echo "<one staged file per line>" | python3 worker-pre-commit-scope-check.py <scope.json>

Exits 0 if all staged files are in scope, 1 if any are out of scope.
Prints offending paths to stdout (one per line) when rejecting.
"""
from __future__ import annotations

import json
import re
import sys


def glob_to_re(pattern: str) -> re.Pattern:
    out = ""
    i = 0
    while i < len(pattern):
        c = pattern[i]
        if c == "*" and i + 1 < len(pattern) and pattern[i + 1] == "*":
            out += ".*"
            i += 2
            if i < len(pattern) and pattern[i] == "/":
                i += 1
        elif c == "*":
            out += "[^/]*"
            i += 1
        elif c == "?":
            out += "[^/]"
            i += 1
        elif c in ".^$+(){}[]|\\":
            out += "\\" + c
            i += 1
        else:
            out += c
            i += 1
    return re.compile("^" + out + "$")


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: worker-pre-commit-scope-check.py <scope.json>", file=sys.stderr)
        return 2
    try:
        with open(sys.argv[1]) as f:
            scope = json.load(f)
    except (OSError, json.JSONDecodeError) as e:
        print(f"scope file unreadable: {e}", file=sys.stderr)
        return 0  # fail open if scope file broken (post-spawn validator backstops)

    may = scope.get("may") or []
    must_not = scope.get("must_not") or []
    if not may and not must_not:
        return 0  # no scope declared = no enforcement

    may_re = [glob_to_re(g) for g in may]
    must_not_re = [glob_to_re(g) for g in must_not]

    staged = [line.strip() for line in sys.stdin if line.strip()]
    offending: list[str] = []
    for path in staged:
        if any(rx.match(path) for rx in must_not_re):
            offending.append(f"{path} (matches MUST_NOT)")
            continue
        if may_re and not any(rx.match(path) for rx in may_re):
            offending.append(f"{path} (not in MAY)")

    if offending:
        for off in offending:
            print(off)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
