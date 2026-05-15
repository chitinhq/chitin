#!/usr/bin/env python3
"""Behavior tests for swarm/workflows/clawta_decisions.py."""

from __future__ import annotations

import json
import sqlite3
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "workflows" / "clawta_decisions.py"


class ClawtaDecisionsTests(unittest.TestCase):
    def test_record_and_latest_preserve_selection_mode(self):
        with tempfile.TemporaryDirectory() as tmp:
            db = Path(tmp) / "clawta_decisions.db"
            record = subprocess.run(
                [
                    "python3",
                    str(SCRIPT),
                    "record",
                    "--db",
                    str(db),
                    "--ticket-id",
                    "t_77433314",
                    "--driver",
                    "gemini",
                    "--model",
                    "gemini-2.5-flash",
                    "--shape-bucket",
                    "medium|python+review",
                    "--selection-mode",
                    "exploration",
                    "--no-chain",
                ],
                input="Controlled exploration picked Gemini Flash.",
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(record.returncode, 0, msg=record.stderr)
            self.assertIn("Routing (exploration)", record.stdout)

            with sqlite3.connect(db) as conn:
                row = conn.execute(
                    """
                    SELECT driver, model, shape_bucket, selection_mode, reasoning, outcome
                    FROM clawta_decisions
                    ORDER BY id DESC
                    LIMIT 1
                    """
                ).fetchone()
            self.assertEqual(
                row,
                (
                    "gemini",
                    "gemini-2.5-flash",
                    "medium|python+review",
                    "exploration",
                    "Controlled exploration picked Gemini Flash.",
                    "pending",
                ),
            )

            outcome = subprocess.run(
                [
                    "python3",
                    str(SCRIPT),
                    "mark-outcome",
                    "--db",
                    str(db),
                    "--ticket-id",
                    "t_77433314",
                    "--outcome",
                    "failure",
                    "--failure-kind",
                    "ci_fail",
                ],
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(outcome.returncode, 0, msg=outcome.stderr)

            latest = subprocess.run(
                [
                    "python3",
                    str(SCRIPT),
                    "latest",
                    "--db",
                    str(db),
                    "--ticket-id",
                    "t_77433314",
                    "--json",
                ],
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(latest.returncode, 0, msg=latest.stderr)
            payload = json.loads(latest.stdout)
            self.assertEqual(payload["shape_bucket"], "medium|python+review")
            self.assertEqual(payload["selection_mode"], "exploration")
            self.assertEqual(payload["outcome"], "failure")
            self.assertEqual(payload["failure_kind"], "ci_fail")


if __name__ == "__main__":
    unittest.main()
