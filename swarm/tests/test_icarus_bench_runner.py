"""Tests for chitin-bench-runner — emit_gov_decision and _extract_tick_metadata.

Covers the gov-decision row emission that this ticket (t_bb2a1575)
introduced. These tests do NOT require harbor/ollama/docker; they
exercise the pure-Python logic in the runner that writes rows to
the Chitin governance chain.

Per Knuth lens: every test states the invariant up front in the
docstring.
"""

from __future__ import annotations

import json
import os
import sys
import tempfile
from pathlib import Path
from unittest import TestCase

# Add swarm/bin to sys.path so we can import the runner as a module.
RUNNER_DIR = Path(__file__).resolve().parents[1] / "bin"
sys.path.insert(0, str(RUNNER_DIR))

# The runner script uses a hyphen in its filename so a standard import
# won't work. We load it via exec() against a fresh namespace dict.
# The script references __file__ at module level (REPO_ROOT), so we
# inject it.
_runner_path = RUNNER_DIR / "chitin-bench-runner"
_runner: dict = {"__file__": str(_runner_path), "__name__": "icarus_bench_runner"}
exec(_runner_path.read_text(), _runner)


class TestEmitGovDecision(TestCase):
    """Invariant: emit_gov_decision appends exactly one JSON line per
    call to the daily gov-decisions JSONL, containing all required
    fields (action_type, block_reason, model) plus optional fields
    (reward, steps_used) when provided."""

    def setUp(self) -> None:
        self.tmpdir = tempfile.mkdtemp()
        # Override CHITIN_DIR so tests don't pollute ~/.chitin.
        self._original_chitin_dir = _runner["CHITIN_DIR"]
        _runner["CHITIN_DIR"] = Path(self.tmpdir)

    def tearDown(self) -> None:
        _runner["CHITIN_DIR"] = self._original_chitin_dir
        # Cleanup
        for f in Path(self.tmpdir).rglob("*.jsonl"):
            f.unlink()

    def test_emits_one_line_with_required_fields(self):
        """Invariant: a single call appends exactly one JSON line with
        action_type=icarus_bench_tick and all four metadata columns."""
        _runner["emit_gov_decision"](
            task_name="sqlite-db-truncate",
            block_reason="none",
            reward=1.0,
            steps_used=5,
            model="ollama/qwen3-coder:30b-32k",
            status="ran",
        )
        # Find the JSONL file
        jsonl_files = list(Path(self.tmpdir).glob("gov-decisions-*.jsonl"))
        self.assertEqual(len(jsonl_files), 1, "expected exactly one JSONL file")

        lines = jsonl_files[0].read_text().strip().splitlines()
        self.assertEqual(len(lines), 1, "expected exactly one line")

        row = json.loads(lines[0])
        self.assertEqual(row["action_type"], "icarus_bench_tick")
        self.assertEqual(row["action_target"], "sqlite-db-truncate")
        self.assertEqual(row["block_reason"], "none")
        self.assertEqual(row["model"], "ollama/qwen3-coder:30b-32k")
        self.assertEqual(row["reward"], 1.0)
        self.assertEqual(row["steps_used"], 5)
        self.assertEqual(row["tick_status"], "ran")
        self.assertIn("ts", row)
        self.assertIn("allowed", row)

    def test_emits_with_block_reason(self):
        """Invariant: block_reason from the tick is forwarded as-is."""
        _runner["emit_gov_decision"](
            task_name="flaky-task",
            block_reason="harness_exception:TimeoutError",
            reward=None,
            steps_used=None,
            model="ollama/qwen3.6:27b",
            status="ran",
        )
        jsonl_files = list(Path(self.tmpdir).glob("gov-decisions-*.jsonl"))
        row = json.loads(jsonl_files[0].read_text().strip())
        self.assertEqual(row["block_reason"], "harness_exception:TimeoutError")
        self.assertEqual(row["reason"], "harness_exception:TimeoutError")

    def test_emits_without_optional_fields_when_none(self):
        """Invariant: reward and steps_used are omitted from the JSON line
        when not provided (None), not written as null."""
        _runner["emit_gov_decision"](
            task_name="infra-fail-task",
            block_reason="infra_fail",
            reward=None,
            steps_used=None,
            model="ollama/test",
            status="infra_fail",
        )
        jsonl_files = list(Path(self.tmpdir).glob("gov-decisions-*.jsonl"))
        row = json.loads(jsonl_files[0].read_text().strip())
        self.assertNotIn("reward", row)
        self.assertNotIn("steps_used", row)

    def test_empty_block_reason_becomes_none_string(self):
        """Invariant: empty block_reason is normalized to 'none'."""
        _runner["emit_gov_decision"](
            task_name="test-task",
            block_reason="",
            reward=0.5,
            steps_used=3,
            model="test",
            status="ran",
        )
        jsonl_files = list(Path(self.tmpdir).glob("gov-decisions-*.jsonl"))
        row = json.loads(jsonl_files[0].read_text().strip())
        self.assertEqual(row["block_reason"], "none")
        self.assertEqual(row["reason"], "none")

    def test_multiple_calls_append_to_same_file(self):
        """Invariant: two calls on the same day append two lines to the
        same daily JSONL file."""
        for i in range(2):
            _runner["emit_gov_decision"](
                task_name=f"task-{i}",
                block_reason="none",
                reward=float(i),
                steps_used=i + 1,
                model="test",
                status="ran",
            )
        jsonl_files = list(Path(self.tmpdir).glob("gov-decisions-*.jsonl"))
        self.assertEqual(len(jsonl_files), 1)
        lines = jsonl_files[0].read_text().strip().splitlines()
        self.assertEqual(len(lines), 2)

    def test_fsync_on_write(self):
        """Invariant: the written line is flushed and fsynced so it
        survives a crash. We verify by reading it back immediately."""
        _runner["emit_gov_decision"](
            task_name="sync-test",
            block_reason="none",
            reward=1.0,
            steps_used=1,
            model="test",
            status="ran",
        )
        jsonl_files = list(Path(self.tmpdir).glob("gov-decisions-*.jsonl"))
        # If fsync worked, the line is durable. We just verify it's readable.
        row = json.loads(jsonl_files[0].read_text().strip())
        self.assertEqual(row["action_target"], "sync-test")


