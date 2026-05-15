#!/usr/bin/env python3
"""Repo-local governance bench harness.

Runs fixed sentinel-analysis tasks against bundled fixtures and emits a small
summary artifact under bench/out/. This is intentionally narrow: it exists to
catch governance regressions without depending on external bench repos.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parent.parent
BENCH_DIR = ROOT / "bench"
TASKS_DIR = BENCH_DIR / "tasks"
OUT_DIR = BENCH_DIR / "out"


@dataclass(frozen=True)
class TaskResult:
    id: str
    description: str
    status: str
    duration_ms: int
    returncode: int
    stdout: str
    stderr: str
    observed: dict[str, Any]
    failures: list[str]


def _now() -> datetime:
    return datetime.now(tz=timezone.utc)


def _load_task(path: Path) -> dict[str, Any]:
    raw = json.loads(path.read_text())
    if not isinstance(raw, dict):
        raise ValueError(f"{path}: task json must be an object")
    raw["_task_path"] = str(path)
    return raw


def _iter_tasks() -> list[dict[str, Any]]:
    tasks = [_load_task(path) for path in sorted(TASKS_DIR.glob("*/task.json"))]
    if not tasks:
        raise RuntimeError(f"no bench tasks found under {TASKS_DIR}")
    return tasks


def _summary_markdown(summary: dict[str, Any]) -> str:
    lines = [
        f"# Governance Bench — {summary['generated_at'][:10]}",
        "",
        f"Pass: {summary['pass_count']}  Fail: {summary['fail_count']}  Total: {summary['task_count']}",
        "",
        "## Tasks",
        "",
    ]
    for task in summary["tasks"]:
        lines.append(
            f"- `{task['id']}`: {task['status']} "
            f"({task['duration_ms']} ms, rc={task['returncode']})"
        )
        if task["failures"]:
            for failure in task["failures"]:
                lines.append(f"  - {failure}")
    lines.append("")
    return "\n".join(lines)


def _read_single_json(out_dir: Path) -> dict[str, Any]:
    json_files = sorted(out_dir.glob("*.json"))
    if len(json_files) != 1:
        raise AssertionError(f"expected exactly 1 json output, found {len(json_files)}")
    return json.loads(json_files[0].read_text())


def _assert_equals(name: str, actual: Any, expected: Any, failures: list[str]) -> None:
    if actual != expected:
        failures.append(f"{name}: got {actual!r}, want {expected!r}")


def _validate_success_output(body: dict[str, Any], expect: dict[str, Any], failures: list[str]) -> dict[str, Any]:
    observed = {
        "stream": body.get("stream"),
        "proposal_count": body.get("metadata", {}).get("promotion", {}).get("proposal_count"),
        "promotion_status": body.get("metadata", {}).get("promotion", {}).get("status"),
        "pattern_count": len(body.get("patterns", [])),
        "top_rule_id": body.get("patterns", [{}])[0].get("rule_id") if body.get("patterns") else None,
        "parse_errors": body.get("input_summary", {}).get("parse_errors"),
        "no_template_reasons": [item.get("reason_no_template") for item in body.get("no_template_patterns", [])],
    }

    if "stream" in expect:
        _assert_equals("stream", observed["stream"], expect["stream"], failures)
    if "proposal_count" in expect:
        _assert_equals("proposal_count", observed["proposal_count"], expect["proposal_count"], failures)
    if "promotion_status" in expect:
        _assert_equals("promotion_status", observed["promotion_status"], expect["promotion_status"], failures)
    if "pattern_count" in expect:
        _assert_equals("pattern_count", observed["pattern_count"], expect["pattern_count"], failures)
    if "top_rule_id" in expect:
        _assert_equals("top_rule_id", observed["top_rule_id"], expect["top_rule_id"], failures)
    if "parse_errors" in expect:
        _assert_equals("parse_errors", observed["parse_errors"], expect["parse_errors"], failures)
    if "no_template_reason" in expect and expect["no_template_reason"] not in observed["no_template_reasons"]:
        failures.append(
            "no_template_reason: "
            f"missing {expect['no_template_reason']!r} in {observed['no_template_reasons']!r}"
        )

    return observed


def _run_task(task: dict[str, Any]) -> TaskResult:
    task_path = Path(task["_task_path"])
    expect = task["expect"]
    task_dir = task_path.parent
    out_dir = OUT_DIR / task["id"]
    out_dir.mkdir(parents=True, exist_ok=True)
    for child in out_dir.iterdir():
        if child.is_file():
            child.unlink()

    fixture_dir = (task_dir / task["fixture_dir"]).resolve()
    cmd = [
        sys.executable,
        "-m",
        task["module"],
        "--window",
        task["window"],
        "--top-n",
        str(task["top_n"]),
        "--out-dir",
        str(out_dir),
        "--decisions-dir",
        str(fixture_dir),
        "--now",
        task["now"],
    ]
    env = os.environ.copy()
    env["PYTHONPATH"] = str(ROOT / "python")

    started = time.perf_counter()
    proc = subprocess.run(
        cmd,
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
    )
    duration_ms = int((time.perf_counter() - started) * 1000)

    failures: list[str] = []
    _assert_equals("returncode", proc.returncode, expect["returncode"], failures)
    if "stderr_contains" in expect and expect["stderr_contains"] not in proc.stderr:
        failures.append(
            f"stderr_contains: missing {expect['stderr_contains']!r} in stderr {proc.stderr!r}"
        )

    observed: dict[str, Any] = {}
    if proc.returncode == 0:
        try:
            observed = _validate_success_output(_read_single_json(out_dir), expect, failures)
        except Exception as exc:
            failures.append(f"output validation error: {exc}")

    status = "pass" if not failures else "fail"
    return TaskResult(
        id=task["id"],
        description=task["description"],
        status=status,
        duration_ms=duration_ms,
        returncode=proc.returncode,
        stdout=proc.stdout,
        stderr=proc.stderr,
        observed=observed,
        failures=failures,
    )


def main() -> int:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    tasks = _iter_tasks()
    results = [_run_task(task) for task in tasks]

    summary = {
        "schema_version": "1",
        "suite": "governance-bench",
        "generated_at": _now().isoformat(),
        "task_count": len(results),
        "pass_count": sum(1 for result in results if result.status == "pass"),
        "fail_count": sum(1 for result in results if result.status != "pass"),
        "tasks": [asdict(result) for result in results],
    }

    summary_json = OUT_DIR / "governance-bench-summary.json"
    summary_md = OUT_DIR / "governance-bench-summary.md"
    summary_json.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
    summary_md.write_text(_summary_markdown(summary))

    for result in results:
        print(f"{result.status.upper():4} {result.id} ({result.duration_ms} ms)")
        for failure in result.failures:
            print(f"  - {failure}")

    print(f"Wrote {summary_json}")
    print(f"Wrote {summary_md}")
    return 0 if summary["fail_count"] == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
