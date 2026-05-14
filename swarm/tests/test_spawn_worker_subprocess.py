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


if __name__ == "__main__":
    unittest.main()
