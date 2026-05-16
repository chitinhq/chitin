#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

python3 - <<'PY'
from __future__ import annotations

import fnmatch
import sys
from datetime import date
from pathlib import Path

try:
    import yaml
except ModuleNotFoundError as exc:
    print(f"ERROR: {exc}. Install PyYAML to run scripts/check-scripts-manifest.sh.", file=sys.stderr)
    sys.exit(2)


REPO_ROOT = Path.cwd()
MANIFEST_PATH = REPO_ROOT / "scripts" / "MANIFEST.yaml"
ALLOWED_CATEGORIES = {"ci", "migration", "operator", "runtime-critical"}


def fail_closed(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr)
    sys.exit(2)


if not MANIFEST_PATH.is_file():
    fail_closed("scripts/MANIFEST.yaml is missing")

try:
    manifest = yaml.safe_load(MANIFEST_PATH.read_text())
except yaml.YAMLError as exc:
    fail_closed(f"scripts/MANIFEST.yaml is malformed: {exc}")

if manifest is None:
    manifest = {}
if not isinstance(manifest, dict):
    fail_closed("scripts/MANIFEST.yaml must be a mapping with exclude_patterns and entries")

exclude_patterns = manifest.get("exclude_patterns", [])
entries_raw = manifest.get("entries", [])
if exclude_patterns is None:
    exclude_patterns = []
if entries_raw is None:
    entries_raw = []

if not isinstance(exclude_patterns, list) or any(not isinstance(item, str) for item in exclude_patterns):
    fail_closed("exclude_patterns must be a list of glob strings")
if not isinstance(entries_raw, list):
    fail_closed("entries must be a list")


def is_excluded(rel_path: str) -> bool:
    return any(fnmatch.fnmatch(rel_path, pattern) for pattern in exclude_patterns)


manifest_paths: list[str] = []
duplicate_paths: list[str] = []
seen_paths: set[str] = set()
schema_errors: list[str] = []
runtime_coverage_errors: list[str] = []
migration_errors: list[str] = []
stale_entries: list[str] = []

for index, item in enumerate(entries_raw):
    if not isinstance(item, dict):
        schema_errors.append(f"entries[{index}] must be a mapping")
        continue
    path = item.get("path")
    category = item.get("category")
    purpose = item.get("purpose")
    if not isinstance(path, str) or not path:
        schema_errors.append(f"entries[{index}] missing string path")
        continue
    if not isinstance(category, str) or category not in ALLOWED_CATEGORIES:
        schema_errors.append(
            f"{path}: category must be one of {', '.join(sorted(ALLOWED_CATEGORIES))}"
        )
    if not isinstance(purpose, str) or not purpose.strip():
        schema_errors.append(f"{path}: purpose must be a non-empty string")
    if path in seen_paths:
        duplicate_paths.append(path)
    seen_paths.add(path)
    manifest_paths.append(path)
    entry_file = REPO_ROOT / path
    if not entry_file.is_file():
        stale_entries.append(path)

    if category == "runtime-critical":
        tested_by = item.get("tested_by")
        port_ticket = item.get("port_ticket")
        has_tested_by = isinstance(tested_by, str) and tested_by.strip()
        has_port_ticket = isinstance(port_ticket, str) and port_ticket.strip()
        if not has_tested_by and not has_port_ticket:
            runtime_coverage_errors.append(
                f"{path}: runtime-critical entries need tested_by or port_ticket"
            )
        if has_tested_by and not (REPO_ROOT / tested_by).is_file():
            runtime_coverage_errors.append(f"{path}: tested_by target not found: {tested_by}")
    if category == "migration":
        added_on = item.get("added_on")
        expires_on = item.get("expires_on")
        if not isinstance(added_on, date):
            migration_errors.append(f"{path}: migration entries need added_on (YYYY-MM-DD)")
        if not isinstance(expires_on, date):
            migration_errors.append(f"{path}: migration entries need expires_on (YYYY-MM-DD)")
        if isinstance(added_on, date) and isinstance(expires_on, date) and expires_on < date.today():
            migration_errors.append(
                f"{path}: expired migration (expires_on {expires_on.isoformat()} < today {date.today().isoformat()})"
            )

if schema_errors or duplicate_paths:
    if duplicate_paths:
        schema_errors.extend(f"duplicate manifest entry: {path}" for path in sorted(set(duplicate_paths)))
    if stale_entries:
        schema_errors.extend(f"manifest entry points to missing file: {path}" for path in sorted(set(stale_entries)))
    print("scripts manifest schema errors:")
    for message in sorted(schema_errors):
        print(f"  - {message}")
    sys.exit(1)

actual_files = sorted(
    str(path.relative_to(REPO_ROOT))
    for path in (REPO_ROOT / "scripts").rglob("*")
    if path.is_file() and not is_excluded(str(path.relative_to(REPO_ROOT)))
)

untracked = sorted(set(actual_files) - set(manifest_paths))
missing = sorted(set(manifest_paths) - set(actual_files))

problems: list[tuple[str, list[str]]] = []
if untracked:
    problems.append(("untracked scripts", untracked))
if missing:
    problems.append(("manifest entries without files", missing))
if runtime_coverage_errors:
    problems.append(("runtime-critical coverage gaps", sorted(runtime_coverage_errors)))
if migration_errors:
    problems.append(("migration TTL errors", sorted(migration_errors)))

if problems:
    for title, items in problems:
        print(f"{title}:")
        for item in items:
            print(f"  - {item}")
    sys.exit(1)

print(
    f"scripts manifest OK: {len(actual_files)} tracked file(s), "
    f"{len(exclude_patterns)} exclude pattern(s)"
)
PY
