"""Load gov-decisions JSONL files with window filtering."""
from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import datetime, timedelta
from pathlib import Path

from analysis.types import Decision, parse_decision_line

GOV_DECISIONS_PATTERN = re.compile(r"^gov-decisions-\d{4}-\d{2}-\d{2}\.jsonl$")


@dataclass(frozen=True)
class Window:
    """Half-open time window: ts >= since AND ts < until."""

    since: datetime
    until: datetime


@dataclass(frozen=True)
class LoadResult:
    decisions: list[Decision]
    files_read: int
    parse_errors: int


def load_gov_decisions(decisions_dir: Path, window: Window) -> LoadResult:
    """Load all gov-decisions-*.jsonl files in dir, filter to window.

    Bad lines are counted in parse_errors and skipped — never raise (I5).
    """
    decisions_dir = Path(decisions_dir)
    if not decisions_dir.exists():
        return LoadResult(decisions=[], files_read=0, parse_errors=0)

    decisions: list[Decision] = []
    parse_errors = 0
    files_read = 0

    for path in sorted(decisions_dir.iterdir()):
        if not GOV_DECISIONS_PATTERN.match(path.name):
            continue
        files_read += 1
        with path.open("r") as f:
            for line in f:
                d = parse_decision_line(line)
                if d is None:
                    if line.strip():
                        parse_errors += 1
                    continue
                if window.since <= d.ts < window.until:
                    decisions.append(d)

    return LoadResult(
        decisions=decisions,
        files_read=files_read,
        parse_errors=parse_errors,
    )


def parse_window_str(s: str, now: datetime) -> Window:
    """Parse 'Nd' / 'Nh' / 'Nm' as a window ending at now."""
    if s.endswith("d"):
        return Window(since=now - timedelta(days=int(s[:-1])), until=now)
    if s.endswith("h"):
        return Window(since=now - timedelta(hours=int(s[:-1])), until=now)
    if s.endswith("m"):
        return Window(since=now - timedelta(minutes=int(s[:-1])), until=now)
    raise ValueError(f"Unrecognized window: {s!r}. Use Nd, Nh, or Nm.")
