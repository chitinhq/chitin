#!/usr/bin/env python3
# spec: 020-sdd-tdd-enforcement
"""L2 pre-commit checker: spec has test coverage.

Usage:
  echo "<one staged file per line>" | python3 worker-pre-commit-spec-has-test-coverage.py --repo-root <path>

Exits 0 if all staged spec and test files meet coverage requirements.
Exits 1 and prints diagnostic to stderr when rejecting.

Per spec 020 Layer 2:
- Staged spec.md files must contain a ## Test coverage section with ≥1 table row
  binding an AC to a named test case.
- Staged test files under recognized test directories must contain a
  `// spec: NNN-<slug>` (or `# spec:` / `/* spec:`) reference in the first 20 lines.
"""
from __future__ import annotations

import os
import re
import sys

SPEC_PATTERN = re.compile(r"^\.specify/specs/[^/]+/spec\.md$")
TEST_DIR_PATTERNS = [
    re.compile(r"/__tests__/"),
    re.compile(r"/tests/"),
    re.compile(r"/e2e/"),
    re.compile(r"\.test\.[a-zA-Z0-9]+$"),
    re.compile(r"\.spec\.[a-zA-Z0-9]+$"),
    re.compile(r"_test\.go$"),
    re.compile(r"/test_.*\.py$"),
]
SPEC_REF_RE = re.compile(r"(?://|#|/\*)\s*spec:\s*\d{3,}-[a-z0-9-]+", re.IGNORECASE)

TEST_COVERAGE_HEADING_RE = re.compile(r"^##\s+Test\s+coverage", re.IGNORECASE)
TABLE_ROW_RE = re.compile(r"^\|\s*.*\|\s*.*\|", re.MULTILINE)


def is_spec_file(path: str) -> bool:
    return bool(SPEC_PATTERN.match(path))


def is_test_file(path: str) -> bool:
    return any(p.search(path) for p in TEST_DIR_PATTERNS)


def check_spec_has_test_coverage(filepath: str) -> list[str]:
    """Check that a spec.md has a ## Test coverage section with ≥1 table row."""
    errors = []
    try:
        content = open(filepath).read()
    except OSError as e:
        errors.append(f"Cannot read {filepath}: {e}")
        return errors

    # Find ## Test coverage section
    match = TEST_COVERAGE_HEADING_RE.search(content)
    if not match:
        errors.append(f"{filepath}: missing '## Test coverage' section")
        return errors

    # Extract section content (from heading to next ## heading or EOF)
    start = match.start()
    # Find next ## heading after this one
    rest = content[match.end():]
    next_heading = re.search(r"\n##\s", rest)
    section = rest[:next_heading.start()] if next_heading else rest

    # Check for at least one table row (line starting with | that isn't a separator)
    rows = [line for line in section.split("\n")
            if line.strip().startswith("|") and not re.match(r"^\|[\s\-:|]+\|$", line.strip())]
    if len(rows) < 1:
        errors.append(f"{filepath}: '## Test coverage' section has no table rows binding ACs to test cases")

    return errors


def check_test_has_spec_reference(filepath: str) -> list[str]:
    """Check that a test file has a spec reference in its first 20 lines."""
    errors = []
    try:
        with open(filepath) as f:
            lines = [f.readline() for _ in range(20)]
    except OSError as e:
        errors.append(f"Cannot read {filepath}: {e}")
        return errors

    for line in lines:
        if SPEC_REF_RE.search(line):
            return []  # Found reference

    errors.append(f"{filepath}: no 'spec: NNN-<slug>' reference found in first 20 lines")
    return errors


def main() -> int:
    repo_root = ""
    args = sys.argv[1:]
    i = 0
    while i < len(args):
        if args[i] == "--repo-root":
            if i + 1 < len(args):
                repo_root = args[i + 1]
                i += 2
            else:
                i += 1
        else:
            i += 1

    staged = [line.strip() for line in sys.stdin if line.strip()]
    if not staged:
        return 0

    errors = []

    for path in staged:
        full_path = os.path.join(repo_root, path) if repo_root else path
        if is_spec_file(path):
            errors.extend(check_spec_has_test_coverage(full_path))
        if is_test_file(path):
            errors.extend(check_test_has_spec_reference(full_path))

    if errors:
        for e in errors:
            print(e, file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())