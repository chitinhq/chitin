#!/usr/bin/env python3
"""Replay recent factory_dispatch_failed events into spec 118 taxonomy."""

from __future__ import annotations

import glob
import json
import os
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


def parse_ts(raw: str) -> datetime | None:
    if not raw:
        return None
    try:
        if raw.endswith("Z"):
            raw = raw[:-1] + "+00:00"
        return datetime.fromisoformat(raw)
    except ValueError:
        return None


def classify(detail: str) -> str:
    msg = detail.lower()
    if "no spec matching ref" in msg or "no spec directory matching" in msg:
        return "spec_ref_not_found"
    if ("spec ref" in msg and "ambiguous" in msg) or ("ref " in msg and " is ambiguous" in msg):
        return "spec_ref_ambiguous"
    if "tasks.md" in msg and (
        "required artifact is missing" in msg or "no such file" in msg or "missing" in msg
    ):
        return "tasks_md_missing"
    if "tasks.md" in msg and ("parse" in msg or "malformed artifact" in msg or "compile failed" in msg):
        return "tasks_md_parse_error"
    if "temporal unreachable" in msg or "client.dial" in msg or "dial tcp" in msg:
        return "temporal_dial_failed"
    if "executeworkflow failed" in msg or "startworkflow" in msg:
        return "temporal_start_workflow_failed"
    if (
        "dag validation failed" in msg
        or "unroutable" in msg
        or "no registered driver declares" in msg
        or "capability is not in the closed taxonomy" in msg
    ):
        return "capability_mismatch"
    return "internal"


def main() -> int:
    chain_dir = os.environ.get("CHITIN_DIR") or os.path.join(os.path.expanduser("~"), ".chitin")
    cutoff = datetime.now(timezone.utc) - timedelta(days=7)
    hist: Counter[str] = Counter()
    total = 0

    for path in sorted(glob.glob(os.path.join(chain_dir, "events-*.jsonl"))):
        with open(path, "r", encoding="utf-8") as fh:
            for line in fh:
                try:
                    row = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if row.get("event_type") != "factory_dispatch_failed":
                    continue
                ts = parse_ts(str(row.get("ts", "")))
                if ts is not None and ts < cutoff:
                    continue
                payload = row.get("payload") or {}
                detail = str(payload.get("detail") or payload.get("error") or "")
                hist[classify(detail)] += 1
                total += 1

    print(f"factory_dispatch_failed events in last 7 days: {total}")
    for kind in KINDS:
        print(f"{kind}\t{hist[kind]}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
