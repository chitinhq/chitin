"""Tests for swarm/chitin_bench — the Chitin Bench Harbor agent.

These tests do NOT require ollama / docker / harbor's containerized
environment. They cover the parser, loop detector, and the
classification helpers used by the bench-ticket emitter.

Per Knuth lens (active for boundary-correctness work): every test
states the invariant up front in the docstring.
"""

from __future__ import annotations

import asyncio
import json
import tempfile
from pathlib import Path
from tempfile import TemporaryDirectory
from types import SimpleNamespace
from unittest import TestCase, main

from harbor.environments.base import ExecResult

from swarm.chitin_bench.agent import (
    BASH_BLOCK_RE,
    BenchAgent,
    LoopDetector,
    STDERR_CAPTURE_LIMIT,
    STDOUT_CAPTURE_LIMIT,
    _strip_provider_prefix,
    is_task_complete,
    parse_bash_block,
    wrap_command_with_timeout,
)


class TestBashBlockParser(TestCase):
    """Invariant: parse_bash_block extracts the FIRST fenced block."""

    def test_extracts_simple_block(self):
        text = "Some thought\n```bash\nls -la\n```\nmore"
        self.assertEqual(parse_bash_block(text), "ls -la")

    def test_accepts_untagged_fence(self):
        text = "```\necho hi\n```"
        self.assertEqual(parse_bash_block(text), "echo hi")

    def test_accepts_sh_tag(self):
        text = "```sh\necho hi\n```"
        self.assertEqual(parse_bash_block(text), "echo hi")

    def test_returns_first_of_multiple_blocks(self):
        text = "```bash\nfirst\n```\n```bash\nsecond\n```"
        self.assertEqual(parse_bash_block(text), "first")

    def test_returns_none_on_no_block(self):
        self.assertIsNone(parse_bash_block("just text, no fence"))

    def test_returns_none_on_empty(self):
        self.assertIsNone(parse_bash_block(""))

    def test_multiline_command_preserved(self):
        text = "```bash\nls -la \\\n  /tmp\n```"
        self.assertEqual(parse_bash_block(text), "ls -la \\\n  /tmp")


class TestTaskCompleteSentinel(TestCase):
    """Invariant: TASK_COMPLETE is recognized on its OWN line."""

    def test_recognized_standalone(self):
        self.assertTrue(is_task_complete("TASK_COMPLETE"))

    def test_recognized_at_end(self):
        self.assertTrue(is_task_complete("```bash\nls\n```\nTASK_COMPLETE"))

    def test_recognized_with_trailing_whitespace(self):
        self.assertTrue(is_task_complete("TASK_COMPLETE  "))

    def test_NOT_recognized_inline(self):
        """Sentinel inside a sentence must NOT trigger termination —
        the agent might be discussing the sentinel without claiming
        the task is done."""
        self.assertFalse(is_task_complete("I will emit TASK_COMPLETE soon."))

    def test_NOT_recognized_partial(self):
        self.assertFalse(is_task_complete("TASK_COMPLETED"))


