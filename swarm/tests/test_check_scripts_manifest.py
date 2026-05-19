#!/usr/bin/env python3
"""Behavior tests for scripts/check-scripts-manifest.sh."""

from __future__ import annotations

import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
LINTER_SRC = REPO_ROOT / "scripts" / "check-scripts-manifest.sh"


def make_sandbox() -> Path:
    sandbox = Path(tempfile.mkdtemp(prefix="scripts-manifest-test-"))
    (sandbox / "scripts").mkdir()
    shutil.copy(LINTER_SRC, sandbox / "scripts" / "check-scripts-manifest.sh")
    (sandbox / "scripts" / "check-scripts-manifest.sh").chmod(0o755)
    return sandbox


def run_linter(sandbox: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["bash", "scripts/check-scripts-manifest.sh"],
        cwd=sandbox,
        capture_output=True,
        text=True,
    )


class ScriptsManifestTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write(self, relpath: str, body: str, executable: bool = False) -> None:
        path = self.sandbox / relpath
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(body)
        if executable:
            path.chmod(0o755)

    def _write_manifest(self, body: str) -> None:
        self._write("scripts/MANIFEST.yaml", textwrap.dedent(body).lstrip())

    def test_empty_manifest_passes_when_only_manifest_is_present(self) -> None:
        self._write_manifest(
            """
            exclude_patterns:
              - scripts/MANIFEST.yaml
              - scripts/check-scripts-manifest.sh
            entries: []
            """
        )

        result = run_linter(self.sandbox)
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn("scripts manifest OK: 0 tracked file(s)", result.stdout)

    def test_tracked_runtime_critical_with_test_reference_passes(self) -> None:
        self._write("scripts/runtime.sh", "#!/usr/bin/env bash\nexit 0\n", executable=True)
        self._write("tests/runtime.bats", "#!/usr/bin/env bats\n", executable=True)
        self._write_manifest(
            """
            exclude_patterns:
              - scripts/MANIFEST.yaml
              - scripts/check-scripts-manifest.sh
            entries:
              - path: scripts/runtime.sh
                category: runtime-critical
                purpose: test fixture runtime path
                tested_by: tests/runtime.bats
            """
        )

        result = run_linter(self.sandbox)
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn("scripts manifest OK: 1 tracked file(s)", result.stdout)

    def test_untracked_script_fails(self) -> None:
        self._write("scripts/runtime.sh", "#!/usr/bin/env bash\nexit 0\n", executable=True)
        self._write("scripts/forgotten.sh", "#!/usr/bin/env bash\nexit 0\n", executable=True)
        self._write("tests/runtime.bats", "#!/usr/bin/env bats\n", executable=True)
        self._write_manifest(
            """
            exclude_patterns:
              - scripts/MANIFEST.yaml
              - scripts/check-scripts-manifest.sh
            entries:
              - path: scripts/runtime.sh
                category: runtime-critical
                purpose: test fixture runtime path
                tested_by: tests/runtime.bats
            """
        )

        result = run_linter(self.sandbox)
        self.assertEqual(result.returncode, 1)
        self.assertIn("untracked scripts:", result.stdout)
        self.assertIn("scripts/forgotten.sh", result.stdout)

    def test_runtime_critical_missing_coverage_fails(self) -> None:
        self._write("scripts/runtime.sh", "#!/usr/bin/env bash\nexit 0\n", executable=True)
        self._write_manifest(
            """
            exclude_patterns:
              - scripts/MANIFEST.yaml
              - scripts/check-scripts-manifest.sh
            entries:
              - path: scripts/runtime.sh
                category: runtime-critical
                purpose: test fixture runtime path
            """
        )

        result = run_linter(self.sandbox)
        self.assertEqual(result.returncode, 1)
        self.assertIn("runtime-critical coverage gaps:", result.stdout)
        self.assertIn("scripts/runtime.sh: runtime-critical entries need tested_by or port_ticket", result.stdout)

    def test_expired_migration_fails(self) -> None:
        self._write("scripts/legacy.py", "#!/usr/bin/env python3\nprint('ok')\n", executable=True)
        self._write_manifest(
            """
            exclude_patterns:
              - scripts/MANIFEST.yaml
              - scripts/check-scripts-manifest.sh
            entries:
              - path: scripts/legacy.py
                category: migration
                purpose: expired migration fixture
                added_on: 2026-01-01
                expires_on: 2026-01-02
            """
        )

        result = run_linter(self.sandbox)
        self.assertEqual(result.returncode, 1)
        self.assertIn("migration TTL errors:", result.stdout)
        self.assertIn("expired migration", result.stdout)

    def test_excluded_support_files_do_not_require_entries(self) -> None:
        self._write("scripts/tool.sh", "#!/usr/bin/env bash\nexit 0\n", executable=True)
        self._write("scripts/support/generated.txt", "ignore me\n")
        self._write_manifest(
            """
            exclude_patterns:
              - scripts/MANIFEST.yaml
              - scripts/check-scripts-manifest.sh
              - scripts/support/**
            entries:
              - path: scripts/tool.sh
                category: operator
                purpose: tracked operator tool
            """
        )

        result = run_linter(self.sandbox)
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
