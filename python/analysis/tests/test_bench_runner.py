"""Integration coverage for the repo-local governance bench harness."""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path


def test_governance_bench_runner_passes():
    repo_root = Path(__file__).resolve().parents[3]
    result = subprocess.run(
        [sys.executable, "bench/run.py"],
        cwd=repo_root,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stderr

    summary_path = repo_root / "bench" / "out" / "governance-bench-summary.json"
    body = json.loads(summary_path.read_text())
    assert body["suite"] == "governance-bench"
    assert body["fail_count"] == 0
    assert body["pass_count"] == body["task_count"]
    assert {task["id"] for task in body["tasks"]} >= {
        "sentinel-candidate",
        "sentinel-empty",
        "sentinel-top-n",
        "sentinel-missing-dir",
    }