class TestLoopDetector(TestCase):
    """Invariant: trips on (a) 3 consecutive identical commands, OR
    (b) 3 consecutive non-zero with identical captured output.
    Resets on any new command."""

    def test_no_loop_under_three(self):
        ld = LoopDetector()
        ld.record("ls", ExecResult(stdout="a", return_code=0))
        ld.record("ls", ExecResult(stdout="a", return_code=0))
        self.assertFalse(ld.is_looping())

    def test_three_identical_commands_trips(self):
        ld = LoopDetector()
        for _ in range(3):
            ld.record("ls /nowhere", ExecResult(stderr="no such file", return_code=1))
        self.assertTrue(ld.is_looping())

    def test_three_different_failures_with_same_output_trips(self):
        """Same stderr, same return_code != 0, even different commands → loop.
        This catches the model spinning on a tool that always says
        'permission denied' regardless of the path argument."""
        ld = LoopDetector()
        for cmd in ("cat a", "cat b", "cat c"):
            ld.record(cmd, ExecResult(stderr="permission denied", return_code=1))
        self.assertTrue(ld.is_looping())

    def test_changing_output_does_not_trip(self):
        """If output is changing (model is exploring), no loop."""
        ld = LoopDetector()
        ld.record("ls /a", ExecResult(stdout="x", return_code=0))
        ld.record("ls /b", ExecResult(stdout="y", return_code=0))
        ld.record("ls /c", ExecResult(stdout="z", return_code=0))
        self.assertFalse(ld.is_looping())

    def test_success_in_window_does_not_trip(self):
        """If any rc==0 is in the 3-window, no loop (we got somewhere)."""
        ld = LoopDetector()
        ld.record("a", ExecResult(stderr="err", return_code=1))
        ld.record("b", ExecResult(stdout="ok", return_code=0))
        ld.record("c", ExecResult(stderr="err", return_code=1))
        self.assertFalse(ld.is_looping())


class TestObservationFormatting(TestCase):
    """Invariant: truncated observations are labeled so the model can
    narrow the next read instead of repeating the same command."""

    def test_truncated_stdout_keeps_tail_visible(self):
        trailer = "assert alert_detected, 'tail still visible'"
        observation = BenchAgent._format_observation(
            "cat /app/test_outputs.py",
            ExecResult(
                stdout=("x" * STDOUT_CAPTURE_LIMIT) + trailer,
                return_code=0,
            ),
        )
        self.assertIn(
            "[stdout truncated: omitted",
            observation,
        )
        self.assertIn("showing head+tail", observation)
        self.assertIn(trailer, observation)

    def test_truncated_stderr_keeps_tail_visible(self):
        trailer = "ValueError: tail still visible"
        observation = BenchAgent._format_observation(
            "python broken.py",
            ExecResult(
                stderr=("e" * STDERR_CAPTURE_LIMIT) + trailer,
                return_code=1,
            ),
        )
        self.assertIn(
            "[stderr truncated: omitted",
            observation,
        )
        self.assertIn("showing head+tail", observation)
        self.assertIn(trailer, observation)


class TestStripProviderPrefix(TestCase):
    """Invariant: ``ollama/<model>`` becomes ``<model>``; everything
    else is untouched. Idempotent."""

    def test_strips_ollama_prefix(self):
        self.assertEqual(
            _strip_provider_prefix("ollama/qwen3-coder:30b-32k"),
            "qwen3-coder:30b-32k",
        )

    def test_no_change_when_no_prefix(self):
        self.assertEqual(
            _strip_provider_prefix("qwen3-coder:30b-32k"),
            "qwen3-coder:30b-32k",
        )

    def test_idempotent(self):
        once = _strip_provider_prefix("ollama/qwen3-coder")
        self.assertEqual(_strip_provider_prefix(once), once)


class TestCommandTimeoutWrapper(TestCase):
    """Invariant: timed commands are wrapped in-container so a Python
    await timeout does not leak the child process into later turns."""

    def test_wraps_with_timeout_and_quoted_bash_command(self):
        wrapped = wrap_command_with_timeout("apt-get install -y opam", 60)
        self.assertEqual(
            wrapped,
            "timeout --kill-after=5s 60s bash -lc 'apt-get install -y opam'",
        )

    def test_agent_executes_wrapped_command(self):
        class FakeEnvironment:
            def __init__(self) -> None:
                self.commands: list[str] = []

            async def exec(self, cmd: str) -> ExecResult:
                self.commands.append(cmd)
                if cmd.startswith("timeout --kill-after=5s 60s bash -lc "):
                    return ExecResult(stdout="ok", stderr="", return_code=0)
                return ExecResult(stdout="", stderr="", return_code=0)

        env = FakeEnvironment()
        context = SimpleNamespace(metadata=None)
        responses = iter(["```bash\napt-get install -y opam\n```", "TASK_COMPLETE"])

        from swarm.chitin_bench import agent as agent_module

        original_ollama_chat = agent_module.ollama_chat
        try:
            agent_module.ollama_chat = lambda *args, **kwargs: next(responses)
            with TemporaryDirectory() as tmpdir:
                agent = BenchAgent(logs_dir=Path(tmpdir), model_name="ollama/test")
                asyncio.run(agent.run("install opam", env, context))
        finally:
            agent_module.ollama_chat = original_ollama_chat

        self.assertGreaterEqual(len(env.commands), 1)
        self.assertTrue(
            env.commands[-1].startswith("timeout --kill-after=5s 60s bash -lc "),
            env.commands[-1],
        )


