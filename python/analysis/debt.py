"""Debt stream — analysis-side parsers + reporting.

Two responsibilities live here:

1. **Stub stream** (the original v1 file): produces valid empty findings
   via the same writers as decisions.py. Foundation-generalization
   proof. Real detection ships in v2.

2. **Debt-ledger loader** (PR #142, follow-up to PR #137's
   `docs/debt-ledger.md`): parses the human-curated ledger into typed
   `DebtEntry` records the GROOM stage + analyst-role agents
   consume.
"""
from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import yaml

from analysis.writers import write_json


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="analysis.debt")
    p.add_argument("--out-dir", default="python/analysis/out")
    p.add_argument("--now", default=None)
    return p.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.now:
        now = datetime.fromisoformat(args.now)
        if now.tzinfo is None:
            now = now.replace(tzinfo=timezone.utc)
    else:
        now = datetime.now(tz=timezone.utc)

    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)
    date_str = now.date().isoformat()
    json_path = out_dir / f"debt-{date_str}.json"
    md_path = out_dir / f"debt-{date_str}.md"

    write_json(json_path, findings=[], no_template=[],
               input_summary={"total_decisions": 0, "denies": 0, "allows": 0,
                              "files_read": 0, "parse_errors": 0,
                              "distinct_rule_ids": 0,
                              "decisions_missing_envelope_id": 0},
               generated_at=now, window_since=now, window_until=now,
               window_size="7d", stream="debt")

    md_path.write_text(
        "# Debt Analysis — " + date_str + "\n\n"
        "_Stub stream. Real detection ships in v2._\n"
    )
    return 0


# ---------------------------------------------------------------------------
# Debt-ledger loader (PR #142)
#
# Schema mirrors `docs/debt-ledger.md`'s yaml-fenced sections:
#
#     id: <slug>
#     discovered_at: <ISO-8601>
#     discovered_by: <swarm | operator | user>
#     severity: blocking | high | medium | low
#     category: code-debt | doc-debt | infra-debt | governance-debt
#     file: <primary file or 'cross-cutting'>
#     status: open | claimed | shipped
#     shipped_in: <PR # if shipped>
#     description: |
#       What's wrong / why it's debt / what scenario it bites in.
#
# `load_ledger` is tolerant of malformed entries — it skips them, emits
# a single stderr warning if any parse errors fired, and returns the
# well-formed entries. Mirrors the never-raise contract of
# `loaders.load_gov_decisions`.
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class DebtEntry:
    id: str
    severity: str
    category: str
    file: str
    status: str
    shipped_in: Optional[str]
    description: str
    discovered_at: str
    discovered_by: str


@dataclass(frozen=True)
class LedgerLoadResult:
    """Mirror of `loaders.LoadResult` for the debt-ledger loader. Exposes
    `parse_errors` so callers/tests can observe the skip-count rather
    than relying on the stderr warning alone."""

    entries: list[DebtEntry]
    parse_errors: int


_REQUIRED_FIELDS = (
    "id",
    "severity",
    "category",
    "file",
    "status",
    "description",
    "discovered_at",
    "discovered_by",
)

_YAML_FENCE_RE = re.compile(r"```yaml\s*(.*?)```", re.DOTALL)


def _stringify_timestamp(value: object) -> str:
    """PyYAML auto-converts unquoted ISO-8601 strings to `datetime`
    objects (and date-only to `date`). Normalize back to ISO-8601 so
    `DebtEntry.discovered_at` is always a string regardless of yaml
    quoting in the source."""
    if isinstance(value, datetime):
        # `datetime.isoformat()` uses '+00:00' for UTC; the doc convention
        # is the 'Z' suffix. Translate so the output round-trips with
        # what the operator typed in the ledger.
        s = value.isoformat()
        return s.replace("+00:00", "Z")
    return str(value)


def load_ledger(path: Path) -> LedgerLoadResult:
    """Parse `docs/debt-ledger.md`'s yaml-fenced sections into typed
    `DebtEntry` records.

    - Missing file → empty result (never raise)
    - Malformed yaml block → skip + count toward parse_errors
    - Missing required fields → skip + count toward parse_errors
    - parse_errors > 0 → single stderr warning at end (operator
      visibility without making the loader lossy)

    The order of returned entries matches the order of yaml-fenced
    sections in the source file — useful for the GROOM stage which
    wants to see the most recent debt first (entries are listed
    newest-first by convention).

    Returns a `LedgerLoadResult` (mirroring `loaders.LoadResult`) so
    callers can observe `parse_errors` directly rather than relying on
    the stderr warning alone.
    """
    try:
        text = path.read_text()
    except OSError:
        return LedgerLoadResult(entries=[], parse_errors=0)

    entries: list[DebtEntry] = []
    parse_errors = 0
    for match in _YAML_FENCE_RE.finditer(text):
        block = match.group(1)
        try:
            data = yaml.safe_load(block)
        except yaml.YAMLError:
            parse_errors += 1
            continue
        if not isinstance(data, dict):
            parse_errors += 1
            continue
        if not all(k in data for k in _REQUIRED_FIELDS):
            parse_errors += 1
            continue
        try:
            entry = DebtEntry(
                id=str(data["id"]),
                severity=str(data["severity"]),
                category=str(data["category"]),
                file=str(data["file"]),
                status=str(data["status"]),
                shipped_in=str(data["shipped_in"]) if data.get("shipped_in") else None,
                description=str(data["description"]).strip(),
                discovered_at=_stringify_timestamp(data["discovered_at"]),
                discovered_by=str(data["discovered_by"]),
            )
        except (TypeError, ValueError):
            parse_errors += 1
            continue
        entries.append(entry)

    if parse_errors > 0:
        print(
            f"[debt.load_ledger] {parse_errors} malformed entries skipped in {path}",
            file=sys.stderr,
        )
    return LedgerLoadResult(entries=entries, parse_errors=parse_errors)


def filter_by_status(entries: list[DebtEntry], status: str) -> list[DebtEntry]:
    """Most callers want only `open` or `claimed` entries when sizing new
    work; `shipped` are historical."""
    return [e for e in entries if e.status == status]


_SEVERITY_ORDER = {"blocking": 4, "high": 3, "medium": 2, "low": 1}


def filter_by_severity(entries: list[DebtEntry], min_severity: str) -> list[DebtEntry]:
    """Return entries at or above `min_severity`. Order is
    blocking > high > medium > low."""
    threshold = _SEVERITY_ORDER.get(min_severity)
    if threshold is None:
        raise ValueError(f"unknown severity: {min_severity!r}")
    return [e for e in entries if _SEVERITY_ORDER.get(e.severity, 0) >= threshold]


if __name__ == "__main__":
    sys.exit(main())
