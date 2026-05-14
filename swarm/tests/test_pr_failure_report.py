from __future__ import annotations

import importlib.machinery
import importlib.util
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "workflows" / "pr_failure_report.py"


def load_module():
    loader = importlib.machinery.SourceFileLoader("pr_failure_report", str(SCRIPT))
    spec = importlib.util.spec_from_loader("pr_failure_report", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


class PRFailureReportTests(unittest.TestCase):
    def test_synthetic_empty_branch_failure_surfaces_gh_error(self):
        module = load_module()
        report = module.build_pr_failure_report(
            "t_2111ce54",
            "swarm/codex-2111ce54",
            "",
            "GraphQL: No commits between main and swarm/codex-2111ce54\n",
            1,
        )

        self.assertIn("gh pr create failed", report["message"])
        self.assertIn("stderr=", report["message"])
        self.assertIn("exit_code=1", report["message"])
        self.assertIn("No commits between main and swarm/codex-2111ce54", report["message"])
        self.assertIn("No commits between main and swarm/codex-2111ce54", report["block_reason"])

    def test_report_labels_stdout_and_stderr_separately(self):
        module = load_module()
        report = module.build_pr_failure_report(
            "t_2111ce54",
            "branch",
            "warning on stdout\n",
            "fatal on stderr\n",
            1,
        )

        self.assertIn("stdout=warning on stdout", report["message"])
        self.assertIn("stderr=fatal on stderr", report["message"])
        self.assertIn("exit_code=1", report["block_reason"])

    def test_report_truncates_gh_output_to_sane_limit(self):
        module = load_module()
        noisy_output = ("stderr line\n" * 400).strip()

        report = module.build_pr_failure_report("t_2111ce54", "branch", "", noisy_output, 1)

        self.assertLessEqual(len(report["message"]), 1300)
        self.assertLessEqual(len(report["block_reason"]), 1200)
        self.assertTrue(report["message"].endswith("..."))


if __name__ == "__main__":
    unittest.main()
