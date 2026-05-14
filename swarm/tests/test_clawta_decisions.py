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
                    SELECT driver, model, selection_mode, reasoning
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
                    "exploration",
                    "Controlled exploration picked Gemini Flash.",
                ),
            )

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
            self.assertEqual(payload["selection_mode"], "exploration")


if __name__ == "__main__":
    unittest.main()
