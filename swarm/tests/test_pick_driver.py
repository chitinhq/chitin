#!/usr/bin/env python3
"""Behavior tests for swarm/workflows/_pick_driver.py."""

from __future__ import annotations

import hashlib
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "workflows" / "_pick_driver.py"


CARDS = {
    "claude-code-headless": {
        "id": "claude-code-headless",
        "capabilities": [{"skill": "review", "depth": "strong"}],
        "models": [{"id": "sonnet-4-6", "premium_cost": 0.03}],
    },
    "copilot": {
        "id": "copilot",
        "capabilities": [
            {"skill": "python", "depth": "moderate"},
            {"skill": "review", "depth": "moderate"},
        ],
        "models": [{"id": "gpt-4.1", "premium_cost": 0.01}],
    },
    "gemini": {
        "id": "gemini",
        "capabilities": [
            {"skill": "python", "depth": "strong"},
            {"skill": "review", "depth": "strong"},
        ],
        "models": [
            {"id": "gemini-2.5-flash-lite", "premium_cost": 0.05},
            {"id": "gemini-2.5-flash", "premium_cost": 0.10},
            {"id": "gemini-2.5-pro", "premium_cost": 0.50},
        ],
    },
    "codex": {
        "id": "codex",
        "capabilities": [
            {"skill": "python", "depth": "strong"},
            {"skill": "review", "depth": "strong"},
        ],
        "models": [{"id": "gpt-5.5", "premium_cost": 0.40}],
    },
}


