"""Unit tests for spawn_worker_subprocess prompt transport."""

from __future__ import annotations

import importlib.machinery
import importlib.util
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "workflows" / "spawn_worker_subprocess.py"


def load_module():
    loader = importlib.machinery.SourceFileLoader("spawn_worker_subprocess", str(SCRIPT))
    spec = importlib.util.spec_from_loader("spawn_worker_subprocess", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


class SpawnWorkerSubprocessTests(unittest.TestCase):
    def test_prepare_worker_command_codex_uses_stdin_not_prompt_argv(self):
        module = load_module()
        argv, stdin_text = module.prepare_worker_command(
            {
                "driver": "codex",
                "cmd": "codex",
                "model": "gpt-5.5",
                "prompt": "ticket body here",
                "args": ["exec", "--model", "{model}", "{prompt}"],
            }
        )

        self.assertEqual(argv, ["codex", "exec", "--model", "gpt-5.5"])
        self.assertEqual(stdin_text, "ticket body here")

    def test_prepare_worker_command_gemini_uses_empty_prompt_flag_plus_stdin(self):
        module = load_module()
        argv, stdin_text = module.prepare_worker_command(
            {
                "driver": "gemini",
                "cmd": "gemini",
                "model": "gemini-2.5-flash",
                "prompt": "ticket body here",
                "args": ["-y", "-p", "{prompt}", "--model", "{model}"],
            }
        )

        self.assertEqual(argv, ["gemini", "-y", "-p", "", "--model", "gemini-2.5-flash"])
        self.assertEqual(stdin_text, "ticket body here")

    def test_prepare_worker_command_copilot_uses_stdin_not_prompt_argv(self):
        module = load_module()
        argv, stdin_text = module.prepare_worker_command(
            {
                "driver": "copilot",
                "cmd": "chitin-kernel",
                "model": "gpt-4.1",
                "prompt": "ticket body here",
                "args": ["drive", "copilot", "--model", "{model}", "--cwd", ".", "{prompt}"],
            }
        )

        self.assertEqual(argv, ["chitin-kernel", "drive", "copilot", "--model", "gpt-4.1", "--cwd", "."])
        self.assertEqual(stdin_text, "ticket body here")

    def test_prepare_worker_command_max_size_prompt_stays_off_argv(self):
        # Boundary: max. A very large ticket body must travel via stdin_text,
        # never argv — that is the whole point of the change (ps visibility +
        # ARG_MAX). Check every stdin-routed driver.
        module = load_module()
        huge_prompt = "x" * 500_000
        for driver, cmd in (("codex", "codex"), ("copilot", "chitin-kernel"), ("gemini", "gemini")):
            argv, stdin_text = module.prepare_worker_command(
                {
                    "driver": driver,
                    "cmd": cmd,
                    "model": "m",
                    "prompt": huge_prompt,
                    "args": ["--model", "{model}", "{prompt}"],
                }
            )
            self.assertEqual(stdin_text, huge_prompt, msg=driver)
            self.assertNotIn(huge_prompt, argv, msg=driver)

    def test_prepare_worker_command_missing_prompt_does_not_crash(self):
        # Boundary: error. A config with no "prompt" key must not raise — the
        # helper falls back to an empty prompt, still routed via stdin so the
        # {prompt} placeholder never becomes a literal argv entry.
        module = load_module()
        argv, stdin_text = module.prepare_worker_command(
            {
                "driver": "codex",
                "cmd": "codex",
                "model": "gpt-5.5",
                "args": ["exec", "--model", "{model}", "{prompt}"],
            }
        )
        self.assertEqual(argv, ["codex", "exec", "--model", "gpt-5.5"])
        self.assertEqual(stdin_text, "")


if __name__ == "__main__":
    unittest.main()