class TestExtractTickMetadata(TestCase):
    """Invariant: _extract_tick_metadata returns block_reason, reward,
    steps_used from the harbor trial result.json, falling back
    gracefully when the file is missing or malformed."""

    def setUp(self) -> None:
        self.tmpdir = tempfile.mkdtemp()
        self._original_jobs_dir = _runner["JOBS_DIR"]
        _runner["JOBS_DIR"] = Path(self.tmpdir) / "jobs" / "icarus"

    def tearDown(self) -> None:
        _runner["JOBS_DIR"] = self._original_jobs_dir

    def _write_trial_result(self, job_name: str, trial_name: str, data: dict) -> None:
        trial_dir = _runner["JOBS_DIR"] / job_name / trial_name
        trial_dir.mkdir(parents=True, exist_ok=True)
        (trial_dir / "result.json").write_text(json.dumps(data))

    def test_extracts_reward_and_block_reason(self):
        """Invariant: icarus_block_reason and reward are extracted from
        the harbor result.json."""
        self._write_trial_result("icarus-test", "trial-0", {
            "agent_result": {
                "metadata": {"icarus_block_reason": "stuck_in_loop"},
                "steps": 12,
            },
            "verifier_result": {"reward": 0.75},
        })
        result = {"status": "ran", "job_name": "icarus-test"}
        meta = _runner["_extract_tick_metadata"](result, "icarus-test")
        self.assertEqual(meta["block_reason"], "stuck_in_loop")
        self.assertEqual(meta["reward"], 0.75)
        self.assertEqual(meta["steps_used"], 12)

    def test_extracts_steps_used_from_icarus_metadata(self):
        """Invariant: the current Icarus artifact shape stores steps in
        agent_result.metadata.icarus_steps_used, and the runner must
        preserve that in the gov-decision row metadata."""
        self._write_trial_result("icarus-meta", "trial-0", {
            "agent_result": {
                "metadata": {
                    "icarus_block_reason": "ollama_error",
                    "icarus_steps_used": 8,
                },
            },
            "verifier_result": {"rewards": {"reward": 0.0}},
        })
        result = {"status": "ran", "job_name": "icarus-meta"}
        meta = _runner["_extract_tick_metadata"](result, "icarus-meta")
        self.assertEqual(meta["block_reason"], "ollama_error")
        self.assertEqual(meta["reward"], 0.0)
        self.assertEqual(meta["steps_used"], 8)

    def test_extracts_reward_from_rewards_dict(self):
        """Invariant: reward is extracted from verifier_result.rewards.reward
        when that key path exists (current harbor format)."""
        self._write_trial_result("icarus-test2", "trial-0", {
            "verifier_result": {"rewards": {"reward": 1.0}},
            "agent_result": {},
        })
        result = {"status": "ran", "job_name": "icarus-test2"}
        meta = _runner["_extract_tick_metadata"](result, "icarus-test2")
        self.assertEqual(meta["reward"], 1.0)

    def test_defaults_block_reason_to_none_when_no_result(self):
        """Invariant: when no result.json exists, block_reason defaults
        to 'none' unless the result status indicates a known failure."""
        result = {"status": "ran", "job_name": "nonexistent-job"}
        meta = _runner["_extract_tick_metadata"](result, "nonexistent-job")
        self.assertEqual(meta["block_reason"], "none")
        self.assertIsNone(meta["reward"])
        self.assertIsNone(meta["steps_used"])

    def test_timeout_status_sets_block_reason(self):
        """Invariant: a timeout result sets block_reason='timeout'."""
        result = {"status": "timeout", "elapsed_s": 1800, "job_name": "icarus-timeout"}
        meta = _runner["_extract_tick_metadata"](result, "icarus-timeout")
        self.assertEqual(meta["block_reason"], "timeout")

    def test_infra_fail_status_sets_block_reason(self):
        """Invariant: an infra_fail result sets block_reason to the
        reason from the run_one_task result dict."""
        result = {"status": "infra_fail", "reason": "harbor CLI not on PATH", "job_name": ""}
        meta = _runner["_extract_tick_metadata"](result, "")
        self.assertEqual(meta["block_reason"], "harbor CLI not on PATH")

    def test_exception_info_sets_block_reason(self):
        """Invariant: exception_info in result.json yields
        harness_exception:<type>."""
        self._write_trial_result("icarus-exc", "trial-0", {
            "exception_info": {"exception_type": "RuntimeError"},
            "agent_result": {},
        })
        result = {"status": "ran", "job_name": "icarus-exc"}
        meta = _runner["_extract_tick_metadata"](result, "icarus-exc")
        self.assertEqual(meta["block_reason"], "harness_exception:RuntimeError")

    def test_environment_start_timeout_is_not_marked_harness_exception(self):
        """Invariant: Harbor environment startup failures are classified
        as environment_setup_failed, not agent/harness exceptions."""
        self._write_trial_result("icarus-env-timeout", "trial-0", {
            "exception_info": {
                "exception_type": "EnvironmentStartTimeoutError",
                "exception_message": "Environment start timed out after 600.0 seconds",
            },
            "agent_result": {},
        })
        result = {"status": "ran", "job_name": "icarus-env-timeout"}
        meta = _runner["_extract_tick_metadata"](result, "icarus-env-timeout")
        self.assertEqual(
            meta["block_reason"],
            "environment_setup_failed:EnvironmentStartTimeoutError",
        )

    def test_malformed_result_json_falls_back(self):
        """Invariant: a malformed result.json is tolerated; metadata
        defaults are returned."""
        trial_dir = _runner["JOBS_DIR"] / "icarus-bad" / "trial-0"
        trial_dir.mkdir(parents=True, exist_ok=True)
        (trial_dir / "result.json").write_text("NOT JSON {{{")
        result = {"status": "ran", "job_name": "icarus-bad"}
        meta = _runner["_extract_tick_metadata"](result, "icarus-bad")
        self.assertEqual(meta["block_reason"], "none")


class TestBenchModelDefaults(TestCase):
    """Invariant: the runner, loop, and cron installer stay aligned on the
    coder-tuned default model so bench tasks do not silently route back to
    qwen3.6:27b."""

    def test_runner_defaults_to_qwen3_coder(self):
        self.assertEqual(
            _runner["DEFAULT_MODEL"],
            "ollama/qwen3-coder:30b-32k",
        )

    def test_shell_wrappers_default_to_qwen3_coder(self):
        loop_script = (RUNNER_DIR / "chitin-bench-loop").read_text()
        install_script = (RUNNER_DIR / "install-chitin-bench-cron.sh").read_text()

        self.assertIn(
            'CHITIN_BENCH_MODEL="${CHITIN_BENCH_MODEL:-ollama/qwen3-coder:30b-32k}"',
            loop_script,
        )
        self.assertIn(
            'MODEL="${CHITIN_BENCH_MODEL:-ollama/qwen3-coder:30b-32k}"',
            install_script,
        )


if __name__ == "__main__":
    from unittest import main as unittest_main
    unittest_main()
