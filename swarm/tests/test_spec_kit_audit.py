from __future__ import annotations

import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path


def load_module():
    path = Path(__file__).resolve().parents[1] / "bin" / "spec-kit-audit.py"
    spec = importlib.util.spec_from_file_location("spec_kit_audit", path)
    module = importlib.util.module_from_spec(spec)
    sys.modules["spec_kit_audit"] = module
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


class SpecKitAuditTests(unittest.TestCase):
    def test_explicit_status_wins(self) -> None:
        module = load_module()
        self.assertEqual(module.explicit_status("**Status**: Implemented\n"), "Implemented")
        self.assertEqual(module.explicit_status("Status: Superseded\n"), "Superseded")

    def test_extract_tickets_is_exact(self) -> None:
        module = load_module()
        text = "Refs: t_75c8c8c1, t_75c8c8c1 and not_t_75c8c8c1_more"
        self.assertEqual(module.extract_tickets(text), ["t_75c8c8c1"])

    def test_infer_status_uses_terminal_markers_before_git(self) -> None:
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            spec = repo / ".specify" / "specs" / "001-demo" / "spec.md"
            spec.parent.mkdir(parents=True)
            spec.write_text("# Demo\n\n**Status**: Superseded\n\nRefs: t_1234abcd\n")

            item = module.infer_status(repo, spec)

        self.assertEqual(item.slug, "001-demo")
        self.assertEqual(item.status, "Superseded")
        self.assertEqual(item.tickets, ["t_1234abcd"])
        self.assertIn("explicit status marker", item.evidence)

    def test_format_text_lists_status(self) -> None:
        module = load_module()
        item = module.SpecAudit("001-demo", ".specify/specs/001-demo/spec.md", "Accepted", ["t_1234abcd"], 1, "tracked")
        text = module.format_text([item])
        self.assertIn("001-demo\tAccepted\tt_1234abcd\ttracked", text)


if __name__ == "__main__":
    unittest.main()
