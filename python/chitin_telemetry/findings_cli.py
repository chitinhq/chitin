"""CLI surface for findings — the structured agent contract.

# Findings JSON contract (v1)

`argus findings --since <epoch_or_iso> [--severity LEVEL]` emits a
JSON array of findings, one per line (or pretty-printed with
`--pretty`):

```json
{
  "schema_version": 1,
  "id": 42,
  "finding_hash": "...",
  "ts_unix": 1715567890,
  "detector": "deny_cluster",
  "severity": "warning",
  "title": "Deny cluster: 8 denies in 300s",
  "body": "...",                            # detector-specific JSON or markdown
  "citations": ["t_abc123", "lockdown"],
  "operator_action": null,                   # null | "ack" | "snooze" | "flag" | "apply"
  "operator_action_ts": null,
  "pushed_ts": null,
  "superseded_by": null
}
```

This contract is **stable**. New fields may be added without bumping
`schema_version`. Renames or removals bump it.

`argus finding {ack,snooze,flag,apply} <id>` updates operator action.
"""
from __future__ import annotations

import json
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from chitin_telemetry import findings_store, migrations


SCHEMA_VERSION = 1


def _parse_since(raw: str) -> int:
    """Parse a since-ts as either unix epoch int OR ISO 8601."""
    raw = raw.strip()
    try:
        return int(raw)
    except ValueError:
        pass
    try:
        # Accept Z suffix
        dt = datetime.fromisoformat(raw.replace("Z", "+00:00"))
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        return int(dt.timestamp())
    except ValueError as e:
        raise ValueError(f"unrecognized timestamp: {raw!r}") from e


def cmd_findings(args) -> int:
    """List findings since a timestamp as JSON."""
    db_path = Path(args.db_path)
    if not db_path.exists():
        print(f"chitin-telemetry: index not found at {db_path}", file=sys.stderr)
        return 1
    try:
        since_ts = _parse_since(args.since)
    except ValueError as e:
        print(f"chitin-telemetry: {e}", file=sys.stderr)
        return 2

    conn = migrations.open_readonly(db_path)
    rows = findings_store.since(
        conn,
        since_ts,
        severity=args.severity,
        include_acked=args.include_acked,
        limit=args.limit,
    )
    payload = [
        {
            "schema_version": SCHEMA_VERSION,
            "id": r.id,
            "finding_hash": r.finding_hash,
            "ts_unix": r.ts_unix,
            "ts": datetime.fromtimestamp(r.ts_unix, tz=timezone.utc).isoformat(),
            "detector": r.detector,
            "severity": r.severity,
            "title": r.title,
            "body": r.body,
            "citations": r.citations,
            "operator_action": r.operator_action,
            "operator_action_ts": r.operator_action_ts,
            "pushed_ts": r.pushed_ts,
            "superseded_by": r.superseded_by,
        }
        for r in rows
    ]
    if args.pretty:
        print(json.dumps(payload, indent=2))
    else:
        for item in payload:
            print(json.dumps(item))
    return 0


def cmd_finding_action(args) -> int:
    """Set operator action on a finding (ack/snooze/flag/apply)."""
    db_path = Path(args.db_path)
    if not db_path.exists():
        print(f"chitin-telemetry: index not found at {db_path}", file=sys.stderr)
        return 1
    conn = migrations.open_writable(db_path)
    ok = findings_store.set_operator_action(conn, int(args.finding_id), args.action)
    conn.close()
    if not ok:
        print(f"chitin-telemetry: no finding with id={args.finding_id}", file=sys.stderr)
        return 3
    print(f"chitin-telemetry: finding {args.finding_id} -> {args.action}", file=sys.stderr)
    return 0


def cmd_action_rate(args) -> int:
    """Print operator engagement metrics."""
    db_path = Path(args.db_path)
    if not db_path.exists():
        print(f"chitin-telemetry: index not found at {db_path}", file=sys.stderr)
        return 1
    conn = migrations.open_readonly(db_path)
    metrics = findings_store.action_rate(conn, window_days=args.window_days)
    metrics["schema_version"] = SCHEMA_VERSION
    metrics["computed_ts_unix"] = int(time.time())
    print(json.dumps(metrics, indent=2))
    return 0
