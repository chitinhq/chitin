"""Debt stream stub. Foundation-generalization proof — produces valid empty
findings via the same writers as decisions.py.

v1: empty patterns, valid JSON/markdown. Plug in real detection in v2.
"""
from __future__ import annotations

import argparse
import sys
from datetime import datetime, timezone
from pathlib import Path

from analysis.writers import write_json


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="analysis.debt")
    p.add_argument("--out-dir", default="python/analysis/out")
    p.add_argument("--now", default=None)
    return p.parse_args(argv)


from dataclasses import dataclass
from typing import List, Optional, Any
import re
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


@dataclass
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


def load_ledger(path: Path) -> list[DebtEntry]:
    """
    Parses docs/debt-ledger.md's yaml-fenced sections into DebtEntry dataclasses.
    Skips malformed entries, counts parse_errors (never raises).
    """
    import yaml
    entries = []
    parse_errors = 0
    try:
        text = path.read_text()
    except Exception:
        return []
    # Find all yaml-fenced code blocks
    pattern = re.compile(r"```yaml(.*?)```", re.DOTALL)
    for match in pattern.finditer(text):
        block = match.group(1)
        try:
            data = yaml.safe_load(block)
            # Validate required fields
            required = ["id", "severity", "category", "file", "status", "description", "discovered_at", "discovered_by"]
            if not all(k in data for k in required):
                parse_errors += 1
                continue
            entry = DebtEntry(
                id=data["id"],
                severity=data["severity"],
                category=data["category"],
                file=data["file"],
                status=data["status"],
                shipped_in=data.get("shipped_in"),
                description=data["description"],
                discovered_at=data["discovered_at"],
                discovered_by=data["discovered_by"]
            )
            entries.append(entry)
        except Exception:
            parse_errors += 1
            continue
    return entries

if __name__ == "__main__":
    sys.exit(main())
