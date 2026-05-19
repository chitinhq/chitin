"""CLI integration tests for swarm/bin/octi.

These exercise the subcommands that don't require a live kitty session:
verify (one-shot), pause/resume (flag-file mutation), status (file reads).

The `open`/`attach` subcommands run the long controller loop in the
foreground; we do not exercise them here. Their handlers are wrapped
around the same Controller class covered by test_octi_controller.py.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
OCTI = REPO / "swarm" / "bin" / "octi"


def _run(args: list[str], *, env: dict, cwd: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(OCTI), *args],
        env=env,
        cwd=str(cwd),
        capture_output=True,
        text=True,
        timeout=30,
    )


def _make_state_dir(state_root: Path, goal_id: str, *, worktree: Path) -> Path:
    sd = state_root / goal_id
    sd.mkdir(parents=True, exist_ok=True)
    (sd / "goal.txt").write_text("test goal\n")
    (sd / "goal_id").write_text(goal_id + "\n")
    (sd / "branch").write_text(f"octi/{goal_id}\n")
    (sd / "worktree").write_text(str(worktree) + "\n")
    return sd


class TestOctiVerifyCLI(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.state_root = Path(self.tmp.name) / ".swarm" / "octi"
        self.state_root.mkdir(parents=True)
        self.worktree = Path(self.tmp.name) / "wt"
        self.worktree.mkdir()
        self.sd = _make_state_dir(self.state_root, "g-cli", worktree=self.worktree)
        self.env = {
            **os.environ,
            "MINI_STATE_ROOT": str(self.state_root),
            "OCTI_DISCORD_WEBHOOK_URL": "",
        }

    def _write_status(self, **fields) -> None:
        payload = {
            "state": "done",
            "updated_at": 1_000_000,
            "summary": "",
            "next": "",
            "blockers": [],
            "verify": "true",
        }
        payload.update(fields)
        (self.sd / "status.json").write_text(json.dumps(payload))

    def test_verify_pass_exits_zero_and_writes_verdict(self):
        self._write_status(verify="true")
        r = _run(["verify", "--goal-id", "g-cli"], env=self.env, cwd=self.worktree)
        self.assertEqual(r.returncode, 0, f"stderr={r.stderr}")
        out = json.loads(r.stdout)
        self.assertEqual(out["verdict"], "passed")
        verdict = json.loads((self.sd / "controller_verdict.json").read_text())
        self.assertEqual(verdict["verdict"], "passed")
        self.assertEqual(verdict["via"], "octi verify (one-shot)")

    def test_verify_fail_exits_10(self):
        self._write_status(verify="false")
        r = _run(["verify", "--goal-id", "g-cli"], env=self.env, cwd=self.worktree)
        self.assertEqual(r.returncode, 10, f"stderr={r.stderr}")
        out = json.loads(r.stdout)
        self.assertEqual(out["verdict"], "failed")

    def test_verify_no_command_exits_11(self):
        self._write_status(verify="")
        r = _run(["verify", "--goal-id", "g-cli"], env=self.env, cwd=self.worktree)
        self.assertEqual(r.returncode, 11)
        out = json.loads(r.stdout)
        self.assertEqual(out["verdict"], "no_verifier")

    def test_verify_status_missing_exits_6(self):
        r = _run(["verify", "--goal-id", "g-cli"], env=self.env, cwd=self.worktree)
        self.assertEqual(r.returncode, 6, f"stdout={r.stdout} stderr={r.stderr}")

    def test_verify_timeout_short(self):
        self._write_status(verify="sleep 5")
        r = _run(
            ["verify", "--goal-id", "g-cli", "--timeout", "1"],
            env=self.env, cwd=self.worktree,
        )
        self.assertEqual(r.returncode, 10)
        out = json.loads(r.stdout)
        self.assertEqual(out["verdict"], "timeout")


class TestOctiPauseResumeCLI(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.state_root = Path(self.tmp.name) / ".swarm" / "octi"
        self.state_root.mkdir(parents=True)
        self.worktree = Path(self.tmp.name) / "wt"
        self.worktree.mkdir()
        self.sd = _make_state_dir(self.state_root, "g-pause", worktree=self.worktree)
        self.env = {**os.environ, "MINI_STATE_ROOT": str(self.state_root)}

    def test_pause_creates_flag(self):
        r = _run(["pause", "--goal-id", "g-pause"], env=self.env, cwd=self.worktree)
        self.assertEqual(r.returncode, 0, f"stderr={r.stderr}")
        flag = self.sd / "controller.paused"
        self.assertTrue(flag.is_file())

    def test_resume_removes_flag(self):
        flag = self.sd / "controller.paused"
        flag.write_text("paused\n")
        r = _run(["resume", "--goal-id", "g-pause"], env=self.env, cwd=self.worktree)
        self.assertEqual(r.returncode, 0)
        self.assertFalse(flag.is_file())

    def test_resume_when_not_paused_is_noop(self):
        flag = self.sd / "controller.paused"
        self.assertFalse(flag.is_file())
        r = _run(["resume", "--goal-id", "g-pause"], env=self.env, cwd=self.worktree)
        self.assertEqual(r.returncode, 0)


class TestOctiStatusCLI(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.state_root = Path(self.tmp.name) / ".swarm" / "octi"
        self.state_root.mkdir(parents=True)
        self.worktree = Path(self.tmp.name) / "wt"
        self.worktree.mkdir()
        self.sd = _make_state_dir(self.state_root, "g-stat", worktree=self.worktree)
        self.env = {**os.environ, "MINI_STATE_ROOT": str(self.state_root)}

    def test_status_includes_paused_flag(self):
        (self.sd / "controller.paused").write_text("\n")
        (self.sd / "status.json").write_text(json.dumps({
            "state": "working", "updated_at": 100, "summary": "x",
            "next": "y", "blockers": [], "verify": "",
        }))
        r = _run(["status", "--goal-id", "g-stat"], env=self.env, cwd=self.worktree)
        self.assertEqual(r.returncode, 0, f"stderr={r.stderr}")
        out = json.loads(r.stdout)
        self.assertTrue(out["paused"])
        self.assertEqual(out["status"]["state"], "working")
        self.assertIsNone(out["controller_verdict"])

    def test_status_includes_verdict(self):
        (self.sd / "status.json").write_text(json.dumps({
            "state": "done", "updated_at": 100, "summary": "",
            "next": "", "blockers": [], "verify": "true",
        }))
        (self.sd / "controller_verdict.json").write_text(json.dumps({
            "verdict": "passed", "returncode": 0,
        }))
        r = _run(["status", "--goal-id", "g-stat"], env=self.env, cwd=self.worktree)
        self.assertEqual(r.returncode, 0)
        out = json.loads(r.stdout)
        self.assertEqual(out["controller_verdict"]["verdict"], "passed")


class TestOctiGoalIdDiscovery(unittest.TestCase):
    """Goal-id auto-discovery from cwd parents."""

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        # Mimic an in-worktree invocation: cwd has its own .swarm/octi/<id>/
        self.cwd = Path(self.tmp.name) / "wt"
        self.cwd.mkdir()
        local_state = self.cwd / ".swarm" / "octi"
        local_state.mkdir(parents=True)
        _make_state_dir(local_state, "g-auto", worktree=self.cwd)
        # Status.json in the auto-discovered state dir
        (local_state / "g-auto" / "status.json").write_text(json.dumps({
            "state": "working", "updated_at": 0, "summary": "",
            "next": "", "blockers": [], "verify": "",
        }))
        self.env = {**os.environ, "MINI_STATE_ROOT": str(local_state)}

    def test_status_auto_discovers_goal_id_from_cwd(self):
        r = _run(["status"], env=self.env, cwd=self.cwd)
        self.assertEqual(r.returncode, 0, f"stderr={r.stderr}")
        out = json.loads(r.stdout)
        self.assertEqual(out["goal_id"], "g-auto")


if __name__ == "__main__":
    unittest.main()
