from __future__ import annotations

import importlib.machinery
import importlib.util
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "workflows" / "worker_failure_report.py"


def load_module():
    loader = importlib.machinery.SourceFileLoader("worker_failure_report", str(SCRIPT))
    spec = importlib.util.spec_from_loader("worker_failure_report", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


class WorkerFailureReportTests(unittest.TestCase):
    def test_completed_no_commit_surfaces_structured_comment(self):
        module = load_module()
        report = module.build_failure_report(
            "t_bd46cee9",
            {
                "status": "completed_no_commit",
                "exit_reason": "model-concluded-nothing",
                "driver": "copilot",
                "model": "gpt-4.1",
                "commit_count_ahead": 0,
                "transcript_tail": "[stderr] session ended\n[stdout] nothing to change",
                "error": "Worker session ended without creating any commits",
            },
        )

        self.assertIn("exit_reason=model-concluded-nothing", report["message"])
        self.assertIn("model=gpt-4.1", report["message"])
        self.assertIn("transcript_tail=", report["message"])
        self.assertNotIn("gh pr create failed", report["message"])
        self.assertEqual(report["block_reason"], "worker model-concluded-nothing")


if __name__ == "__main__":
    unittest.main()
