"""AC9 + AC13 — temp prompt file unlink verification.

The P2 security gate from the spec review: argv cleanliness alone is
insufficient; we must also verify temp prompt files are unlinked.
"""

from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from swarm.mini._internal import kitty as kitty_mod
from swarm.mini._internal.statedir import cleanup_stale_temp_files


def _fake_ok_proc() -> subprocess.CompletedProcess:
    return subprocess.CompletedProcess(args=["kitty"], returncode=0, stdout=b"", stderr=b"")


class TestTempFileUnlink(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.sd = Path(self.tmp.name)

    def test_temp_file_unlinked_after_inject(self):
        captured: dict = {}

        def fake_run(args, **kwargs):
            # capture the temp file path that was passed via --from-file
            for a in args:
                if isinstance(a, str) and a.startswith("--from-file="):
                    captured["path"] = a[len("--from-file="):]
            return _fake_ok_proc()

        with mock.patch.object(kitty_mod, "run_kitten",
                               side_effect=lambda a, **kw: fake_run([kitty_mod.kitty_bin(), "@"] + a, **kw)):
            kitty_mod.inject_via_temp_file(
                "g1", "/goal hello world", state_dir=self.sd, label="open",
                wait_ready=False,
            )

        self.assertIn("path", captured)
        self.assertFalse(Path(captured["path"]).exists(),
                         f"temp file leaked: {captured['path']!r}")

    def test_already_gone_unlink_not_an_error(self):
        # Force the kitty stub to unlink the temp file mid-call.
        def fake_run(args, **kwargs):
            for a in args:
                if isinstance(a, str) and a.startswith("--from-file="):
                    Path(a[len("--from-file="):]).unlink()
            return _fake_ok_proc()

        with mock.patch.object(kitty_mod, "run_kitten",
                               side_effect=lambda a, **kw: fake_run([kitty_mod.kitty_bin(), "@"] + a, **kw)):
            kitty_mod.inject_via_temp_file(
                "g1", "msg", state_dir=self.sd, label="nudge", wait_ready=False,
            )  # must not raise

    def test_stale_cleanup_removes_old_inject_files(self):
        # Simulate a crashed prior session leaving temp files behind.
        import os as _os
        from swarm.mini._internal.statedir import STATE_ROOT_ENV, state_dir as _state_dir
        _os.environ[STATE_ROOT_ENV] = str(self.sd)
        try:
            sd = _state_dir("g-stale")
            sd.mkdir(parents=True, exist_ok=True)
            stale1 = sd / ".inject-open-12345-aaaa.txt"
            stale1.write_text("stale prompt material")
            old_ts = time.time() - 3600
            os.utime(stale1, (old_ts, old_ts))
            fresh = sd / ".inject-nudge-67890-bbbb.txt"
            fresh.write_text("fresh prompt material")

            unlinked = cleanup_stale_temp_files("g-stale", max_age_seconds=60)
            self.assertEqual(unlinked, 1)
            self.assertFalse(stale1.exists())
            self.assertTrue(fresh.exists())
        finally:
            _os.environ.pop(STATE_ROOT_ENV, None)


if __name__ == "__main__":
    unittest.main()
