#!/usr/bin/env python3
import json
import os
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "_pick_driver.py"


class PickDriverTest(unittest.TestCase):
    def run_picker(self, classify, cards, *, llm_json=None, extra_env=None):
        with tempfile.TemporaryDirectory() as tmp:
            cards_dir = Path(tmp) / "cards"
            cards_dir.mkdir()
            for name, payload in cards.items():
                (cards_dir / name).write_text(payload, encoding="utf-8")

            bin_dir = Path(tmp) / "bin"
            bin_dir.mkdir()
            fake_clawta = bin_dir / "clawta"
            fake_clawta.write_text(
                "#!/usr/bin/env bash\n"
                f"printf '%s\\n' {json.dumps(json.dumps(llm_json or {}))}\n",
                encoding="utf-8",
            )
            fake_clawta.chmod(0o755)

            env = os.environ.copy()
            env["OPENCLAW_AGENT_CARDS_DIR"] = str(cards_dir)
            env["PATH"] = f"{bin_dir}:{env['PATH']}"
            if extra_env:
                env.update(extra_env)

            return subprocess.run(
                ["python3", str(SCRIPT)],
                input=json.dumps(classify),
                text=True,
                capture_output=True,
                check=True,
                env=env,
            )

    def test_llm_choice_missing_required_capability_falls_back(self):
        classify = {"complexity": "med", "capabilities": ["go", "debug"]}
        cards = {
            "codex.json": json.dumps(
                {
                    "id": "codex",
                    "capabilities": [{"skill": "go"}, {"skill": "debug"}],
                    "models": [{"id": "gpt-5.5", "premium_cost": 2.0}],
                }
            ),
            "copilot.json": json.dumps(
                {
                    "id": "copilot",
                    "capabilities": [{"skill": "debug"}],
                    "models": [{"id": "gpt-4.1", "premium_cost": 0.5}],
                }
            ),
        }

        result = self.run_picker(
            classify,
            cards,
            llm_json={
                "driver": "copilot",
                "model": "gpt-4.1",
                "reasoning": "cheap",
            },
        )
        payload = json.loads(result.stdout)

        self.assertEqual(payload["driver"], "codex")
        self.assertEqual(payload["model"], "gpt-5.5")
        self.assertEqual(payload["router_mode"], "deterministic")
        self.assertIn("LLM fallback", payload["reasoning"])
        self.assertIn("lacks required capabilities", payload["reasoning"])

    def test_llm_choice_invalid_model_falls_back(self):
        classify = {"complexity": "high", "capabilities": ["python"]}
        cards = {
            "codex.json": json.dumps(
                {
                    "id": "codex",
                    "capabilities": [{"skill": "python"}],
                    "models": [{"id": "gpt-5.5", "premium_cost": 2.0}],
                }
            ),
        }

        result = self.run_picker(
            classify,
            cards,
            llm_json={
                "driver": "codex",
                "model": "not-on-card",
                "reasoning": "strong",
            },
        )
        payload = json.loads(result.stdout)

        self.assertEqual(payload["driver"], "codex")
        self.assertEqual(payload["model"], "gpt-5.5")
        self.assertEqual(payload["router_mode"], "deterministic")
        self.assertIn("invalid model", payload["reasoning"])

    def test_deterministic_model_selection_uses_complexity(self):
        cards = {
            "claude-code.json": json.dumps(
                {
                    "id": "claude-code",
                    "capabilities": [{"skill": "python"}],
                    "models": [
                        {"id": "claude-haiku-4-5", "premium_cost": 0.15},
                        {"id": "claude-sonnet-4-6", "premium_cost": 0.5},
                        {"id": "claude-opus-4-7", "premium_cost": 1.0},
                    ],
                }
            )
        }

        low = self.run_picker(
            {"complexity": "low", "capabilities": ["python"]},
            cards,
            llm_json={"driver": "missing", "model": "x", "reasoning": "bad"},
        )
        med = self.run_picker(
            {"complexity": "med", "capabilities": ["python"]},
            cards,
            llm_json={"driver": "missing", "model": "x", "reasoning": "bad"},
        )
        high = self.run_picker(
            {"complexity": "high", "capabilities": ["python"]},
            cards,
            llm_json={"driver": "missing", "model": "x", "reasoning": "bad"},
        )

        self.assertEqual(json.loads(low.stdout)["model"], "claude-haiku-4-5")
        self.assertEqual(json.loads(med.stdout)["model"], "claude-sonnet-4-6")
        self.assertEqual(json.loads(high.stdout)["model"], "claude-opus-4-7")

    def test_llm_model_choice_has_complexity_floor(self):
        classify = {"complexity": "med", "capabilities": ["docs"]}
        cards = {
            "copilot.json": json.dumps(
                {
                    "id": "copilot",
                    "capabilities": [{"skill": "docs"}],
                    "models": [
                        {"id": "gpt-4.1", "premium_cost": 0.0},
                        {"id": "claude-haiku-4.5", "premium_cost": 0.33},
                        {"id": "gpt-5.4", "premium_cost": 1.0},
                    ],
                }
            )
        }

        result = self.run_picker(
            classify,
            cards,
            llm_json={
                "driver": "copilot",
                "model": "gpt-4.1",
                "reasoning": "cheap docs",
            },
        )
        payload = json.loads(result.stdout)

        self.assertEqual(payload["driver"], "copilot")
        self.assertEqual(payload["router_mode"], "llm")
        self.assertEqual(payload["model"], "claude-haiku-4.5")

    def test_corrupt_cards_are_reported(self):
        classify = {"complexity": "low", "capabilities": ["ts"]}
        cards = {
            "codex.json": json.dumps(
                {
                    "id": "codex",
                    "capabilities": [{"skill": "ts"}],
                    "models": [{"id": "gpt-5.5", "premium_cost": 2.0}],
                }
            ),
            "broken.json": textwrap.dedent(
                """
                {"id": "broken",
                """
            ).strip(),
        }

        result = self.run_picker(
            classify,
            cards,
            llm_json={"driver": "unknown", "model": "x", "reasoning": "bad"},
        )
        payload = json.loads(result.stdout)

        self.assertEqual(payload["driver"], "codex")
        self.assertEqual(payload["card_load_errors"], 1)
        self.assertIn("card load error", payload["reasoning"])
        self.assertIn("failed to load agent card", result.stderr)


if __name__ == "__main__":
    unittest.main()
