from __future__ import annotations

import importlib.util
import sys
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "workflows" / "ticket_metadata.py"


def load_module():
    spec = importlib.util.spec_from_loader(
        "ticket_metadata_test",
        SourceFileLoader("ticket_metadata_test", str(SCRIPT)),
    )
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules["ticket_metadata_test"] = module
    spec.loader.exec_module(module)
    return module


class TicketMetadataTests(unittest.TestCase):
    def test_parse_role_reads_explicit_known_role(self) -> None:
        module = load_module()
        body = "role: telemetry\n\nAcceptance:\n- mine chain"
        self.assertEqual(module.parse_role(body), "telemetry")

    def test_parse_role_falls_back_for_unknown_role(self) -> None:
        module = load_module()
        body = "role: architect\n"
        self.assertEqual(module.parse_role(body), "programmer")

    def test_resolve_role_reads_nested_task_body(self) -> None:
        module = load_module()
        ticket = {"task": {"body": "Role: reviewer\n"}}
        self.assertEqual(module.resolve_role(ticket), "reviewer")

    def test_resolve_role_routes_telemetry_from_title(self) -> None:
        module = load_module()
        ticket = {
            "task": {
                "title": "feat(swarm): add telemetry invariant role",
                "body": "Acceptance:\n- mine the chain",
            }
        }
        self.assertEqual(module.resolve_role(ticket), "telemetry")

    def test_resolve_role_keeps_explicit_role_over_title_inference(self) -> None:
        module = load_module()
        ticket = {
            "task": {
                "title": "review invariant docs",
                "body": "role: reviewer\n",
            }
        }
        self.assertEqual(module.resolve_role(ticket), "reviewer")

    def test_parse_role_defaults_when_missing(self) -> None:
        module = load_module()
        self.assertEqual(module.parse_role(None), "programmer")


if __name__ == "__main__":
    unittest.main()
