from __future__ import annotations

import importlib.util
import subprocess
import sys
from importlib.machinery import SourceFileLoader
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
INVARIANTS = REPO_ROOT / "swarm" / "bin" / "clawta-invariants"


def _load_module():
    spec = importlib.util.spec_from_loader(
        "clawta_invariants_pr_liveness_test",
        SourceFileLoader("clawta_invariants_pr_liveness_test", str(INVARIANTS)),
    )
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules["clawta_invariants_pr_liveness_test"] = module
    spec.loader.exec_module(module)
    return module


def _completed(stdout: str = "", stderr: str = "", returncode: int = 0):
    return subprocess.CompletedProcess(["fake"], returncode, stdout=stdout, stderr=stderr)


def test_pr_liveness_rejects_closed_pr(monkeypatch):
    module = _load_module()

    def fake_run(cmd, **kwargs):
        assert cmd[:3] == ["gh", "pr", "view"]
        return _completed('{"state":"CLOSED","headRefName":"agent/codex-5f18463a","baseRefName":"swarm","mergedAt":null}')

    monkeypatch.setattr(module, "run", fake_run)

    live, reason = module.pr_liveness("https://github.com/o/r/pull/907", "agent/codex-5f18463a")

    assert live is False
    assert "not OPEN" in reason


def test_pr_liveness_rejects_wrong_head(monkeypatch):
    module = _load_module()

    def fake_run(cmd, **kwargs):
        return _completed('{"state":"OPEN","headRefName":"agent/other","baseRefName":"swarm","mergedAt":null}')

    monkeypatch.setattr(module, "run", fake_run)

    live, reason = module.pr_liveness("https://github.com/o/r/pull/907", "agent/codex-5f18463a")

    assert live is False
    assert "does not match" in reason


def test_repair_transient_pr_failure_does_not_record_closed_pr(monkeypatch):
    module = _load_module()
    recorded = []

    monkeypatch.setattr(
        module,
        "fetch_comments",
        lambda ticket_id: [
            "branch agent/codex-5f18463a pushed but gh pr create failed",
            "PR opened: https://github.com/wjcmurphy/bench-devs-platform/pull/907",
        ],
    )
    monkeypatch.setattr(module, "pr_liveness", lambda pr_url, branch: (False, "PR is CLOSED (not OPEN)"))
    monkeypatch.setattr(module, "record_pr", lambda ticket_id, pr_url, dry_run: recorded.append((ticket_id, pr_url)) or True)

    repaired = module.repair_transient_pr_failure(
        {"id": "t_5f18463a", "assignee": "codex"},
        dry_run=False,
    )

    assert repaired is None
    assert recorded == []


def test_repair_transient_pr_failure_records_open_matching_pr(monkeypatch):
    module = _load_module()
    recorded = []

    monkeypatch.setattr(
        module,
        "fetch_comments",
        lambda ticket_id: [
            "branch agent/codex-5f18463a pushed but gh pr create failed",
            "PR opened: https://github.com/wjcmurphy/bench-devs-platform/pull/907",
        ],
    )
    monkeypatch.setattr(module, "pr_liveness", lambda pr_url, branch: (True, "open matching PR"))
    monkeypatch.setattr(module, "record_pr", lambda ticket_id, pr_url, dry_run: recorded.append((ticket_id, pr_url, dry_run)) or True)

    repaired = module.repair_transient_pr_failure(
        {"id": "t_5f18463a", "assignee": "codex"},
        dry_run=False,
    )

    assert repaired == "https://github.com/wjcmurphy/bench-devs-platform/pull/907"
    assert recorded == [("t_5f18463a", "https://github.com/wjcmurphy/bench-devs-platform/pull/907", False)]
