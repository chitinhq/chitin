"""Souls stream stub. Foundation-generalization proof.

This module exists to prove the writers + JSON schema generalize past
the decisions stream. It produces a valid empty findings document for the
souls stream so consumers can branch on `stream` without crashing on
schema-mismatch when souls hasn't shipped real detection yet.

Invariants (see SPEC.md):
    I2  Emits both JSON canonical + markdown projection.
    I3  No network.
    I6  Output goes to `out/` only.
    Schema-version stamping uses `stream="souls"` so downstream readers can
    branch correctly.

Boundaries:
    - Always produces a 2-file artifact (JSON + markdown), even with zero
      findings. The empty-write IS the proof.
    - `--now` accepted for deterministic tests; otherwise wall-clock.

Replace this stub with real detection when the souls stream ships in v2.
"""
from __future__ import annotations

import argparse
import sys
from datetime import datetime, timezone
from pathlib import Path

from analysis.writers import write_json


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="analysis.souls")
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
    json_path = out_dir / f"souls-{date_str}.json"
    md_path = out_dir / f"souls-{date_str}.md"

    write_json(json_path, findings=[], no_template=[],
               input_summary={"total_decisions": 0, "denies": 0, "allows": 0,
                              "files_read": 0, "parse_errors": 0,
                              "distinct_rule_ids": 0,
                              "decisions_missing_envelope_id": 0},
               generated_at=now, window_since=now, window_until=now,
               window_size="7d", stream="souls")

    md_path.write_text(
        "# Souls Analysis — " + date_str + "\n\n"
        "_Stub stream. Real detection ships in v2._\n"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
