"""Integration coverage for the repo-local governance bench harness."""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest


@pytest.fixture(scope="module")
def governance_bench_summary():
    repo_root = Path(__file__).resolve().parents[3]
    result = subprocess.run(
        [sys.executable, "bench/run.py"],
        cwd=repo_root,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stderr

    summary_path = repo_root / "bench" / "out" / "governance-bench-summary.json"
    return json.loads(summary_path.read_text())


def test_governance_bench_runner_passes(governance_bench_summary):
    body = governance_bench_summary
    assert body["suite"] == "governance-bench"
    assert body["fail_count"] == 0
    assert body["pass_count"] == body["task_count"]
    assert {task["id"] for task in body["tasks"]} >= {
        "sentinel-candidate",
        "sentinel-empty",
        "sentinel-top-n",
        "sentinel-missing-dir",
    }


def test_governance_bench_max_boundary_covers_top_n_truncation(governance_bench_summary):
    tasks = {task["id"]: task for task in governance_bench_summary["tasks"]}
    task = tasks["sentinel-top-n"]

    assert task["status"] == "pass"
    assert task["observed"]["pattern_count"] == 1
    assert task["observed"]["proposal_count"] == 1
    assert "below top-N cutoff" in task["observed"]["no_template_reasons"]


def test_governance_bench_error_boundary_covers_missing_input(governance_bench_summary):
    tasks = {task["id"]: task for task in governance_bench_summary["tasks"]}
    task = tasks["sentinel-missing-dir"]

    assert task["status"] == "pass"
    assert task["returncode"] == 2
    assert "does not exist" in task["stderr"].lower()
