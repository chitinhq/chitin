#!/usr/bin/env python3
# spec: 020-sdd-tdd-enforcement
"""L1 pre-commit checker: no code without test.

Usage:
  echo "<one staged file per line>" | python3 worker-pre-commit-no-code-without-test.py [--commit-message MSG]

Exits 0 if all staged code files have a matching test or escape clause.
Exits 1 and prints offending code files to stdout when rejecting.

Per spec 020 Layer 1:
- Code paths: src/**, routes/**, services/**, lib/**, controllers/**, models/**
- Test paths: __tests__/**, tests/**, e2e/**, *.test.*, *.spec.*
- Escape clause: 'no-test-change-justified:' line in commit message
"""
from __future__ import annotations

import re
import sys

CODE_PATTERNS = [
    re.compile(r"^src/"),
    re.compile(r"^routes/"),
    re.compile(r"^services/"),
    re.compile(r"^lib/"),
    re.compile(r"^controllers/"),
    re.compile(r"^models/"),
]

TEST_PATTERNS = [
    re.compile(r"/__tests__/"),
    re.compile(r"/tests/"),
    re.compile(r"/e2e/"),
    re.compile(r"\.test\.[a-zA-Z0-9]+$"),
    re.compile(r"\.spec\.[a-zA-Z0-9]+$"),
]

ESCAPE_RE = re.compile(r"no-test-change-justified:\s*\S", re.IGNORECASE)


def is_code_path(path: str) -> bool:
    return any(p.search(path) for p in CODE_PATTERNS)


def is_test_path(path: str) -> bool:
    return any(p.search(path) for p in TEST_PATTERNS)


def main() -> int:
    commit_msg = ""
    args = sys.argv[1:]
    i = 0
    while i < len(args):
        if args[i] == "--commit-message":
            if i + 1 < len(args):
                commit_msg = args[i + 1]
                i += 2
            else:
                i += 1
        else:
            i += 1

    staged = [line.strip() for line in sys.stdin if line.strip()]
    if not staged:
        return 0

    code_files = [f for f in staged if is_code_path(f)]
    if not code_files:
        return 0

    test_files = [f for f in staged if is_test_path(f)]
    has_escape = bool(ESCAPE_RE.search(commit_msg))

    if test_files or has_escape:
        return 0

    # Rejection — print offending code files
    for f in code_files:
        print(f)
    return 1


if __name__ == "__main__":
    sys.exit(main())