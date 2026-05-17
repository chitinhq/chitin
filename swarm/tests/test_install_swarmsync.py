from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
INSTALL = REPO_ROOT / "scripts" / "install-swarm.sh"
CHECK_SYNC = REPO_ROOT / "scripts" / "check-swarm-deployed-sync.sh"
CHECK_HARDCODES = REPO_ROOT / "scripts" / "check-no-chitin-hardcodes.sh"


class InstallSwarmSyncTests(unittest.TestCase):
    def test_installs_imported_helper_modules_and_import_smokes_scripts(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            openclaw = Path(tmp) / "openclaw"
            home.mkdir()
            env = os.environ.copy()
            env.update(
                {
                    "HOME": str(home),
                    "OPENCLAW_HOME": str(openclaw),
                    "KANBAN_BOARDS_DIR": str(home / ".hermes" / "kanban" / "boards"),
                    "KANBAN_DB": str(home / ".hermes" / "kanban" / "boards" / "chitin" / "kanban.db"),
                }
            )

            subprocess.run(["bash", str(INSTALL)], cwd=REPO_ROOT, env=env, check=True, text=True, capture_output=True)
            subprocess.run(["bash", str(CHECK_SYNC)], cwd=REPO_ROOT, env=env, check=True, text=True, capture_output=True)
            subprocess.run(["bash", str(CHECK_HARDCODES)], cwd=REPO_ROOT, env=env, check=True, text=True, capture_output=True)

            self.assertTrue((openclaw / "bin" / "board_resolver.py").exists())
            board_config = home / ".hermes" / "kanban" / "boards" / "chitin" / "config.json"
            self.assertTrue(board_config.exists())

            repo_value = subprocess.run(
                ["python3", str(openclaw / "bin" / "board_resolver.py"), "repo"],
                cwd=REPO_ROOT,
                env=env,
                text=True,
                capture_output=True,
                check=True,
            ).stdout.strip()
            workspace_value = subprocess.run(
                ["python3", str(openclaw / "bin" / "board_resolver.py"), "workspace"],
                cwd=REPO_ROOT,
                env=env,
                text=True,
                capture_output=True,
                check=True,
            ).stdout.strip()
            self.assertEqual(repo_value, "chitinhq/chitin")
            self.assertEqual(workspace_value, str(home / "workspace" / "chitin"))

            for script in ("clawta-report", "clawta-pr-lifecycle"):
                deployed = openclaw / "bin" / script
                proc = subprocess.run(
                    ["python3", "-c", f"import runpy; runpy.run_path({str(deployed)!r}, run_name='__clawta_import_smoke__')"],
                    cwd=REPO_ROOT,
                    env=env,
                    text=True,
                    capture_output=True,
                    timeout=20,
                )
                self.assertEqual(proc.returncode, 0, proc.stderr)

    def test_hardcode_guard_passes_for_repo_tree(self) -> None:
        proc = subprocess.run(
            ["bash", str(CHECK_HARDCODES)],
            cwd=REPO_ROOT,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)


if __name__ == "__main__":
    unittest.main()