# ── Bench-ticket-emitter classify_failure (no harbor agents needed) ──

class TestEmitterClassifier(TestCase):
    """Invariant: every trial result either passes (reward >= 1.0) or
    produces exactly one (is_failure=True, block_reason, evidence) tuple."""

    def _classify(self, trial: dict, trial_dir: Path | None = None):
        # Import lazily; the emitter module imports nothing heavy.
        import importlib.util
        from pathlib import Path
        emitter_path = Path(__file__).resolve().parents[1] / "bin" / "chitin-bench-ticket-emitter"
        loader_spec = importlib.util.spec_from_loader(
            "chitin_bench_emitter",
            importlib.machinery.SourceFileLoader(  # type: ignore[attr-defined]
                "chitin_bench_emitter", str(emitter_path),
            ),
        )
        mod = importlib.util.module_from_spec(loader_spec)
        loader_spec.loader.exec_module(mod)
        return mod.classify_failure(trial, trial_dir)

    def test_pass_when_reward_one(self):
        is_fail, reason, _ = self._classify(
            {"verifier_result": {"reward": 1.0}}
        )
        self.assertFalse(is_fail)
        self.assertEqual(reason, "passed")

    def test_pass_when_nested_reward_one(self):
        """Harbor writes {'rewards': {'reward': 1.0}} in current builds."""
        is_fail, reason, _ = self._classify(
            {"verifier_result": {"rewards": {"reward": 1.0}}}
        )
        self.assertFalse(is_fail)
        self.assertEqual(reason, "passed")

    def test_fail_when_reward_zero(self):
        is_fail, reason, _ = self._classify(
            {"verifier_result": {"reward": 0.0}}
        )
        self.assertTrue(is_fail)
        self.assertEqual(reason, "verifier_failed")

    def test_fail_when_nested_reward_zero(self):
        is_fail, reason, _ = self._classify(
            {"verifier_result": {"rewards": {"reward": 0.0}}}
        )
        self.assertTrue(is_fail)
        self.assertEqual(reason, "verifier_failed")

    def test_exception_classified_with_type(self):
        is_fail, reason, _ = self._classify(
            {"exception_info": {
                "exception_type": "DockerBuildError",
                "exception_message": "image pull failed",
            }}
        )
        self.assertTrue(is_fail)
        self.assertIn("DockerBuildError", reason)

    def test_chitin_bench_block_reason_classified(self):
        is_fail, reason, _ = self._classify({
            "verifier_result": {"reward": 0.0},
            "agent_result": {
                "metadata": {"chitin_bench_block_reason": "step_budget_exceeded"},
            },
        })
        self.assertTrue(is_fail)
        self.assertEqual(reason, "harness_loud_fail:step_budget_exceeded")

    def test_missing_verifier_classified(self):
        is_fail, reason, _ = self._classify({})
        self.assertTrue(is_fail)
        self.assertEqual(reason, "verifier_missing")

    def test_cleanup_after_success_is_classified_separately(self):
        """Invariant: a trial that tears down state before TASK_COMPLETE is
        classified as a false-success cleanup, not generic verifier failure."""
        with TemporaryDirectory() as tmp:
            trial_dir = Path(tmp)
            agent_dir = trial_dir / "agent"
            agent_dir.mkdir()
            trajectory = agent_dir / "chitin-bench-trajectory.jsonl"
            trajectory.write_text(
                "\n".join(
                    [
                        json.dumps(
                            {
                                "step": 11,
                                "role": "exec",
                                "content": "$ curl -s http://localhost:8080/hello.html\n[return_code=0]\nSTDOUT:\nhello world",
                                "metadata": {"cmd": "curl -s http://localhost:8080/hello.html", "return_code": 0},
                            }
                        ),
                        json.dumps(
                            {
                                "step": 12,
                                "role": "exec",
                                "content": "$ cd /git/server && git branch -D master && rm -rf /var/www/html/*\n[return_code=0]",
                                "metadata": {
                                    "cmd": "cd /git/server && git branch -D master && rm -rf /var/www/html/*",
                                    "return_code": 0,
                                },
                            }
                        ),
                        json.dumps(
                            {
                                "step": 15,
                                "role": "assistant",
                                "content": "```bash\necho \"Setup complete\"\n```\n\nTASK_COMPLETE",
                                "metadata": {},
                            }
                        ),
                    ]
                )
                + "\n"
            )

            is_fail, reason, evidence = self._classify(
                {
                    "trial_uri": trial_dir.as_uri(),
                    "verifier_result": {"rewards": {"reward": 0.0}},
                }
            )

        self.assertTrue(is_fail)
        self.assertEqual(reason, "false_success_cleanup")
        self.assertIn("git branch -D master", evidence)

    def test_verifier_bootstrap_failure_classified_from_stdout(self):
        """Verifier bootstrap breakage in sibling logs must not collapse
        into a generic verifier_failed ticket."""
        with tempfile.TemporaryDirectory() as tmpdir:
            verifier_dir = Path(tmpdir) / "verifier"
            verifier_dir.mkdir(parents=True, exist_ok=True)
            (verifier_dir / "test-stdout.txt").write_text(
                "E: The repository 'http://cran.rstudio.com/bin/linux/ubuntu focal/ Release' "
                "does not have a Release file.\n"
                "/tests/test.sh: line 8: curl: command not found\n"
                "/tests/test.sh: line 19: uvx: command not found\n"
            )
            is_fail, reason, evidence = self._classify(
                {"verifier_result": {"rewards": {"reward": 0.0}}},
                Path(tmpdir),
            )
        self.assertTrue(is_fail)
        self.assertEqual(reason, "environment_setup_failed")
        self.assertIn("curl: command not found", evidence)


