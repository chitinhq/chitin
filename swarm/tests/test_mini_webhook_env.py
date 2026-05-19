"""AC7 — webhook URL never sourced from committed file."""

from __future__ import annotations

import subprocess
import sys
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]


class TestWebhookEnvOnly(unittest.TestCase):
    def test_no_mini_file_holds_actual_webhook_url(self):
        """Real Discord webhook URLs have a long hash; pattern: discord.com/api/webhooks/<digits>/<token>.
        Walk Mini-owned files on disk (independent of git index state)."""
        import re

        url_re = re.compile(r"discord\.com/api/webhooks/\d+/[A-Za-z0-9_-]+")
        targets: list[Path] = []
        for rel in [
            "swarm/bin/mini", "swarm/bin/octi-worker",
            "swarm/bin/install-mini.sh", "swarm/docs/mini.md",
        ]:
            p = REPO / rel
            if p.is_file():
                targets.append(p)
        for d in [REPO / "swarm" / "mini", REPO / "swarm" / "tests"]:
            if d.is_dir():
                for p in d.rglob("*"):
                    if p.is_file() and (p.name.startswith("test_mini_") or "mini" in p.parts):
                        targets.append(p)

        offenders: list[str] = []
        for p in targets:
            try:
                if url_re.search(p.read_text(errors="ignore")):
                    offenders.append(str(p.relative_to(REPO)))
            except (OSError, UnicodeDecodeError):
                pass
        self.assertFalse(offenders,
                         f"Mini-owned file(s) contain a real Discord webhook URL: {offenders}")

    def test_webhook_file_path_is_gitignored_or_under_dotswarm(self):
        """Any per-session webhook URL must live under .swarm/octi/*/webhook.url"""
        # This is a documentation test — the path is structurally protected by
        # being under .swarm/ which should be gitignored.
        proc = subprocess.run(
            ["git", "-C", str(REPO), "check-ignore", ".swarm/octi/anything/webhook.url"],
            capture_output=True, text=True, check=False,
        )
        # exit 0 means the path IS ignored (good); exit 1 means not ignored (bad)
        self.assertEqual(proc.returncode, 0,
                         f".swarm/octi/*/webhook.url is not gitignored:\n{proc.stdout}\n{proc.stderr}")


if __name__ == "__main__":
    unittest.main()
