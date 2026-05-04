"""Tests for analysis.skill_mine — n-gram surface over chain events.

Covers the core invariants:
  - chain_id grouping (events from different files but same chain
    fold into one session)
  - to_verb's abstraction (gh-pr-view, git-status, read-yaml, etc.)
  - is_trivial filtering (Read→Read→Read drops out)
  - per-session uniqueness so one chatty chain doesn't over-rank
    a pattern.
"""
from __future__ import annotations

import json
from pathlib import Path

from analysis.skill_mine import (
    collect_sessions,
    is_trivial,
    iter_decision_events,
    to_verb,
)


def _ev(chain_id: str, ts: str, action_type: str, target: str = "", event_type: str = "decision") -> str:
    return json.dumps({
        "chain_id": chain_id,
        "ts": ts,
        "event_type": event_type,
        "payload": {"action_type": action_type, "action_target": target},
    })


def test_to_verb_shell_subcommand() -> None:
    # Implementation is intentionally one level deep (cmd-subcmd) —
    # `gh pr view ...` and `gh pr checks ...` collapse to the same
    # `gh-pr` bucket because that level of granularity is enough to
    # rank patterns; deeper splits balloon the verb cardinality.
    assert to_verb("shell.exec", "gh pr view 247") == "gh-pr"
    assert to_verb("shell.exec", "git status") == "git-status"
    assert to_verb("shell.exec", "rtk gh pr checks") == "gh-pr"


def test_to_verb_chain_collapse() -> None:
    assert to_verb("shell.exec", "ls -la && git status") == "shell-chain"
    assert to_verb("shell.exec", "x || y") == "shell-chain"


def test_to_verb_file_extension_buckets() -> None:
    assert to_verb("file.read", "/tmp/x.yaml") == "read-yaml"
    assert to_verb("file.read", "/tmp/x.yml") == "read-yaml"  # yml→yaml normalization
    assert to_verb("file.read", "/tmp/x.tsx") == "read-ts"   # tsx→ts
    assert to_verb("file.write", "/tmp/x.go") == "edit-go"


def test_is_trivial_filters_repeats_and_browse() -> None:
    assert is_trivial(("read-md", "read-md"))
    assert is_trivial(("read-md", "read-ts"))      # all-read
    assert is_trivial(("edit-json", "edit-yaml"))  # all-edit
    assert not is_trivial(("read-md", "git-status"))
    assert not is_trivial(("read-md", "edit-md"))  # mixed


def test_iter_decision_events_skips_non_decision(tmp_path: Path) -> None:
    p = tmp_path / "events-x.jsonl"
    p.write_text(
        _ev("c1", "t1", "shell.exec", "ls") + "\n"
        + json.dumps({"event_type": "audit", "payload": {}}) + "\n"
        + _ev("c1", "t2", "file.read", "/x") + "\n"
    )
    rows = list(iter_decision_events(p))
    assert len(rows) == 2
    assert rows[0][0] == "c1"
    assert rows[0][2] == "shell.exec"


def test_collect_sessions_groups_by_chain_id_across_files(tmp_path: Path) -> None:
    """Two files, three events between them, two chain_ids —
    must group into two sessions, not three."""
    f1 = tmp_path / "events-aaa.jsonl"
    f2 = tmp_path / "events-bbb.jsonl"
    f1.write_text(
        _ev("chain-A", "2026-05-03T10:00:00Z", "shell.exec", "ls") + "\n"
        + _ev("chain-B", "2026-05-03T10:01:00Z", "file.read", "/x") + "\n"
    )
    f2.write_text(
        _ev("chain-A", "2026-05-03T10:02:00Z", "shell.exec", "git status") + "\n"
    )
    sessions = collect_sessions([f1, f2])
    assert "chain-A" in sessions
    assert "chain-B" in sessions
    # chain-A should have BOTH ls and git status, ordered chronologically
    assert len(sessions["chain-A"]) == 2
    assert sessions["chain-A"][0][0] == "ls"           # first event
    assert sessions["chain-A"][1][0] == "git-status"   # second event from second file


def test_collect_sessions_orders_chronologically(tmp_path: Path) -> None:
    """Events written out-of-order (later ts before earlier) must
    still produce a chronologically-ordered session — n-gram
    semantics depend on it."""
    f = tmp_path / "events-aaa.jsonl"
    f.write_text(
        _ev("chain-X", "2026-05-03T10:02:00Z", "shell.exec", "git push") + "\n"
        + _ev("chain-X", "2026-05-03T10:00:00Z", "shell.exec", "git status") + "\n"
        + _ev("chain-X", "2026-05-03T10:01:00Z", "shell.exec", "git add") + "\n"
    )
    sessions = collect_sessions([f])
    seq = [v for v, _ in sessions["chain-X"]]
    assert seq == ["git-status", "git-add", "git-push"]


def test_collect_sessions_handles_missing_chain_id(tmp_path: Path) -> None:
    """Events without chain_id (very old logs) bucket under <unknown>
    instead of being silently dropped."""
    f = tmp_path / "events-aaa.jsonl"
    f.write_text(
        json.dumps({
            "ts": "2026-05-03T10:00:00Z",
            "event_type": "decision",
            "payload": {"action_type": "shell.exec", "action_target": "ls"},
        }) + "\n"
    )
    sessions = collect_sessions([f])
    assert "<unknown>" in sessions
    assert len(sessions["<unknown>"]) == 1
