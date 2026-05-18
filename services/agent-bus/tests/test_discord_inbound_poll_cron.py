"""Spec 023 R2 + R3 — inbound poll cron registration + poll-all behavior.

# spec: 023-agent-bus-bidirectional-liveness

Static-analysis tests against the install script + lobster/python source.
Mirrors the regression-test pattern used by spec 018's
test_dispatch_base_freshness_regression.py.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
INSTALL_SCRIPT = ROOT / "swarm" / "bin" / "install-agent-bus-cron.sh"
DISCORD_MIRROR = ROOT / "services" / "agent-bus" / "discord_mirror.py"


class InstallScriptTests(unittest.TestCase):
    def test_install_script_exists_and_is_executable(self):
        """R3: install script lives at swarm/bin and is +x."""
        self.assertTrue(INSTALL_SCRIPT.exists(), f"{INSTALL_SCRIPT} must exist")
        self.assertTrue(os.access(INSTALL_SCRIPT, os.X_OK), "install script must be executable")

    def test_install_script_writes_known_job_id(self):
        """R3: jobs.json edit uses a stable id so reinstall replaces, not appends."""
        text = INSTALL_SCRIPT.read_text()
        self.assertIn('JOB_ID="agbus-inb-poll"', text,
                      "JOB_ID literal must be present for replace-on-reinstall")
        self.assertIn('JOB_NAME="agent-bus-inbound-poll"', text)

    def test_install_script_idempotent(self):
        """R3 AC: running the install script twice produces identical jobs.json."""
        with tempfile.TemporaryDirectory() as td:
            home = Path(td)
            cron_dir = home / ".hermes" / "cron"
            cron_dir.mkdir(parents=True)
            jobs_path = cron_dir / "jobs.json"
            jobs_path.write_text(json.dumps({"jobs": []}, indent=2) + "\n")

            scripts_dir = home / ".hermes" / "scripts"
            scripts_dir.mkdir(parents=True)

            env = {**os.environ, "HOME": str(home)}

            # Run install twice
            subprocess.run(
                ["bash", str(INSTALL_SCRIPT)],
                env=env, check=True, capture_output=True,
            )
            first = jobs_path.read_text()

            subprocess.run(
                ["bash", str(INSTALL_SCRIPT)],
                env=env, check=True, capture_output=True,
            )
            second = jobs_path.read_text()

            self.assertEqual(first, second,
                             "two consecutive installs must produce identical jobs.json")

            # Job must be present exactly once
            jobs = json.loads(first)["jobs"]
            ids = [j["id"] for j in jobs]
            self.assertEqual(ids.count("agbus-inb-poll"), 1,
                             "exactly one agbus-inb-poll job after install")


class DiscordMirrorPollAllTests(unittest.TestCase):
    def test_poll_all_command_registered(self):
        """R2: discord_mirror.py has a 'poll-all' subcommand."""
        text = DISCORD_MIRROR.read_text()
        self.assertIn('"poll-all"', text, "poll-all subparser must be registered")
        self.assertIn("def cmd_poll_all", text, "cmd_poll_all must be defined")

    def test_poll_all_iterates_every_linked_thread(self):
        """R2: poll_all reads from the threads table where discord_thread_id IS NOT NULL."""
        text = DISCORD_MIRROR.read_text()
        # Static check: the SELECT must scope to rows with a discord_thread_id
        self.assertIn("WHERE discord_thread_id IS NOT NULL", text,
                      "poll-all must filter threads to those with a discord_thread_id")
        # And it must iterate (not LIMIT 1)
        cmd_block = text.split("def cmd_poll_all", 1)[1].split("\ndef ", 1)[0]
        self.assertIn("for row in rows", cmd_block,
                      "poll-all must iterate every matching thread")

    def test_poll_all_uses_lock_file_for_concurrency_safety(self):
        """R6: a flock-based lock prevents double-ingestion under cron jitter."""
        text = DISCORD_MIRROR.read_text()
        cmd_block = text.split("def cmd_poll_all", 1)[1].split("\ndef ", 1)[0]
        self.assertIn("import fcntl", cmd_block, "fcntl must be imported for the lock")
        self.assertIn("LOCK_EX | fcntl.LOCK_NB", cmd_block,
                      "non-blocking exclusive lock required to skip-not-wait")

    def test_poll_all_handles_per_thread_errors_gracefully(self):
        """R2: a single thread's poll failure must NOT abort the iteration."""
        text = DISCORD_MIRROR.read_text()
        cmd_block = text.split("def cmd_poll_all", 1)[1].split("\ndef ", 1)[0]
        # The per-thread block must be in a try/except so one broken
        # mirror doesn't stop the rest.
        self.assertIn("try:", cmd_block)
        self.assertIn("except Exception", cmd_block)


if __name__ == "__main__":
    unittest.main()