class PickDriverTests(unittest.TestCase):
    def run_pick_driver(self, classify: dict, **env_overrides: str) -> dict:
        with tempfile.TemporaryDirectory() as tmp:
            cards_dir = Path(tmp)
            souls_dir = cards_dir / "souls"
            souls_dir.mkdir()
            for card_id, payload in CARDS.items():
                (cards_dir / f"{card_id}.json").write_text(json.dumps(payload), encoding="utf-8")
            for soul_id in ("knuth", "davinci", "sun-tzu", "socrates"):
                (souls_dir / f"{soul_id}.md").write_text(f"{soul_id} soul\n", encoding="utf-8")

            env = {
                **os.environ,
                "OPENCLAW_AGENT_CARDS_DIR": str(cards_dir),
                "CHITIN_SOULS_DIR": str(souls_dir),
                "ROUTER_MODE": "deterministic",
                **env_overrides,
            }
            result = subprocess.run(
                ["python3", str(SCRIPT)],
                input=json.dumps(classify),
                capture_output=True,
                text=True,
                check=False,
                env=env,
            )
            self.assertEqual(result.returncode, 0, msg=result.stderr)
            return json.loads(result.stdout)

    def run_pick_driver_raw(self, raw_input: str, **env_overrides: str) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as tmp:
            cards_dir = Path(tmp)
            souls_dir = cards_dir / "souls"
            souls_dir.mkdir()
            for card_id, payload in CARDS.items():
                (cards_dir / f"{card_id}.json").write_text(json.dumps(payload), encoding="utf-8")
            for soul_id in ("knuth", "davinci", "sun-tzu", "socrates"):
                (souls_dir / f"{soul_id}.md").write_text(f"{soul_id} soul\n", encoding="utf-8")

            env = {
                **os.environ,
                "OPENCLAW_AGENT_CARDS_DIR": str(cards_dir),
                "CHITIN_SOULS_DIR": str(souls_dir),
                "ROUTER_MODE": "deterministic",
                **env_overrides,
            }
            return subprocess.run(
                ["python3", str(SCRIPT)],
                input=raw_input,
                capture_output=True,
                text=True,
                check=False,
                env=env,
            )

    def test_exploration_can_be_disabled(self):
        result = self.run_pick_driver(
            {"complexity": "low", "capabilities": ["python"]},
            CLAWTA_EXPLORATION_PERCENT="0",
        )

        self.assertEqual(result["driver"], "copilot")
        self.assertEqual(result["selection_mode"], "exploitation")
        self.assertEqual(result["exploration_candidates_considered"], 0)

    def test_deterministic_router_mode_emits_valid_pick_without_llm(self):
        result = self.run_pick_driver(
            {"complexity": "low", "capabilities": ["python"]},
            ROUTER_MODE="deterministic",
            CLAWTA_EXPLORATION_PERCENT="0",
        )

        self.assertEqual(result["driver"], "copilot")
        self.assertEqual(result["router_mode"], "deterministic")
        self.assertEqual(result["selection_mode"], "exploitation")

    def test_force_driver_bypasses_routing_and_emits_requested_lane(self):
        result = self.run_pick_driver(
            {"complexity": "high", "capabilities": ["python", "review"]},
            FORCE_DRIVER="codex",
        )

        self.assertEqual(result["driver"], "codex")
        self.assertEqual(result["router_mode"], "forced")
        self.assertIn("FORCE_DRIVER env var bypassed routing logic", result["reasoning"])

    def test_exploration_pool_is_bounded_and_can_promote_gemini(self):
        result = self.run_pick_driver(
            {"complexity": "low", "capabilities": ["python"]},
            CLAWTA_EXPLORATION_PERCENT="100",
            CLAWTA_EXPLORATION_MAX_CANDIDATES="1",
        )

        self.assertEqual(result["driver"], "gemini")
        self.assertEqual(result["model"], "gemini-2.5-flash")
        self.assertEqual(result["selection_mode"], "exploration")
        self.assertEqual(result["exploration_candidates_considered"], 1)

    def test_exploration_never_violates_required_capabilities(self):
        result = self.run_pick_driver(
            {"complexity": "low", "capabilities": ["python", "review"]},
            CLAWTA_EXPLORATION_PERCENT="100",
            CLAWTA_EXPLORATION_MAX_CANDIDATES="2",
        )

        self.assertNotEqual(result["driver"], "claude-code-headless")
        self.assertIn(result["driver"], {"copilot", "gemini", "codex"})
        self.assertEqual(result["selection_mode"], "exploration")

    def test_high_risk_tasks_stay_on_exploitation_without_override(self):
        result = self.run_pick_driver(
            {
                "complexity": "low",
                "capabilities": ["python"],
                "risk_level": "high",
            },
            CLAWTA_EXPLORATION_PERCENT="100",
            CLAWTA_EXPLORATION_MAX_CANDIDATES="1",
        )

        self.assertEqual(result["driver"], "copilot")
        self.assertEqual(result["selection_mode"], "exploitation")
        self.assertIn("risk_level=high excluded from exploration", result["reasoning"])

    def test_governance_critical_tasks_require_explicit_opt_in(self):
        result = self.run_pick_driver(
            {
                "complexity": "low",
                "capabilities": ["python"],
                "governance_critical": True,
            },
            CLAWTA_EXPLORATION_PERCENT="100",
            CLAWTA_EXPLORATION_MAX_CANDIDATES="1",
        )
        self.assertEqual(result["selection_mode"], "exploitation")

        allowed = self.run_pick_driver(
            {
                "complexity": "low",
                "capabilities": ["python"],
                "governance_critical": True,
            },
            CLAWTA_EXPLORATION_PERCENT="100",
            CLAWTA_EXPLORATION_MAX_CANDIDATES="1",
            CLAWTA_EXPLORATION_ALLOW_GOVERNANCE_CRITICAL="1",
        )
        self.assertEqual(allowed["driver"], "gemini")
        self.assertEqual(allowed["selection_mode"], "exploration")

    def test_rejects_prefixed_stdout_noise_before_json(self):
        result = self.run_pick_driver_raw(
            '_pick_driver compatibility noise\n{"complexity":"low","capabilities":["python"]}',
            CLAWTA_EXPLORATION_PERCENT="0",
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Expecting value", result.stderr)

    def test_empty_stdin_is_rejected(self):
        # Boundary: empty input. Strict-JSON routing must fail fast, not
        # parse an empty string into a half-formed classify dict.
        for raw in ("", "   \n\t  "):
            result = self.run_pick_driver_raw(raw, CLAWTA_EXPLORATION_PERCENT="0")
            self.assertNotEqual(result.returncode, 0, msg=f"raw={raw!r}")
            self.assertIn("classify produced no JSON object", result.stderr)

    def test_max_size_classify_payload_still_routes(self):
        # Boundary: max. A large-but-valid classify payload (many capability
        # tokens + a long description) must still parse and route cleanly —
        # strict JSON parsing has no length ceiling of its own.
        classify = {
            "complexity": "low",
            "capabilities": ["python"],
            "estimated_loc": 10**9,
            "notes": "x" * 200_000,
            "extra_tokens": [f"tok-{i}" for i in range(5_000)],
        }
        result = self.run_pick_driver(classify, CLAWTA_EXPLORATION_PERCENT="0")
        self.assertEqual(result["driver"], "copilot")
        self.assertEqual(result["selection_mode"], "exploitation")

    def test_missing_soul_file_boundary_routes_with_unstamped_soul(self):
        # Boundary: error. If the selected soul file genuinely cannot be
        # located (no CHITIN_SOULS_DIR, no souls/ under the repo root —
        # exactly how the installed ~/.openclaw workflow runs), routing must
        # still succeed and emit JSON. The soul_id is reported but the
        # soul_hash is left empty rather than crashing dispatch.
        with tempfile.TemporaryDirectory() as tmp:
            missing_souls_dir = Path(tmp) / "missing-souls"
            souls_less_repo = Path(tmp) / "souls-less-repo"
            souls_less_repo.mkdir()
            result = self.run_pick_driver_raw(
                json.dumps(
                    {
                        "complexity": "low",
                        "capabilities": ["python"],
                        "ticket_title": "Run ledger invariant regression",
                    }
                ),
                CLAWTA_EXPLORATION_PERCENT="0",
                CHITIN_SOULS_DIR=str(missing_souls_dir),
                CHITIN_REPO=str(souls_less_repo),
            )

        self.assertEqual(result.returncode, 0, msg=result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(payload["soul_id"], "knuth")
        self.assertEqual(payload["soul_hash"], "")
        self.assertEqual(payload["soul_category"], "correctness")

    def test_correctness_ticket_selects_knuth_soul_and_composite_fingerprint(self):
        result = self.run_pick_driver(
            {
                "complexity": "low",
                "capabilities": ["python"],
                "ticket_title": "Run ledger invariant regression",
                "ticket_body": "Update the gate and run ledger schema with tests.",
                "governance_critical": True,
            },
            CLAWTA_EXPLORATION_PERCENT="0",
        )

        expected_hash = hashlib.sha256("knuth soul\n".encode("utf-8")).hexdigest()
        expected_fingerprint = hashlib.sha256(
            f"{result['driver']}{result['model']}knuth{expected_hash}".encode("utf-8")
        ).hexdigest()

        self.assertEqual(result["soul_id"], "knuth")
        self.assertEqual(result["soul_hash"], expected_hash)
        self.assertEqual(result["soul_category"], "correctness")
        self.assertEqual(result["agent_fingerprint"], expected_fingerprint)

    def test_board_config_soul_map_override_is_honored(self):
        result = self.run_pick_driver(
            {
                "complexity": "low",
                "capabilities": ["python"],
                "ticket_title": "Architecture cleanup",
                "ticket_body": "Refactor the dispatch boundary.",
            },
            CLAWTA_EXPLORATION_PERCENT="0",
            KANBAN_BOARD_SOUL_MAP=json.dumps(
                {
                    "correctness": "knuth",
                    "architecture": "socrates",
                    "dispatch": "sun-tzu",
                    "research": "davinci",
                    "default": "sun-tzu",
                }
            ),
        )

        self.assertEqual(result["soul_id"], "socrates")
        self.assertEqual(result["soul_category"], "architecture")

    def test_soul_map_empty_boundary_uses_default_dispatch_soul(self):
        result = self.run_pick_driver(
            {"complexity": "low", "capabilities": ["python"]},
            CLAWTA_EXPLORATION_PERCENT="0",
            KANBAN_BOARD_SOUL_MAP="",
        )

        self.assertEqual(result["soul_id"], "sun-tzu")
        self.assertEqual(result["soul_category"], "default")

    def test_soul_file_max_boundary_hashes_large_content(self):
        with tempfile.TemporaryDirectory() as tmp:
            souls_dir = Path(tmp)
            content = "knuth\n" + ("x" * 200_000)
            for soul_id in ("davinci", "sun-tzu", "socrates"):
                (souls_dir / f"{soul_id}.md").write_text(
                    f"{soul_id} soul\n", encoding="utf-8"
                )
            (souls_dir / "knuth.md").write_text(content, encoding="utf-8")

            result = self.run_pick_driver(
                {
                    "complexity": "low",
                    "capabilities": ["python"],
                    "ticket_title": "Invariant regression",
                },
                CLAWTA_EXPLORATION_PERCENT="0",
                CHITIN_SOULS_DIR=str(souls_dir),
            )

        self.assertEqual(result["soul_id"], "knuth")
        self.assertEqual(
            result["soul_hash"],
            hashlib.sha256(content.encode("utf-8")).hexdigest(),
        )

    def test_missing_default_soul_file_boundary_routes_with_unstamped_soul(self):
        # Boundary: error, default category. An empty souls dir plus a
        # souls-less repo root must not crash dispatch — the default
        # sun-tzu soul is reported with an empty hash.
        with tempfile.TemporaryDirectory() as tmp:
            souls_less_repo = Path(tmp) / "souls-less-repo"
            souls_less_repo.mkdir()
            result = self.run_pick_driver_raw(
                json.dumps({"complexity": "low", "capabilities": ["python"]}),
                CLAWTA_EXPLORATION_PERCENT="0",
                CHITIN_SOULS_DIR=str(souls_less_repo / "no-souls-here"),
                CHITIN_REPO=str(souls_less_repo),
            )

        self.assertEqual(result.returncode, 0, msg=result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(payload["soul_id"], "sun-tzu")
        self.assertEqual(payload["soul_hash"], "")
        self.assertEqual(payload["soul_category"], "default")


if __name__ == "__main__":
    unittest.main()
