from __future__ import annotations

import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CHECK = ROOT / "scripts" / "check-swarm-kanban-isolation.sh"


class CheckSwarmKanbanIsolationTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = Path(tempfile.mkdtemp(prefix="kanban-isolation-"))
        (self.sandbox / "swarm").mkdir()
        (self.sandbox / "swarm" / "tests").mkdir(parents=True)
        (self.sandbox / "scripts").mkdir()
        shutil.copy(CHECK, self.sandbox / "scripts" / CHECK.name)
        (self.sandbox / "scripts" / CHECK.name).chmod(0o755)

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def run_check(self) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["bash", str(self.sandbox / "scripts" / CHECK.name)],
            cwd=self.sandbox,
            text=True,
            capture_output=True,
        )

    def test_passes_when_swarm_has_no_direct_kanban_writes(self) -> None:
        (self.sandbox / "swarm" / "ok.py").write_text(
            "conn.execute(\"SELECT status FROM tasks WHERE id = ?\", (ticket_id,))\n"
        )
        result = self.run_check()
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn("PASS", result.stdout)

    def test_rejects_python_update_against_tasks(self) -> None:
        (self.sandbox / "swarm" / "bad.py").write_text(
            "conn.execute(\"UPDATE tasks SET assignee = ? WHERE id = ?\", (\"red\", ticket_id))\n"
        )
        result = self.run_check()
        self.assertEqual(result.returncode, 1)
        self.assertIn("bad.py", result.stderr)

    def test_ignores_test_fixtures(self) -> None:
        (self.sandbox / "swarm" / "tests" / "fixture.py").write_text(
            "conn.execute(\"UPDATE tasks SET assignee = ? WHERE id = ?\", (\"red\", ticket_id))\n"
        )
        result = self.run_check()
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