class TestEmitterTicketBody(TestCase):
    """Invariant: emitted bench-failure tickets point triage at the
    current Chitin Bench surface, not stale Icarus-era paths."""

    @staticmethod
    def _load_emitter_module():
        import importlib.util
        from pathlib import Path

        emitter_path = Path(__file__).resolve().parents[1] / "bin" / "chitin-bench-ticket-emitter"
        loader_spec = importlib.util.spec_from_loader(
            "chitin_bench_emitter_for_body",
            importlib.machinery.SourceFileLoader(  # type: ignore[attr-defined]
                "chitin_bench_emitter_for_body", str(emitter_path),
            ),
        )
        mod = importlib.util.module_from_spec(loader_spec)
        loader_spec.loader.exec_module(mod)
        return mod

    def test_ticket_body_references_current_triage_paths_and_heuristic(self):
        mod = self._load_emitter_module()
        body = mod.ticket_body(
            task_name="count-dataset-tokens",
            block_reason="verifier_failed",
            evidence='{"rewards":{"reward":0.0}}',
            trial_paths=["/tmp/trial/result.json"],
            agent_info={
                "name": "chitin-bench",
                "version": "1.0.0",
                "model_info": {"name": "qwen3.6:27b"},
            },
        )

        self.assertIn("swarm.chitin_bench.agent:BenchAgent", body)
        self.assertIn("swarm/tests/test_chitin_bench.py", body)
        self.assertIn("treat it as a model-capability", body)


if __name__ == "__main__":
    main(verbosity=2)
