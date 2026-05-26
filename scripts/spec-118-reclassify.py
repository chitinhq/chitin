#!/usr/bin/env python3
"""Replay factory_dispatch_failed events from the last 7 days by spec 118 kind."""

from __future__ import annotations

import glob
import json
import os
import re
from collections import Counter
from datetime import datetime, timedelta, timezone


KINDS = (
    "spec_ref_not_found",
    "spec_ref_ambiguous",
    "tasks_md_missing",
    "tasks_md_parse_error",
    "temporal_dial_failed",
    "temporal_start_workflow_failed",
    "capability_mismatch",
    "internal",
)


_NS_FRACTION_RE = re.compile(r"\.(\d+)")


def parse_ts(value: str) -> datetime | None:
    if not value:
        return None
    # chitin events are time.RFC3339Nano (up to 9 fractional-second digits);
    # datetime.fromisoformat only accepts 0/3/6 digits, so truncate to microseconds.
    def _truncate(match: re.Match[str]) -> str:
        return "." + match.group(1)[:6]

    normalized = _NS_FRACTION_RE.sub(_truncate, value).replace("Z", "+00:00")
    try:
        return datetime.fromisoformat(normalized)
    except ValueError:
        return None


def classify(detail: str) -> str:
    msg = detail.lower()
    if "no spec matching ref" in msg:
        return "spec_ref_not_found"
    if " is ambiguous" in msg or ("ref " in msg and "ambiguous" in msg):
        return "spec_ref_ambiguous"
    if "tasks.md" in msg and ("required artifact is missing" in msg or "no such file" in msg):
        return "tasks_md_missing"
    if "tasks.md" in msg and (
        "malformed artifact" in msg or "parse" in msg or "not a spec-kit" in msg
    ):
        return "tasks_md_parse_error"
    if "temporal unreachable" in msg or "client.dial" in msg or "dial tcp" in msg:
        return "temporal_dial_failed"
    if "executeworkflow failed" in msg or "startworkflow" in msg:
        return "temporal_start_workflow_failed"
    if (
        "dag validation failed" in msg
        or "unroutable" in msg
        or "no registered driver declares" in msg
        or "capability mismatch" in msg
    ):
        return "capability_mismatch"
    return "internal"


def main() -> int:
    chitin_dir = os.environ.get("CHITIN_DIR") or os.path.expanduser("~/.chitin")
    cutoff = datetime.now(timezone.utc) - timedelta(days=7)
    counts: Counter[str] = Counter({kind: 0 for kind in KINDS})
    total = 0

    for path in sorted(glob.glob(os.path.join(chitin_dir, "events-*.jsonl"))):
        with open(path, "r", encoding="utf-8") as f:
            for line in f:
                try:
                    row = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if row.get("event_type") != "factory_dispatch_failed":
                    continue
                ts = parse_ts(str(row.get("ts", "")))
                if ts is None or ts < cutoff:
                    continue
                payload = row.get("payload") or {}
                detail = str(payload.get("error") or payload.get("detail") or "")
                counts[classify(detail)] += 1
                total += 1

    print(f"factory_dispatch_failed events since {cutoff.isoformat()}: {total}")
    for kind in KINDS:
        print(f"{kind}\t{counts[kind]}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
